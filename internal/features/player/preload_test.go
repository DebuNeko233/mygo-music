/*
验证下一首预取、网络重试、风控退避及中断下载的恢复策略。
*/

package player

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// A cache miss prepares the next URL and leaves the audio download for playback.
func TestPrefetchNext(t *testing.T) {
	b := song(t)
	srv, hits := songServer(t, b)
	var urls atomic.Int64
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if id == 2 {
			urls.Add(1)
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	t.Cleanup(func() { songURL = old })
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pump(t, ch, "the next track's URL", func() bool { return nextPrepared(p) })
	if p.ahead.dl != nil || hits(2) != 0 || urls.Load() != 1 {
		t.Fatal("a cache miss downloaded audio before the track played")
	}
	p.Next()
	pump(t, ch, "the next track playing", func() bool { return p.out != nil && p.Current().ID == 2 })
	if urls.Load() != 1 || hits(2) != 1 {
		t.Fatalf("playback used %d URL requests and %d audio requests", urls.Load(), hits(2))
	}
	if p.Loading() {
		t.Error("still says it loads, as it plays")
	}
}

func nextPrepared(p *Player) bool {
	if p.ahead != nil {
		select {
		case <-p.ahead.ready:
			return p.ahead.info != nil
		default:
		}
	}
	return false
}

// Switching while prefetch asks for a URL must use the same request and download.
func TestNextTakesPendingPrefetch(t *testing.T) {
	b := song(t)
	body := make(chan struct{})
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		if r.URL.Path != "/2" {
			w.Write(b)
			return
		}
		downloads.Add(1)
		w.Write(b[:len(b)/2])
		w.(http.Flusher).Flush()
		select {
		case <-body:
			w.Write(b[len(b)/2:])
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	defer close(body)
	asked := make(chan struct{})
	release := make(chan struct{})
	releaseURL := sync.OnceFunc(func() { close(release) })
	defer releaseURL()
	var calls atomic.Int64
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if id == 2 && calls.Add(1) == 1 {
			close(asked)
			<-release
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	t.Cleanup(func() { songURL = old })
	ch, p := newLoadPlayer(t, NewCache(t.TempDir(), 1<<30))
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)
	pump(t, ch, "the pending URL request", func() bool {
		select {
		case <-asked:
			return true
		default:
			return false
		}
	})
	p.Next()
	releaseURL()
	pump(t, ch, "the next track playing", func() bool { return p.out != nil && p.Current().ID == 2 })
	if n, gets := calls.Load(), downloads.Load(); n != 1 || gets != 1 {
		t.Fatalf("the next track made %d URL requests and %d downloads", n, gets)
	}
}

// Prefetch used to download the next track again even when it was already cached.
func TestPrefetchUsesCachedTrack(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<30)
	b := song(t)
	st := &netease.Stream{Level: "lossless", Type: "mp3", Size: int64(len(b))}
	part := filepath.Join(c.dir, "song.part")
	if err := os.WriteFile(part, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.keep(2, st.Level, st, part, st.Size); !ok {
		t.Fatal("could not keep the next track")
	}
	p := &Player{Cache: c, post: func(func()) {}}
	defer p.dropAhead()
	p.startPrefetch(2, "exhigh", nil, func(context.Context, int64, string) (*netease.Stream, error) {
		return nil, netease.ErrUnavailable
	}, 0)
	a := p.ahead
	<-a.ready
	if a.dl == nil || a.info.Level != st.Level {
		t.Fatal("the cached track was not ready at the requested quality")
	}
}

// A track that does not load is asked for again, rather than skipped as if
// it could not play: a slow or dropped connection is not an unavailable
// song.
func TestLoadRetries(t *testing.T) {
	b := song(t)
	srv, _ := songServer(t, b)
	old := songURL
	var n atomic.Int64
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if n.Add(1) <= 2 {
			return nil, errors.New("connection dropped")
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	defer func() { songURL = old }()
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pump(t, ch, "the track to load", func() bool { return p.out != nil })
	if id := p.Current().ID; id != 1 {
		t.Errorf("skipped to track %d", id)
	}
	if n := n.Load(); n < 3 {
		t.Errorf("asked %d times", n)
	}
}

// Risk control waits six seconds when the song is due, ten when it is only
// being fetched ahead; an ordinary failure keeps the existing backoff.
func TestRiskControlUsesLongerRetryGaps(t *testing.T) {
	for _, tc := range []struct {
		err  error
		what func(int, error) time.Duration
		want time.Duration
	}{
		{netease.ErrRiskControl, loadGap, 6 * time.Second},
		{errors.New("connection dropped"), loadGap, 400 * time.Millisecond},
		{netease.ErrRiskControl, prefetchGap, 10 * time.Second},
		{errors.New("connection dropped"), prefetchGap, 400 * time.Millisecond},
	} {
		if got := tc.what(1, tc.err); got != tc.want {
			t.Errorf("retry gap for %v: %s, want %s", tc.err, got, tc.want)
		}
	}
}

// Risk control retries a prefetch once, rather than keeping an account-level
// refusal active with requests until the song ends.
func TestRiskControlPrefetchIsLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var asks atomic.Int64
		ch := make(chan func(), 64)
		p := New(func(fn func()) { ch <- fn })
		Cue(p, []netease.Track{{ID: 1}, {ID: 2}})
		Hold(p, time.Second)
		ask := func(context.Context, int64, string) (*netease.Stream, error) {
			asks.Add(1)
			return nil, netease.ErrRiskControl
		}
		p.startPrefetch(2, "exhigh", nil, ask, 0)
		synctest.Wait()
		time.Sleep(10*time.Second - time.Millisecond)
		synctest.Wait()
		if run(ch) {
			t.Fatal("prefetch retried before its risk-control delay")
		}
		select {
		case fn := <-ch:
			fn()
		case <-time.After(time.Second):
			t.Fatal("prefetch retry was never posted")
		}
		synctest.Wait()
		time.Sleep(10 * time.Second)
		run(ch)
		synctest.Wait()
		if got := asks.Load(); got != 2 {
			t.Fatalf("risk control kept requesting: %d requests", got)
		}
	})
}

// A prefetch waiting out risk control stops when the song has moved on.
func TestRiskControlPrefetchStopsWhenTheSongMovesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var asks atomic.Int64
		ch := make(chan func(), 64)
		p := New(func(fn func()) { ch <- fn })
		Cue(p, []netease.Track{{ID: 1}, {ID: 2}})
		Hold(p, time.Second)
		ask := func(context.Context, int64, string) (*netease.Stream, error) {
			asks.Add(1)
			return nil, netease.ErrRiskControl
		}
		p.startPrefetch(2, "exhigh", nil, ask, 0)
		synctest.Wait()
		p.gen++ // Next, previous, or another queue invalidates this wait.
		select {
		case fn := <-ch:
			fn() // Run the original retry after its timer fires.
		case <-time.After(11 * time.Second):
			t.Fatal("prefetch retry was never posted")
		}
		synctest.Wait()
		if got := asks.Load(); got != 1 {
			t.Errorf("the old song asked again: %d requests", got)
		}
	})
}

// Temporary errors on one song leave the next one able to load.
func TestTemporaryFailuresDoNotExhaustLaterSongs(t *testing.T) {
	b := bytes.Repeat(song(t), 15) // Long enough for the risk-check waits.
	srv, _ := songServer(t, b)
	var asks sync.Map
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		v, _ := asks.LoadOrStore(id, new(atomic.Int64))
		if v.(*atomic.Int64).Add(1) == 1 {
			return nil, netease.ErrRiskControl
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	defer func() { songURL = old }()
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5 * 60 * 1000}, {ID: 2, Duration: 5 * 60 * 1000}}, 0)
	pumpFor(t, ch, "the first song after a temporary refusal", 15*time.Second, func() bool { return p.out != nil })
	if p.err != "" || p.Current().ID != 1 {
		t.Fatalf("did not recover: %s", p.Err())
	}
	pumpFor(t, ch, "the next song's URL after the same refusal", 15*time.Second, func() bool { return nextPrepared(p) })
	p.Next()
	pump(t, ch, "the next song playing", func() bool { return p.Ready() && p.Current().ID == 2 })
}

// A track that does not load at all stops there, with what went wrong on
// show, rather than playing the one after it.
func TestLoadFailureDoesNotSkip(t *testing.T) {
	b := song(t)
	_, hits := songServer(t, b)
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		return nil, errors.New("connection dropped")
	}
	defer func() { songURL = old }()
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pumpFor(t, ch, "the load to give up", 15*time.Second, func() bool { return !p.loading && p.err != "" })
	if id := p.Current().ID; id != 1 {
		t.Errorf("skipped to track %d", id)
	}
	if p.Playing() {
		t.Error("plays nothing, as if playing")
	}
	if n := hits(2); n != 0 {
		t.Errorf("asked for the next track %d times", n)
	}
}

// cutServer serves the song, cut off halfway, as a flaky connection does.
func cutServer(t *testing.T, b []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b[:len(b)/2])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cutStream returns what played of a song whose download was cut off
// halfway, as a stream of the player's: a song of id, from url.
func cutStream(t *testing.T, c *Cache, id int64, url string, size int) stream {
	t.Helper()
	dl := c.download(id, "exhigh", &netease.Stream{URL: url, Size: int64(size), Level: "exhigh", Type: "mp3"})
	dl.waitFor(int64(size))
	if dl.Err() == nil {
		t.Fatal("the cut download ended whole")
	}
	s, err := openStream(dl, 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() }) // what came of it, closed as a stream of the player's would be
	return s
}

// A download that failed is not shown as a complete song.
// A response with no length is not a song whose bytes have all arrived.
func TestDownloadWithoutALengthKeepsComing(t *testing.T) {
	b := song(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	d := startDownload(srv.URL, 0, nil, nil)
	d.waitFor(math.MaxInt64)
	if err := d.Err(); err != nil {
		t.Fatal(err)
	}
	if d.n != int64(len(b)) {
		t.Fatalf("downloaded %d of %d", d.n, len(b))
	}
	d.seekTo(10)
	d.waitFor(math.MaxInt64)
	if err := d.Err(); err != nil {
		t.Fatal("seek of a song whose length was unknown: ", err)
	}
}

func TestFailedDownloadHasNoCompleteProgress(t *testing.T) {
	b := song(t)
	cut := cutServer(t, b)
	d := startDownload(cut.URL, int64(len(b)), nil, nil)
	d.waitFor(math.MaxInt64)
	if d.Err() == nil {
		t.Fatal("cut download succeeded")
	}
	if got := d.Progress(); got != 0 {
		t.Fatalf("failed download showed progress %v", got)
	}
}

// A download cut off as it plays is asked for again, and the song goes on
// from there: it used to end where the bytes did, halfway through, and the
// next track played.
func TestCutOffRetries(t *testing.T) {
	b := song(t)
	whole, _ := songServer(t, b)
	cut := cutServer(t, b)
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", whole.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	defer func() { songURL = old }()

	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	Cue(p, []netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}})
	p.stream = cutStream(t, c, 1, cut.URL, len(b))

	p.cutOff() // what plays ended, and the download is why
	if id := p.Current().ID; id != 1 {
		t.Fatalf("moved on to track %d", id)
	}
	if p.tries != 1 {
		t.Errorf("tried %d times", p.tries)
	}
	pump(t, ch, "the track again", func() bool { return p.out != nil })
	if id := p.Current().ID; id != 1 {
		t.Errorf("plays track %d", id)
	}
}

// A song whose download keeps breaking off is left where it stopped, with
// what went wrong on show: its tries used to start over each time it loaded
// again, so a song that kept breaking off was retried for as long as the
// listener waited.
func TestCutOffStops(t *testing.T) {
	b := song(t)
	whole, _ := songServer(t, b)
	cut := cutServer(t, b)
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", whole.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	defer func() { songURL = old }()

	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	Cue(p, []netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}})
	// Broken off, asked for again, and broken off again: once for every try
	// it gets, and then it is left where it stopped.
	for i := int64(1); i <= loadTries+1; i++ {
		gen := p.gen
		p.stream = cutStream(t, c, 100+i, cut.URL, len(b))
		p.cutOff()
		if p.err != "" {
			break // left where it stopped
		}
		pump(t, ch, "the track again", func() bool { return p.gen != gen })
	}

	if p.Playing() {
		t.Error("plays on, as it cannot be had")
	}
	if p.err == "" {
		t.Error("does not say what went wrong")
	}
	if id := p.Current().ID; id != 1 {
		t.Errorf("moved on to track %d", id)
	}
}

// A song whose bytes all came in plays on to the next one when it ends.
func TestSongEndPlaysNext(t *testing.T) {
	b := song(t)
	srv, _ := songServer(t, b)
	serveURL(t, srv, len(b))
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	Cue(p, []netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}})
	s, err := openStream(downloaded(b), 5000)
	if err != nil {
		t.Fatal(err)
	}
	p.stream = s

	p.cutOff()
	pump(t, ch, "the next track", func() bool { return p.out != nil && p.Current().ID == 2 })
}

// Preparing the next track waits for the current download to publish its cache.
func TestPrefetchWaitsForThisOne(t *testing.T) {
	b := song(t)
	rest := make(chan struct{})
	release := sync.OnceFunc(func() { close(rest) })
	defer release()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		// Half of it, and the rest only when the test lets it come.
		w.Write(b[:len(b)/2])
		w.(http.Flusher).Flush()
		<-rest
		w.Write(b[len(b)/2:])
	}))
	t.Cleanup(srv.Close)
	var asks atomic.Int64
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if id == 2 {
			asks.Add(1)
		}
		return &netease.Stream{URL: srv.URL, Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	t.Cleanup(func() { songURL = old })
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pump(t, ch, "the track playing", func() bool { return p.out != nil })
	if n := asks.Load(); n != 0 {
		t.Errorf("asked for the next track %d times, as this one still comes in", n)
	}
	release()
	pump(t, ch, "the next playback URL", func() bool { return nextPrepared(p) })
	if asks.Load() != 1 {
		t.Fatalf("prepared the next URL %d times", asks.Load())
	}
}

// A transient failure preparing the next URL is retried without starting audio.
func TestPrefetchRetriesPlaybackURL(t *testing.T) {
	b := song(t)
	srv, hits := songServer(t, b)
	var attempts atomic.Int64
	old := songURL
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if id == 2 && attempts.Add(1) == 1 {
			return nil, errors.New("temporary network failure")
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	t.Cleanup(func() { songURL = old })
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pump(t, ch, "the retried playback URL", func() bool { return nextPrepared(p) })
	if attempts.Load() != 2 || p.ahead.dl != nil || hits(2) != 0 {
		t.Fatalf("prefetch made %d URL attempts and %d audio requests", attempts.Load(), hits(2))
	}
	p.Next()
	pump(t, ch, "the next track playing", func() bool { return p.out != nil && p.Current().ID == 2 })
}

// A track that fails to load three times in a row plays on the fourth ask:
// two tries a moment apart left the listener with a song that would have
// played had it been asked for once more.
func TestLoadSurvivesABriefOutage(t *testing.T) {
	b := song(t)
	srv, _ := songServer(t, b)
	old := songURL
	var n atomic.Int64
	songURL = func(_ context.Context, id int64, level string) (*netease.Stream, error) {
		if id == 1 && n.Add(1) <= 3 {
			return nil, errors.New("connection dropped")
		}
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(len(b)), Level: level, Type: "mp3"}, nil
	}
	defer func() { songURL = old }()
	c := NewCache(t.TempDir(), 1<<30)
	ch, p := newLoadPlayer(t, c)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)

	pump(t, ch, "the track to load", func() bool { return p.out != nil })
	if id := p.Current().ID; id != 1 {
		t.Errorf("skipped to track %d", id)
	}
	if n := n.Load(); n < 4 {
		t.Errorf("asked %d times", n)
	}
}
