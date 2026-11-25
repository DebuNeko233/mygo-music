package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// isolateUserData keeps settings, sessions and transcripts in the test directory.
func isolateUserData(t *testing.T) {
	t.Helper()
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
}

func newTestApp() *app {
	a := newApp()
	a.art = artcache.New(artcache.Options{Post: func(func()) {}})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}

// testPosts gives each app its own UI queue; install it before starting work.
func testPosts(a *app) chan func() {
	posts := make(chan func(), 64)
	a.postMu.Lock()
	a.postTo = func(fn func(), _ bool) { posts <- fn }
	a.postMu.Unlock()
	return posts
}

func shortLyricsApp(lines []lyrics.Line, w, h int) (*app, *ui.Tester) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricEdgeFade = false
	a.settings.HotComments = false
	a.pl.Cue([]netease.Track{{ID: 1, Duration: 200000}})
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		_, height := c.Size()
		ui.Column(c).Fill().Children(func() { a.views.playback.LyricsPane(c, a.pl.Current(), height, ui.Rect{}, 0) })
	}, w, h, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	return a, tt
}

// coverApp is Now Playing showing the second of three songs.
func coverApp(t *testing.T) (*app, *ui.Tester, ui.Rect) {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}, {ID: 3, Name: "三"}})
	a.pl.Next()
	a.pl.SetLyrics(nil)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 860, 560)
	// Without the tilt and the swing, which the CPU draws slowly in depth:
	// the gestures do as they would.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	r, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	return a, tt, r
}
