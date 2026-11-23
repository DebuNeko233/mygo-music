package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func playlistPerfApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.route = route{Page: pagePlaylist, ID: 7}
	pl := &netease.Playlist{ID: 7, Name: "大型歌单", Tracks: make([]netease.Track, 8000)}
	for i := range pl.Tracks {
		pl.Tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("歌曲 %04d", i+1), Duration: 180000}
	}
	a.library.Playlists[7] = pl
	a.library.Refresh.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	a.touchPlaylist(7)
	return a
}

// Large playlist view builds must keep borrowing tracks rather than copying all records each frame.
func BenchmarkPlaylistMVVMFrame(b *testing.B) {
	a := playlistPerfApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}

// Reopening a fresh playlist must not start work or allocate service closures.
func BenchmarkPlaylistMVVMFresh(b *testing.B) {
	a := playlistPerfApp()
	b.ReportAllocs()
	for b.Loop() {
		a.loadPlaylist(7, false)
	}
}

// Revalidation measures copying, comparing and publishing 8000 songs without disk/network latency.
func BenchmarkPlaylistMVVMRefresh(b *testing.B) {
	a := playlistPerfApp()
	posts := testPosts(a)
	pl := copyPlaylist(*a.library.Playlists[7])
	old := playlistDetail
	b.Cleanup(func() { playlistDetail = old })
	playlistDetail = func(int64) (*netease.Playlist, error) { return &pl, nil }
	b.ReportAllocs()
	for b.Loop() {
		a.playlistRequest(7).At = time.Time{}
		a.loadPlaylist(7, false)
		(<-posts)()
	}
}

// A resting playlist must not poll or request redraws merely to keep its snapshot fresh.
func TestPlaylistMVVMIdleFrameBudget(t *testing.T) {
	a := playlistPerfApp()
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	before := builds
	tt.Advance(2 * time.Second)
	if builds != before {
		t.Fatalf("idle playlist requested %d extra builds", builds-before)
	}
}
