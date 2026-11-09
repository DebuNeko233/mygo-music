package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// An automatic retry of an uncached failed page retains its settled error;
// it must not replace it with an indefinitely animated loading skeleton.
func TestOfflinePageRetryKeepsSettledState(t *testing.T) {
	a, posts := offlineFixture(t)
	a.route = route{page: pagePlaylist, id: 9}
	playlistDetail = func(int64) (*netease.Playlist, error) { return nil, errors.New("offline") }
	a.loadPlaylist(9, false)
	refreshPost(t, posts)
	if a.loadingPl[9] || a.plErr[9] == "" {
		t.Fatal("missing page did not settle as failed")
	}
	hold := make(chan struct{})
	playlistDetail = func(int64) (*netease.Playlist, error) { <-hold; return nil, errors.New("offline") }
	a.networkChanged(mygo.NetworkStatus{})
	a.loadPlaylist(9, false)
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Error("background retry animated the failed page")
	}
	close(hold)
	refreshPost(t, posts)
}

// Offline lyrics in both the normal and stage panes are settled unavailable
// states, not instrumental results or loading animations.
func TestOfflineLyricsStopAnimating(t *testing.T) {
	for _, pane := range []int{paneLyrics, paneStage} {
		a, _ := offlineFixture(t)
		a.pl.Cue([]netease.Track{{ID: 1, Name: "Offline"}})
		a.networkChanged(mygo.NetworkStatus{})
		a.nowPlaying, a.npPane = true, pane
		tt := ui.NewTester(a.view, 1100, 800)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		tt.Advance(time.Second)
		_, loaded := a.pl.Lyrics()
		if loaded || a.pl.LyricsError() == "" || tt.FramePending() {
			t.Fatalf("pane %d retained a loading animation or classified failure as instrumental", pane)
		}
	}
}
