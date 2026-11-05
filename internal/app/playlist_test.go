package app

import (
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

// A playlist asked to play while it loads plays once it is here, rather than
// the second ask being dropped and the button doing nothing: the page's card
// and its play button both ask for it.
func TestPlaylistPlaysWhenItLands(t *testing.T) {
	asked := make(chan int64, 4)
	answer := make(chan struct{})
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		asked <- id
		<-answer
		return &netease.Playlist{ID: id, Tracks: []netease.Track{
			{ID: 1, Name: "一", Duration: 200000}, {ID: 2, Name: "二", Duration: 200000},
		}}, nil
	}
	defer func() { playlistDetail = netease.PlaylistDetail }()
	posts := make(chan func(), 4)
	testPost = func(fn func()) { posts <- fn }
	defer func() { testPost = nil }()

	a := newTestApp()
	a.settings = defaultSettings()
	a.loadPlaylist(7, false) // the page, loading
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("the playlist wasn't asked for")
	}
	a.loadPlaylist(7, true) // its play button, while it loads
	close(answer)
	var landed func()
	select {
	case landed = <-posts:
	case <-time.After(time.Second):
		t.Fatal("the playlist never landed")
	}
	landed()
	if a.loadingPl[7] || a.pl.Current() == nil || a.pl.Current().ID != 1 {
		t.Fatalf("playing %+v, still loading %v", a.pl.Current(), a.loadingPl[7])
	}
	if q := a.pl.Queue(); len(q) != 2 {
		t.Errorf("queued %d tracks", len(q))
	}
}
