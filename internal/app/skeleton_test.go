package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// skeletonScenes are the views that load, as they show while they do.
var skeletonScenes = []struct {
	name string
	set  func(a *app)
}{
	{"home", func(a *app) {}},
	{"charts", func(a *app) { a.nav.Current = route{Page: pageCharts} }},
	{"daily", func(a *app) { a.nav.Current = route{Page: pageDaily} }},
	{"playlist", func(a *app) {
		a.nav.Current = route{Page: pagePlaylist, ID: 7}
		a.library.Loading[7] = true
	}},
	{"search", func(a *app) {
		a.nav.Current = route{Page: pageSearch, Query: "春日影", Kind: netease.SearchSongs}
		a.searches.Pages.Set(a.nav.Current, &viewmodel.SearchPage{Page: viewmodel.Page{Loading: true}})
	}},
	{"artists", func(a *app) {
		a.nav.Current = route{Page: pageSearch, Query: "春日影", Kind: netease.SearchArtists}
		a.searches.Pages.Set(a.nav.Current, &viewmodel.SearchPage{Page: viewmodel.Page{Loading: true}})
	}},
	{"login", func(a *app) { a.account.Login = viewmodel.LoginState{Open: true} }},
	{"comments", func(a *app) {
		a.playbackState.Open, a.playbackState.Pane = true, paneComments
		a.comments = commentState{track: 1, loading: true}
	}},
	{"more-comments", func(a *app) {
		a.playbackState.Open, a.playbackState.Pane = true, paneComments
		a.comments = commentState{track: 1, loading: true, loaded: true, total: 1, latest: []netease.Comment{{ID: 1, Content: "好听"}}}
	}},
	{"lyrics", func(a *app) { a.playbackState.Open, a.playbackState.Pane = true, paneLyrics }},
	{"stage", func(a *app) { a.playbackState.Open, a.playbackState.Pane = true, paneStage }},
}

func skeletonApp(set func(a *app)) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.home.Loading = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "春日影"}})
	set(a)
	return a
}

// With NM_FRAMES set, saves each skeleton in the light and the dark, the
// shimmer partway across.
func TestSkeletonFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, s := range skeletonScenes {
		for _, dark := range []bool{false, true} {
			a := skeletonApp(s.set)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Advance(450 * time.Millisecond)
			tt.Frame()
			name := s.name
			if dark {
				name += "-dark"
			}
			f, _ := os.Create(filepath.Join(dir, "skeleton-"+name+".png"))
			png.Encode(f, tt.Image())
			f.Close()
		}
	}
}
