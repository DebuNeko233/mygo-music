package player

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

// An uncached offline song ends loading immediately, with no queue skip or
// retry loop; the same song can still be retried explicitly after reconnect.
func TestOfflineUncachedSongStopsBuffering(t *testing.T) {
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	posts, p := newLoadPlayer(t, NewCache(t.TempDir(), 1<<20))
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	p.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}}, 0)
	pump(t, posts, "offline loading to finish", func() bool { return !p.Loading() && !p.MetadataLoading() })
	if p.Playing() || p.Current().ID != 1 || p.Err() == "" || p.tries != 0 || p.lyricsLoaded || p.LyricsError() == "" {
		t.Fatal("offline song stayed active, skipped, retried or pretended to have no lyrics")
	}
}

// A complete local stream opens without asking for a URL while offline.
func TestOfflineCompleteSongUsesCache(t *testing.T) {
	b := song(t)
	cache := NewCache(t.TempDir(), 1<<30)
	st := &netease.Stream{Level: "exhigh", Type: "mp3", Size: int64(len(b))}
	file := cache.part(st)
	if file == nil {
		t.Fatal("cannot create fixture")
	}
	file.Write(b)
	file.Close()
	cache.keep(1, "exhigh", st, file.Name(), int64(len(b)))
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	p := New(func(func()) {})
	p.Cache = cache
	_, stream, err := p.open(netease.Track{ID: 1}, "exhigh", nil, func(int64, string) (*netease.Stream, error) {
		t.Error("asked for cached audio URL")
		return nil, netease.ErrOffline
	}, cache)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
}

// Disconnecting during retry backoff stops its buffering state and invalidates
// its scheduled callback, preserving the current song and position.
func TestOfflineCancelsPlaybackBackoff(t *testing.T) {
	p := New(func(func()) {})
	p.track = &netease.Track{ID: 1}
	p.pending = 123
	p.loading, p.retrying, p.playing = true, true, true
	gen := p.gen
	p.NetworkOffline()
	if p.Loading() || p.Playing() || p.gen == gen || p.Current().ID != 1 || p.pending != 123 || p.Err() == "" {
		t.Fatal("offline backoff retained active buffering or lost position")
	}
}

// Offline lyric failure remains an error until a successful retry, never an
// instrumental result or an endlessly animated loading state.
func TestOfflineLyricsRecoverAfterFailure(t *testing.T) {
	posts := make(chan func(), 4)
	p := New(func(f func()) { posts <- f })
	p.track = &netease.Track{ID: 1}
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", netease.ErrOffline }
	p.RetryMetadata()
	(<-posts)()
	if p.MetadataLoading() || p.lyricsLoaded || p.LyricsError() == "" {
		t.Fatal("offline lyrics did not settle as unavailable")
	}
	p.FetchLyrics = func(int64) (string, string, string, error) { return "[00:01]Recovered", "", "", nil }
	p.RetryMetadata()
	if p.LyricsError() == "" {
		t.Fatal("background retry erased settled error before success")
	}
	(<-posts)()
	if !p.lyricsLoaded || p.LyricsError() != "" || len(p.lyrics) != 1 {
		t.Fatal("lyrics did not recover")
	}
}

// A stream decoder waiting for more bytes wakes on disconnect, rather than
// remaining blocked until the download's stall timeout.
func TestOfflineDownloadUnblocksReader(t *testing.T) {
	netease.SetOffline(false)
	t.Cleanup(func() { netease.SetOffline(false) })
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 100))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	dl := startDownload(server.URL, 1000, nil, nil)
	defer dl.Close()
	<-entered
	done := make(chan int64, 1)
	go func() { done <- dl.waitFor(1000) }()
	netease.SetOffline(true)
	select {
	case n := <-done:
		if n >= 1000 || !errors.Is(dl.Err(), netease.ErrOffline) {
			t.Fatal("disconnected stream did not expose its incomplete data")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("decoder kept waiting for network bytes")
	}
}

// Stopping an interrupted stream invalidates old metadata callbacks and also
// releases their busy flags, so reconnect can request missing metadata again.
func TestOfflineStoppedStreamCanRetryMetadata(t *testing.T) {
	p := New(func(func()) {})
	p.track = &netease.Track{ID: 1}
	p.lyricBusy, p.coverBusy = true, true
	p.NetworkOffline()
	p.stop()
	if p.MetadataLoading() || !p.MetadataPending() || p.LyricsError() == "" {
		t.Fatal("invalidated metadata stayed busy after stream stopped")
	}
}
