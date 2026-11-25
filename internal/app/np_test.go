/*
验证正在播放页的层级、时钟、队列、标签快捷键与基本播放控制。
*/

package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
)

// Escape closes Now Playing, which goes out as one layer: mid-way it still
// covers the page and the sidebar alike.
func TestNowPlayingClosesAsOneLayer(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 1, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	a.pl.SetLyrics(nil)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetDark(false)
	a.playbackState.Open = true
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find("专辑封面"); !ok {
		t.Fatal("now playing did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.playbackState.Open {
		t.Fatal("escape left now playing open")
	}
	// Mid-way out, it still covers the page and the sidebar alike.
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	img := tt.Image()
	sx, sy := img.Bounds().Dx(), img.Bounds().Dy()
	dark := func(x, y float32) bool {
		r, g, b, _ := img.At(int(x*float32(sx)/1200), int(y*float32(sy)/800)).RGBA()
		return (r+g+b)/3 < 0x9000
	}
	if !dark(900, 400) || !dark(100, 400) {
		t.Errorf("page %v, sidebar %v: the closing layer should cover both", dark(900, 400), dark(100, 400))
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if _, ok := tt.Find("专辑封面"); ok {
		t.Error("now playing still shows after closing")
	}
}

// A settled Now Playing page rebuilt its times only with its lyrics or seek
// bar, which could leave both clocks unchanged for several seconds.
func TestNowPlayingClockTicksEverySecond(t *testing.T) {
	for _, tc := range []struct {
		name string
		pane int
	}{
		{"lyrics", playbackview.PaneLyrics},
		{"comments", playbackview.PaneComments},
		{"queue", playbackview.PaneQueue},
		{"stage", playbackview.PaneStage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.pl.Cue([]netease.Track{{ID: 1, Name: "宝船", Duration: 200000}})
			a.pl.SetLyrics(nil)
			out := newSilentAudio()
			out.Play()
			a.pl.Attach(out)
			defer out.Close()
			a.playbackState.Open, a.playbackState.Pane = true, tc.pane
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) {
				builds++
				a.view(c)
			}, 1200, 800)
			tt.Advance(playbackview.NPSettle)
			before := builds
			// The silent source stays at zero, so tickTimes schedules its
			// next build one second plus the 10 ms boundary allowance later.
			tt.Advance(time.Second + 10*time.Millisecond)
			if got := builds - before; got == 0 {
				t.Errorf("pane %d did not rebuild its times for a second", tc.pane)
			}
		})
	}
}

// Picking a song from the queue plays it and leaves the queue listed as it
// was, songs before it and all.
func TestQueueStaysAsATrackIsPlayed(t *testing.T) {
	a := newTestApp()
	var tracks []netease.Track
	for i := range 12 {
		tracks = append(tracks, netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("歌%02d", i), Duration: 200000})
	}
	a.pl.Cue(tracks)
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneQueue
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("歌05"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.pl.Current().Name; got != "歌05" {
		t.Fatalf("playing %q, want 歌05", got)
	}
	// The tracks before it are still listed, as they were.
	for _, s := range []string{"歌01", "歌04", "歌06", "歌09"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
}

// The app chooses Now Playing's pane before it makes the player: it
// crashed on start when the choice asked the player what played.
func TestLyricsPaneBeforePlayer(t *testing.T) {
	a := &app{settings: settings{LyricsStage: true}}
	if pane := playbackview.LyricsPaneChosen(a.settings.LyricsStage); pane != playbackview.PaneStage {
		t.Errorf("pane %d", pane)
	}
}

// 上一首 plays the song before, however far this one has played: a few
// seconds in, it went back to this one's start, and took a second click.
func TestPrevGoesBackASong(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一", Duration: 200000}, {ID: 2, Name: "二", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.pl.Hold(30 * time.Second)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("上一首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if id := a.pl.Current().ID; id != 2 {
		t.Errorf("playing %d, want the one before, 2", id)
	}
}

// Now Playing holds still as the quality's badge goes, while the next song
// loads, and comes back: the row of times grew with it, moving the column.
func TestNowPlayingHoldsStillWithoutBadge(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 1200, 800)
	var at []float32
	for _, st := range []*netease.Stream{{Level: "lossless"}, nil} {
		a.pl.SetStream(st)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		r, ok := tt.Find("专辑封面")
		if !ok {
			t.Fatal("no cover")
		}
		at = append(at, r.Y)
	}
	if at[0] != at[1] {
		t.Errorf("the cover moved from %v to %v as the badge went", at[0], at[1])
	}
}

// ⌘ and a number choose Now Playing's tab in that place, for a song and
// for an episode, whose tabs are others: its last is the comments, wherever
// the captions it may have put it.
func TestCmdNumberChoosesTab(t *testing.T) {
	for _, c := range []struct {
		track netease.Track
		keys  []int // from 1, 0 for the last tab
		panes []int
	}{
		{netease.Track{ID: 1, Name: "宝船", Duration: 200000}, []int{4, 2, 1}, []int{playbackview.PaneWall, playbackview.PaneStage, playbackview.PaneLyrics}},
		{netease.Track{ID: 2, Name: "节目", ProgramID: 9, Duration: 200000}, []int{0, 1}, []int{playbackview.PaneComments, playbackview.PaneProgram}},
	} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.pl.Cue([]netease.Track{c.track})
		a.pl.SetLyrics(nil)
		a.playbackState.Open = true
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		for i, n := range c.keys {
			if n == 0 {
				n = len(a.views.playback.NPTabs())
			}
			tt.Key(ui.Cmd, ui.Key1+ui.Key(n-1))
			tt.Frame()
			if a.playbackState.Pane != c.panes[i] {
				t.Errorf("%s: ⌘%d chose pane %d, want %d", c.track.Name, n, a.playbackState.Pane, c.panes[i])
			}
		}
	}
}

// Now Playing keeps the navigation in place across panes and centered
// on the same axis as comment counts change.
func TestNowPlayingTabsStayPut(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.playbackState.Open = true
	tt := ui.NewTester(a.view, 1200, 800)
	settle := func() {
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
	}
	settle()
	first, ok := tt.Find("歌词")
	if !ok {
		t.Fatal("no 歌词 tab")
	}
	for _, tab := range []playbackview.NPTab{{Pane: playbackview.PaneStage, Label: "律动"}, {Pane: playbackview.PaneQueue, Label: "待播清单"}, {Pane: playbackview.PaneWall, Label: "选歌"}, {Pane: playbackview.PaneLyrics, Label: "歌词"}} {
		if err := tt.Click(tab.Label); err != nil {
			t.Fatal(err)
		}
		settle()
		if a.playbackState.Pane != tab.Pane {
			t.Fatalf("%s chose pane %d, want %d", tab.Label, a.playbackState.Pane, tab.Pane)
		}
		r, ok := tt.Find("歌词")
		if !ok {
			t.Fatalf("no 歌词 tab on %s", tab.Label)
		}
		if r.X != first.X || r.Y != first.Y {
			t.Errorf("on %s the 歌词 tab is at %v,%v, was at %v,%v", tab.Label, r.X, r.Y, first.X, first.Y)
		}
	}
	for _, pane := range []int{playbackview.PaneStage, playbackview.PaneLyrics} {
		a.playbackState.Pane = pane
		a.comments.Loaded, a.comments.Total = true, 0
		settle()
		start, startOK := tt.Find("歌词")
		end, endOK := tt.Find("评论")
		if !startOK || !endOK {
			t.Fatalf("missing navigation tabs in pane %d", pane)
		}
		axis := (start.X + end.X + end.W) / 2
		a.comments.Total = 12345678
		settle()
		start, startOK = tt.Find("歌词")
		end, endOK = tt.Find(a.commentsTab())
		if !startOK || !endOK {
			t.Fatalf("missing navigation tabs after the comment count changed in pane %d", pane)
		}
		if got := (start.X + end.X + end.W) / 2; math.Abs(float64(got-axis)) > 1 || start.Y != first.Y {
			t.Errorf("in pane %d, the comment count moved the navigation axis from %v to %v", pane, axis, got)
		}
	}
}
