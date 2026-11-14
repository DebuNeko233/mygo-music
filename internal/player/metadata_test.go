package player

import (
	"netease-music-mygo/internal/netease"
	"testing"
)

// Metadata recovery does not start audio or change the current queue, pause or seek.
func TestMetadataRecoveryKeepsPlaybackState(t *testing.T) {
	posts := make(chan func(), 4)
	p := New(func(f func()) { posts <- f })
	tr := netease.Track{ID: 7}
	p.track = &tr
	p.pending = 123
	p.playing = false
	calls := 0
	p.FetchLyrics = func(int64) (string, string, string, error) { calls++; return "[00:01]Recovered", "", "", nil }
	p.RetryMetadata()
	p.RetryMetadata()
	(<-posts)()
	if !p.lyricsLoaded || len(p.lyrics) != 1 || p.lyrics[0].Text != "Recovered" || calls != 1 || p.pending != 123 || p.playing || p.track.ID != 7 {
		t.Fatal("metadata recovery altered playback or duplicated a request")
	}
}

// Lyrics returned after a song change cannot overwrite that song's metadata.
func TestMetadataRecoveryRejectsOldTrack(t *testing.T) {
	posts := make(chan func(), 4)
	p := New(func(f func()) { posts <- f })
	p.track = &netease.Track{ID: 1}
	p.FetchLyrics = func(int64) (string, string, string, error) { return "[00:01]Old", "", "", nil }
	p.RetryMetadata()
	p.gen++
	p.track = &netease.Track{ID: 2}
	(<-posts)()
	if p.lyricsLoaded || len(p.lyrics) != 0 {
		t.Fatal("old track replaced current lyrics")
	}
}

// A delayed cached result cannot undo lyrics freshly revalidated in the background.
func TestMetadataRevalidationWinsOverCache(t *testing.T) {
	posts := make(chan func(), 4)
	p := New(func(f func()) { posts <- f })
	p.track = &netease.Track{ID: 1}
	p.FetchLyrics = func(int64) (string, string, string, error) { return "[00:01]Cached", "", "", nil }
	p.RetryMetadata()
	complete := <-posts
	p.RefreshLyrics(1, "[00:01]Fresh", "", "")
	complete()
	if p.lyrics[0].Text != "Fresh" {
		t.Fatal("old cache overwrote fresh lyrics")
	}
}

// A permanent lyric failure must settle once, while the explicit refresh can still succeed.
func TestMetadataPermanentFailureStopsRecovery(t *testing.T) {
	posts := make(chan func(), 4)
	p := New(func(f func()) { posts <- f })
	p.track = &netease.Track{ID: 7}
	calls := 0
	p.FetchLyrics = func(int64) (string, string, string, error) { calls++; return "", "", "", netease.ErrNotLoggedIn }
	p.RetryMetadata()
	(<-posts)()
	p.RecoverMetadata()
	if calls != 1 || p.MetadataPending() || p.MetadataLoading() || !p.LyricsRetryable() {
		t.Fatal("permanent lyrics failure repeated or lost its refresh action")
	}
	p.FetchLyrics = func(int64) (string, string, string, error) { calls++; return "[00:01]Refreshed", "", "", nil }
	p.RetryMetadata()
	(<-posts)()
	if calls != 2 || !p.lyricsLoaded || p.LyricsError() != "" {
		t.Fatal("explicit lyric refresh did not recover")
	}
}

// Replacement lyrics recompute the language even if the track and number of lines stay the same.
func TestLyricLanguageAfterRefresh(t *testing.T) {
	p := New(func(func()) {})
	p.track = &netease.Track{ID: 1}
	p.RefreshLyrics(1, "[00:01]次のことば", "", "")
	if p.LyricLanguage() != "ja" {
		t.Fatal("Japanese lyrics did not select their font language")
	}
	p.RefreshLyrics(1, "[00:01]New lyric", "", "")
	if p.LyricLanguage() != "" {
		t.Fatal("metadata replacement retained the previous language")
	}
}
