/*
验证快速切歌取消旧下载、阻塞解码和预取，并保持 FLAC 增量播放与完整缓存。
*/

package player

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/features/netease"
)

type heldAudioBody struct {
	ctx     context.Context
	prefix  *bytes.Reader
	waiting chan struct{}
	more    chan []byte
	closed  atomic.Bool
}

func (b *heldAudioBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	b.waiting <- struct{}{}
	select {
	case data := <-b.more:
		b.prefix = bytes.NewReader(data)
		return b.Read(p)
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}

func (b *heldAudioBody) Close() error { b.closed.Store(true); return nil }

type rapidSwitchTransport func(*http.Request) (*http.Response, error)

func (f rapidSwitchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A -> B -> C -> B must retire abandoned opens instead of leaving four
// downloads active, including two independent copies of B's partial audio.
func TestRapidSwitchCancelsPendingAudio(t *testing.T) {
	data := testAudio(t, "seek.flac")
	cache := NewCache(t.TempDir(), 1<<20)
	requests := make(chan *heldAudioBody, 16)
	waiting := make(chan struct{}, 16)
	oldClient, oldURL := http.DefaultClient, songURL
	http.DefaultClient = &http.Client{Transport: rapidSwitchTransport(func(r *http.Request) (*http.Response, error) {
		body := &heldAudioBody{ctx: r.Context(), prefix: bytes.NewReader(data[:16]), waiting: waiting, more: make(chan []byte, 1)}
		requests <- body
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: int64(len(data)), Body: body}, nil
	})}
	defer func() { http.DefaultClient, songURL = oldClient, oldURL }()
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		return &netease.Stream{URL: fmt.Sprintf("http://audio.test/%d", id), Size: int64(len(data)), Type: "flac", Level: level}, nil
	}
	posts := make(chan func(), 64)
	resources := make(chan [2]func(), 16)
	p := New(func(fn func()) { posts <- fn }, func(apply, release func()) { resources <- [2]func(){apply, release} })
	p.Cache = cache
	p.volume = 0
	p.Quality = func() string { return "lossless" }
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	loads := 0
	defer func() {
		p.Close()
		cache.Quiesce()
		for range loads {
			select {
			case result := <-resources:
				result[1]()
			case <-time.After(5 * time.Second):
				t.Error("abandoned load did not retire after explicit download cancellation")
				return
			}
		}
		run(posts)
	}()
	prefixArrived := func() {
		t.Helper()
		loads++
		select {
		case <-waiting:
		case <-time.After(5 * time.Second):
			t.Fatal("controlled audio response did not reach its partial-data wait")
		}
	}
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 0)
	prefixArrived()
	p.Next()
	prefixArrived()
	p.Next()
	prefixArrived()
	p.Prev()
	prefixArrived()
	run(posts)
	active, total := 0, len(requests)
	var current *heldAudioBody
	for i := range total {
		body := <-requests
		if i < total-1 {
			select {
			case <-body.ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("switched-away audio request was not cancelled")
			}
		} else {
			current = body
		}
		if !body.closed.Load() && body.ctx.Err() == nil {
			active++
		}
	}
	t.Logf("A -> B -> C -> B: HTTP requests=%d, active=%d, selected=%d, loading=%v, buffering=%v", total, active, p.Current().ID, p.Loading(), p.Buffering())
	if total != 4 || active > 1 {
		t.Errorf("abandoned audio requests remained active: total=%d active=%d", total, active)
	}
	if current == nil {
		t.Fatal("replacement download was not started")
	}
	prefix := flacFirstFramePrefix(t, data)
	current.more <- data[16:prefix]
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !p.Ready() {
		select {
		case fn := <-posts:
			fn()
		case result := <-resources:
			loads--
			result[0]()
		case <-deadline.C:
			t.Fatal("returning to B waited for a complete download")
		}
	}
	shared, err := openShared(p.stream.(*flacStream).dl, 5000)
	if err != nil {
		t.Fatal(err)
	}
	n, err := shared.Read(make([]float32, 512))
	shared.Close()
	progress, _ := p.Downloaded()
	if err != nil || n == 0 || progress >= 1 || p.Buffering() || p.Current().ID != 2 {
		t.Fatalf("replacement did not resume incrementally: samples=%d err=%v progress=%v buffering=%v", n, err, progress, p.Buffering())
	}
	if cache.Size() != 0 {
		t.Fatal("incomplete audio was published as a complete cache entry")
	}
	t.Logf("B resumed with %d PCM samples at %.2f%% downloaded", n, progress*100)
}

// A decoder already waiting for missing FLAC frames must be woken by stop;
// audio.Player.Close alone only marks its worker closed after Read returns.
func TestStopCancelsBlockedFLACRead(t *testing.T) {
	data := testAudio(t, "seek.flac")
	complete := downloaded(data)
	start, err := flacDataStart(complete)
	complete.Close()
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		dl := downloaded(data[:start])
		dl.done, dl.size, dl.length = false, int64(len(data)), int64(len(data))
		defer dl.Close()
		s, err := openStream(dl, 5000)
		if err != nil {
			t.Fatal(err)
		}
		p := New(func(func()) {})
		p.stream, p.out = s, audio.NewPlayer(s)
		defer p.Close()
		synctest.Wait() // The decoder now waits for its first frame.
		p.stop()
		time.Sleep(101 * time.Millisecond) // Virtual time: run the existing 100 ms close timer.
		synctest.Wait()
		dl.mu.Lock()
		closed := dl.closed
		dl.mu.Unlock()
		t.Logf("after audio close timer: underlying download closed=%v", closed)
		if !closed {
			t.Error("decoder close waits for Read, but Read needs the download to be cancelled")
		}
		dl.Close()
		synctest.Wait()
	})
}

// A complete first FLAC frame can produce audio before the remaining file
// arrives; whole-file waiting is not required by this decoder's startup.
func TestFLACStartsFromFirstFrame(t *testing.T) {
	data := testAudio(t, "seek.flac")
	prefix := flacFirstFramePrefix(t, data)
	synctest.Test(t, func(t *testing.T) {
		dl := downloaded(data[:prefix])
		dl.done, dl.size, dl.length = false, int64(len(data)), int64(len(data))
		defer dl.Close()
		result := make(chan int, 1)
		go func() {
			s, err := openStream(dl, 5000)
			if err != nil {
				result <- 0
				return
			}
			n, _ := s.Read(make([]float32, 512))
			result <- n
		}()
		synctest.Wait()
		select {
		case samples := <-result:
			if samples <= 0 || dl.Progress() >= 1 {
				t.Fatal("did not demonstrate incremental FLAC decoding")
			}
		default:
			dl.Close()
			synctest.Wait()
			t.Fatal("FLAC startup waited past its complete first frame")
		}
	})
}

func flacFirstFramePrefix(t *testing.T, data []byte) int {
	t.Helper()
	complete := downloaded(data)
	decoded, err := openStream(complete, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	flac := decoded.(*flacStream)
	reader := bytes.NewReader(data[flac.start:])
	if _, err := flac.parse(reader); err != nil {
		t.Fatal(err)
	}
	return len(data) - reader.Len()
}

// Next, shutdown and cache relocation cancel URL resolution before an audio
// download exists. A cancelled load never retries or overwrites its successor.
func TestPendingURLCancellationAcrossLifecycle(t *testing.T) {
	for _, action := range []string{"next", "close", "cache-move"} {
		t.Run(action, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				old := songURL
				defer func() { songURL = old }()
				started := make(chan context.Context, 4)
				songURL = func(ctx context.Context, _ int64, _ string) (*netease.Stream, error) {
					started <- ctx
					<-ctx.Done()
					return nil, ctx.Err()
				}
				posts := make(chan func(), 32)
				p := New(func(fn func()) { posts <- fn })
				p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
				defer func() { p.Close(); synctest.Wait(); run(posts) }()
				p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}}, 0)
				synctest.Wait()
				ctx := <-started
				switch action {
				case "next":
					p.Next()
				case "close":
					p.Close()
				case "cache-move":
					p.SuspendCache()
				}
				synctest.Wait()
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("retired URL request was not cancelled")
				}
				run(posts)
				if p.tries != 0 || p.Err() != "" || action == "next" && p.Current().ID != 2 {
					t.Fatal("cancelled result retried or changed the current selection")
				}
			})
		})
	}
}

// Cancelling a waiting prefetch releases its worker while the preceding
// track retains its unfinished download and can continue playing.
func TestPrefetchCancellationDoesNotStopCurrentDownload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dl := downloaded(nil)
		dl.done = false
		defer dl.Close()
		p := &Player{post: func(func()) {}}
		p.startPrefetch(2, "exhigh", &mp3Stream{dl: dl}, func(context.Context, int64, string) (*netease.Stream, error) {
			t.Error("cancelled prefetch requested a URL")
			return nil, netease.ErrUnavailable
		}, 0)
		a := p.ahead
		synctest.Wait()
		p.dropAhead()
		synctest.Wait()
		select {
		case <-a.ready:
		default:
			t.Error("cancelled prefetch still waited for the preceding download")
		}
		dl.mu.Lock()
		closed := dl.closed
		dl.mu.Unlock()
		if closed {
			t.Fatal("prefetch cancellation closed foreground audio")
		}
		dl.Close()
		synctest.Wait()
	})
}

// A URL prefetch survives adoption by Next, then belongs to that foreground
// load and is cancelled if the listener skips it before the URL arrives.
func TestAdoptedPrefetchCancelledByLaterSelection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := songURL
		defer func() { songURL = old }()
		songURL = func(context.Context, int64, string) (*netease.Stream, error) { return nil, netease.ErrUnavailable }
		posts := make(chan func(), 32)
		p := New(func(fn func()) { posts <- fn })
		p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
		defer func() { p.Close(); synctest.Wait(); run(posts) }()
		Cue(p, []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
		started := make(chan context.Context, 1)
		p.startPrefetch(2, "exhigh", nil, func(ctx context.Context, _ int64, _ string) (*netease.Stream, error) {
			started <- ctx
			<-ctx.Done()
			return nil, ctx.Err()
		}, 0)
		synctest.Wait()
		ctx := <-started
		p.load(1, 1)
		synctest.Wait()
		if ctx.Err() != nil {
			t.Fatal("adopting the prefetch cancelled its own request")
		}
		p.Next()
		synctest.Wait()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("later selection did not cancel the adopted prefetch")
		}
	})
}

// Cancelling an open of a complete cached song preserves its audio and
// metadata so the same song remains available offline after cancellation.
func TestCancelledCachedOpenPreservesCompleteAudio(t *testing.T) {
	cache := NewCache(t.TempDir(), 1<<20)
	seedCache(t, cache, 1, song(t))
	dl, _, ok := cache.lookup(1, "exhigh")
	if !ok {
		t.Fatal("missing cached fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, err := openDownload(ctx, dl, 5000); s != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cached open: stream=%v error=%v", s, err)
	}
	retained, _, err := cache.lookupOffline(1, "exhigh")
	if err != nil {
		t.Fatal("cancellation invalidated a complete cache entry")
	}
	retained.Close()
}

// A prepared result can wait in the UI queue after decoding. Moving on must
// close that source before its obsolete result is eventually dispatched.
func TestCancelledPreparedAudioBeforeDispatch(t *testing.T) {
	cache := NewCache(t.TempDir(), 1<<20)
	seedCache(t, cache, 1, song(t))
	seedCache(t, cache, 2, song(t))
	synctest.Test(t, func(t *testing.T) {
		posts := make(chan func(), 32)
		resources := make(chan [2]func(), 4)
		p := New(func(fn func()) { posts <- fn }, func(apply, release func()) { resources <- [2]func(){apply, release} })
		p.Cache = cache
		p.volume = 0
		p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
		defer func() {
			p.Close()
			synctest.Wait()
			for len(resources) > 0 {
				(<-resources)[1]()
			}
			run(posts)
		}()
		p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}}, 0)
		synctest.Wait()
		if len(resources) != 1 || p.Ready() {
			t.Fatal("fixture did not leave one decoded result awaiting dispatch")
		}
		cache.mu.Lock()
		var prepared *download
		for dl := range cache.active {
			prepared = dl
		}
		cache.mu.Unlock()
		if prepared == nil {
			t.Fatal("prepared source is missing")
		}
		p.Next()
		synctest.Wait()
		prepared.mu.Lock()
		closed := prepared.closed
		prepared.mu.Unlock()
		if !closed {
			t.Error("obsolete source stayed open until UI dispatch")
		}
		for len(resources) > 0 {
			(<-resources)[0]()
		}
		if !p.Ready() || p.Current().ID != 2 || p.Buffering() {
			t.Fatal("obsolete result displaced the replacement audio")
		}
	})
}

// A URL response racing with cancellation must not start a new download,
// even if the URL provider returns success after its context was cancelled.
func TestCancelledURLResultAvoidsAudioDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Player{}
	dl, _, err := p.fetch(ctx, netease.Track{ID: 1}, "lossless", nil, func(context.Context, int64, string) (*netease.Stream, error) {
		cancel()
		return &netease.Stream{URL: "http://audio.invalid/cancelled"}, nil
	}, nil, nil)
	if dl != nil || !errors.Is(err, context.Canceled) {
		if dl != nil {
			dl.Close()
		}
		t.Fatal("late URL response started an obsolete audio download")
	}
}
