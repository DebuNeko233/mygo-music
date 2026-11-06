package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

type analysisTransport func(*http.Request) (*http.Response, error)

func (f analysisTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func analysisHTTP(t *testing.T, data []byte) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = analysisTransport(func(r *http.Request) (*http.Response, error) {
		body := data
		if r.URL.Host != "fixture.invalid" {
			body = []byte(`{"code":200}`)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
}

// Confirmed song restrictions keep the original skip behavior after transient failures are reclassified.
func TestRestrictedSongsStillSkip(t *testing.T) {
	analysisHTTP(t, nil)
	old := songURL
	var asked []int64
	songURL = func(id int64, _ string) (*netease.Stream, error) {
		// The queue loads these serially; each request posts its result before another starts.
		asked = append(asked, id)
		return nil, netease.ErrUnavailable
	}
	t.Cleanup(func() { songURL = old })
	ch, p := newLoadPlayer(t, nil)
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 0)
	pump(t, ch, "all three songs to be skipped", func() bool { return p.Current().ID == 3 && !p.Loading() && !p.Playing() })
	if !slices.Equal(asked, []int64{1, 2, 3}) || p.out != nil {
		t.Fatalf("requests %v, audio player %v", asked, p.out)
	}
	t.Logf("automatic playback requested %v and stopped on song %d", asked, p.Current().ID)
}

// A corrupt first FLAC frame must remain a decoder failure when the end callback checks it.
func TestDecoderFailureIsNotNaturalEnd(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	start, err := flacDataStart(downloaded(b))
	if err != nil {
		t.Fatal(err)
	}
	b[start] = 0 // Valid metadata, invalid frame sync: no first sample can decode.
	s, err := openStream(downloaded(b), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, decodeErr := s.Read(make([]float32, 4096))
	if decodeErr == nil || n != 0 {
		t.Fatalf("got %d samples, error %v", n, decodeErr)
	}
	var decoded *decodeError
	if !errors.As(s.cut(), &decoded) {
		t.Fatalf("decoder error was lost: %v", s.cut())
	}
	t.Logf("decoded samples=%d, decoder error=%v, cut()=%v (preserved for recovery)", n, decodeErr, s.cut())
}

// Decoder end callbacks must retry and stop on the selected song instead of silently advancing.
func TestCorruptFLACKeepsSelectedSong(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	start, err := flacDataStart(downloaded(b))
	if err != nil {
		t.Fatal(err)
	}
	b[start] = 0
	analysisHTTP(t, b)
	old := songURL
	songURL = func(id int64, _ string) (*netease.Stream, error) {
		if id == 3 {
			return nil, errors.New("stop the test on song 3")
		}
		return &netease.Stream{URL: fmt.Sprintf("https://fixture.invalid/%d", id), Type: "flac", Level: "lossless", Size: int64(len(b))}, nil
	}
	t.Cleanup(func() { songURL = old })
	ch, p := newLoadPlayer(t, nil)
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}, {ID: 3, Duration: 5000}}, 0)
	pump(t, ch, "decoder retries to stop on the selected song", func() bool { return !p.Playing() && !p.Loading() && p.Err() != "" })
	if p.Current().ID != 1 || p.tries != loadTries {
		t.Fatalf("song=%d retries=%d error=%q", p.Current().ID, p.tries, p.Err())
	}
	t.Logf("stayed on song %d after %d retries: %s", p.Current().ID, p.tries, p.Err())
}

// A clean HTTP EOF before the expected song size must be classified as an interrupted download.
func TestShortHTTPBodyIsInterrupted(t *testing.T) {
	analysisHTTP(t, []byte("short"))
	d := startDownload("https://fixture.invalid/short", 100, nil, nil)
	defer d.Close()
	got := d.waitFor(math.MaxInt64)
	if got != 5 || !errors.Is(d.Err(), io.ErrUnexpectedEOF) {
		t.Fatalf("bytes=%d error=%v", got, d.Err())
	}
	t.Logf("received %d of %d expected bytes, download error=%v", got, d.size, d.Err())
}

// Range downloads count their starting byte when distinguishing a complete suffix from a truncated one.
func TestRangeDownloadEndChecksWholeSongSize(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		interrupted bool
	}{
		{"complete suffix", "12345", false}, {"short suffix", "12", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := http.DefaultTransport
			http.DefaultTransport = analysisTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Range") != "bytes=3-" {
					t.Errorf("range=%q", r.Header.Get("Range"))
				}
				return &http.Response{StatusCode: http.StatusPartialContent, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(tc.body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = old })
			ctx, cancel := context.WithCancel(context.Background())
			d := &download{url: "https://fixture.invalid/range", base: 3, size: 8, runs: 1, users: 2, cancel: cancel, finished: make(chan struct{})}
			d.cond.L = &d.mu
			defer d.Close()
			go d.run(ctx, 3, 1, time.Second)
			d.waitFor(math.MaxInt64)
			<-d.finished
			if errors.Is(d.Err(), io.ErrUnexpectedEOF) != tc.interrupted {
				t.Fatalf("error=%v", d.Err())
			}
		})
	}
}

// A valid FLAC must still end naturally, while a truncated frame remains an error.
func TestFLACEndDistinguishesTruncation(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		bad  bool
	}{
		{"whole", b, false}, {"truncated", b[:len(b)/2], true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := openStream(downloaded(tc.data), 5000)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			buf := make([]float32, 4096)
			for {
				n, err := s.Read(buf)
				if err != nil || n == 0 {
					break
				}
			}
			if (s.cut() != nil) != tc.bad {
				t.Fatalf("cut=%v", s.cut())
			}
		})
	}
}

// Rejecting a corrupt cache entry must fetch a fresh song, without its old reader later deleting the replacement.
func TestCorruptCachedSongIsDownloadedAgain(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	analysisHTTP(t, b)
	bad := append([]byte(nil), b...)
	start, err := flacDataStart(downloaded(bad))
	if err != nil {
		t.Fatal(err)
	}
	bad[start] = 0
	dir := t.TempDir()
	c := NewCache(dir, 1<<30)
	st := &netease.Stream{URL: "https://fixture.invalid/song", Size: int64(len(b)), Type: "flac", Level: "lossless"}
	meta, _ := json.Marshal(st)
	path := filepath.Join(dir, "1-lossless.flac")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1-lossless.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	dl, _, ok := c.lookup(1, "lossless")
	if !ok {
		t.Fatal("cache fixture was not found")
	}
	broken, err := openStream(dl, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer broken.Close()
	if _, err := broken.Read(make([]float32, 4096)); err == nil {
		t.Fatal("corrupt fixture decoded")
	}
	dl.discard()
	var asked atomic.Int64
	p := New(func(func()) {})
	p.Cache = c
	fresh, _, err := p.fetch(netease.Track{ID: 1}, "lossless", nil, func(int64, string) (*netease.Stream, error) { asked.Add(1); return st, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	fresh.waitFor(math.MaxInt64)
	if asked.Load() != 1 || fresh.Err() != nil {
		t.Fatalf("requests=%d error=%v", asked.Load(), fresh.Err())
	}
	broken.Close()
	kept, _, ok := c.lookup(1, "lossless")
	if !ok {
		t.Fatal("old reader deleted the newly downloaded song")
	}
	defer kept.Close()
	good, err := openStream(kept, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	if n, err := good.Read(make([]float32, 4096)); n == 0 || err != nil {
		t.Fatalf("fresh cache: samples=%d error=%v", n, err)
	}
}
