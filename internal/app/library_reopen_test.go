package app

import (
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

// Reopening a previously cached playlist must refresh its contents while
// keeping the old page visible; it used to remain stale until a restart.
func TestReopenedPlaylistRefreshes(t *testing.T) {
	isolateUserData(t)
	original := playlistDetail
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		return &netease.Playlist{ID: id, Name: "Updated", Tracks: []netease.Track{{ID: 2}}}, nil
	}
	defer func() { playlistDetail = original }()
	a := newTestApp()
	posts := testPosts(a)
	old := &netease.Playlist{ID: 7, Name: "Old", Tracks: []netease.Track{{ID: 1}}}
	a.library.Playlists[7] = old
	a.pl.Cue([]netease.Track{{ID: 9}})
	a.open(route{Page: pagePlaylist, ID: 7})
	if a.library.Playlists[7] != old {
		t.Fatal("blanked the page while refreshing")
	}
	select {
	case f := <-posts:
		f()
	case <-time.After(2 * time.Second):
		t.Fatal("cached playlist was never asked for again")
	}
	if a.library.Playlists[7].Name != "Updated" || a.library.Playlists[7].Tracks[0].ID != 2 {
		t.Fatal("page stayed stale")
	}
	if a.pl.Queue()[0].ID != 9 {
		t.Fatal("refresh replaced the playing queue")
	}
}
