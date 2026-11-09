package player

import (
	"netease-music-mygo/internal/netease"
	"testing"
	"time"
)

// Offline audio errors follow connection changes without starting playback,
// moving the selected track or losing position, including a second disconnect.
func TestConnectionStatusUpdatesAudioError(t *testing.T) {
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	p := New(func(func()) {})
	p.Cue([]netease.Track{{ID: 1}})
	p.pending = 123
	p.failed(0, 0, *p.track, p.pending, netease.ErrOffline)
	offline := p.Err()
	netease.SetOffline(false)
	online := p.Err()
	if offline == "" || online == "" || !p.PlaybackRetryable() || p.Playing() || p.Loading() || p.Current().ID != 1 || p.pending != 123 {
		t.Fatal("reconnect retained offline state or changed playback")
	}
	netease.SetOffline(true)
	if p.Err() != offline || p.PlaybackRetryable() {
		t.Fatal("second disconnect kept an online status")
	}
	netease.SetOffline(false)
	p.failed(0, 0, *p.track, p.pending, netease.ErrNotLoggedIn)
	auth := p.Err()
	netease.SetOffline(true)
	if p.Err() != auth || p.PlaybackRetryable() {
		t.Fatal("connection changes replaced an unrelated login failure")
	}
}

// Explicit playback recovery reloads the same queue entry at its saved
// position; connection changes alone do not ask for audio or start it.
func TestConnectionStatusPlaybackRetryPreservesPosition(t *testing.T) {
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	posts := make(chan func(), 8)
	p := New(func(f func()) { posts <- f })
	p.Cue([]netease.Track{{ID: 7}, {ID: 8}})
	p.pending = 23 * time.Second
	p.failed(0, 0, *p.track, p.pending, netease.ErrOffline)
	ask := songURL
	t.Cleanup(func() { songURL = ask })
	called := make(chan int64, 1)
	songURL = func(id int64, _ string) (*netease.Stream, error) {
		called <- id
		return nil, netease.ErrNotLoggedIn
	}
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	netease.SetOffline(false)
	if !p.PlaybackRetryable() || p.Playing() || p.Loading() || len(called) != 0 {
		t.Fatal("reconnect started playback without user input")
	}
	p.TogglePlay()
	if !p.Loading() || p.PlaybackRetryable() || p.Current().ID != 7 || p.pending != 23*time.Second {
		t.Fatal("explicit retry changed the song or lost its saved position")
	}
	if id := <-called; id != 7 {
		t.Fatal("retry asked for another queue entry")
	}
	pump(t, posts, "explicit retry to finish", func() bool { return !p.Loading() && !p.MetadataLoading() })
	if p.PlaybackRetryable() || p.Playing() || p.Current().ID != 7 || p.pending != 23*time.Second {
		t.Fatal("failed retry kept offline recovery or changed playback state")
	}
}

// A cancelled request may finish after reconnection; its completion must not
// put offline text back into the online player's audio or lyrics state.
func TestConnectionStatusRejectsStaleOfflineLyricsText(t *testing.T) {
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(false) })
	posts := make(chan func(), 4)
	started, release := make(chan struct{}), make(chan struct{})
	p := New(func(f func()) { posts <- f })
	p.Cue([]netease.Track{{ID: 1}})
	p.NetworkOffline()
	offline := p.LyricsError()
	p.FetchLyrics = func(int64) (string, string, string, error) {
		close(started)
		<-release
		return "", "", "", netease.ErrOffline
	}
	p.RetryMetadata()
	<-started
	netease.SetOffline(false)
	if p.LyricsError() == offline {
		t.Fatal("in-flight lyrics kept offline text after reconnect")
	}
	close(release)
	(<-posts)()
	if p.LyricsError() == offline || p.LyricsError() == "" || p.MetadataLoading() {
		t.Fatal("late failure restored offline text or stayed busy")
	}
	p.failed(0, 0, *p.track, 0, netease.ErrOffline)
	if !p.PlaybackRetryable() {
		t.Fatal("late audio failure lost reconnect recovery")
	}
	netease.SetOffline(true)
	if p.PlaybackRetryable() {
		t.Fatal("late audio failure ignored actual connection")
	}
}
