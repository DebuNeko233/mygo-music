package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// A cover slide moves through painter poses, then releases its outgoing
// layer and stops rebuilding. Count After wakeups as well as display frames.
func TestCoverSlideFrameBudget(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		a := newTestApp()
		a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
		builds := 0
		tt := ui.NewTesterWith(func(c *ui.Context) {
			builds++
			ui.Column(c).Fill().Center().Children(func() { a.npCover(c, a.pl.Current(), nil, 220) })
		}, 320, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
		a.pl.Next()
		tt.Frame()
		tt.Advance(time.Second)
		if a.npCoverState.switching || a.npCoverState.previous != nil {
			t.Fatal("the outgoing cover outlived its animation")
		}
		before := builds
		tt.Advance(time.Second)
		if n := builds - before; n != 0 {
			t.Errorf("reduced=%v: a resting cover rebuilt %d times", reduced, n)
		}
	}
}

// playingApp is an app playing a track with lyrics, at a position the test
// sets with pl.Seek, and the number of times its view was built.
func playingApp() (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	// A line every 10 seconds, sung over the first 5 of them.
	var lrc string
	for i := range 12 {
		lrc += "[0" + string(rune('0'+i*10/60)) + ":" + string(rune('0'+i*10%60/10)) + "0.00]一二三四五六\n"
	}
	lines := lyrics.Build(lrc, "", "")
	for i := range lines {
		l := &lines[i]
		l.End = l.At + 5*time.Second
		l.Words = lyrics.SpreadWords(l.Text, l.At, l.End)
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	return a, tt, &builds
}

// framesAt counts the frames the view asks for over a second, at 60 a
// second, with playback held at pos.
func framesAt(a *app, tt *ui.Tester, pos time.Duration) int {
	a.pl.Seek(pos)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	asked := 0
	for range 60 {
		tt.Advance(time.Second / 60)
		if tt.FramePending() {
			asked++
			tt.Frame()
		}
	}
	return asked
}

// Now Playing asks for frames only as fast as what it shows changes. The
// Tester runs timers as it settles a frame, so this counts only what does
// not change for a while: a frame log of the app (MYGO_FRAME_STATS) measures
// the rest.
func TestNowPlayingFrameBudget(t *testing.T) {
	a, tt, _ := playingApp()
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("a page under the player: %d frames a second", n)
	}
	// Its rows still, the wall asks for no more than the lyrics beside it
	// do: it moves only as it is moved.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.nowPlaying, a.npPane = true, paneLyrics
	lyricsFrames := framesAt(a, tt, 12*time.Second)
	a.npPane = paneWall
	if n := framesAt(a, tt, 12*time.Second); n > lyricsFrames+3 {
		t.Errorf("the wall, still: %d frames a second, the lyrics %d", n, lyricsFrames)
	}
	tt.SetPreferences(ui.Preferences{})
	a.npPane = paneStage
	a.settings.StageSparks = true
	for _, s := range stageStyles {
		a.settings.StageStyle = s.id
		if n := framesAt(a, tt, 18*time.Second); n > 12 {
			t.Errorf("the stage between lines, in %s: %d frames a second", s.label, n)
		}
	}
}

// Another song's backdrop fades in over the last one's, which is then let
// go, so that the backdrop stops asking for frames.
func TestBackdropFadeEnds(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.art.art[netease.Sized("a", 600)+"#backdrop"] = &artwork{tint: ui.RGB(200, 40, 40)}
	a.art.art[netease.Sized("b", 600)+"#backdrop"] = &artwork{tint: ui.RGB(40, 40, 200)}
	q := a.pl.Queue()
	q[1].Album.PicURL, q[2].Album.PicURL = "a", "b"
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	if a.backdropCache.before == nil {
		t.Fatal("the backdrop changed songs without fading")
	}
	tt.Advance(backdropFade + 100*time.Millisecond)
	tt.Frame()
	if a.backdropCache.before != nil {
		t.Error("the last song's backdrop still fades after the fade")
	}
}

// Turning fog off discards its speed and bass immediately: the old easing
// state repainted the static background for about two seconds after a switch.
func TestStaticBackdropStopsFog(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Fog = true
	tt := ui.NewTester(func(c *ui.Context) { a.backdrop(c, nil, ui.RGB(50, 80, 140)) }, 240, 160)
	// Tester uses software rendering, so seed the moving GPU fog's state
	// before switching modes; no audio device or wall-clock phase is needed.
	bd := &a.backdropCache
	bd.speed, bd.last = 1, time.Unix(1, 0)
	bd.level.h[0], bd.level.last = 1, bd.last
	a.settings.Fog = false
	tt.Frame()
	at, canvas := bd.at, bd.canvas
	for range 120 {
		tt.Advance(backdropFrame)
		tt.Frame()
		// FramePending alone misses delayed Painter.After callbacks, so
		// also verify that the cache and paint timestamp never change.
		if bd.at != at || bd.canvas != canvas || bd.speed != 0 || bd.level != (barState{}) || !bd.last.IsZero() {
			t.Fatal("static background kept the fog's animation state or repainted")
		}
		if tt.FramePending() {
			t.Fatal("static background requested another frame")
		}
	}
}

// A paused playlist must stop scheduling frames with its glass locate
// button visible, after locating hides it, and after its header changes.
func TestPlaylistLocateFrameBudget(t *testing.T) {
	_, pl, tt := locateFixture(t, 199, false)
	for _, stage := range []string{"offscreen", "visible", "header changed"} {
		switch stage {
		case "visible":
			clickLocate(t, tt)
		case "header changed":
			pl.Description = "A refreshed playlist description."
			tt.Frame()
		}
		tt.Move(100, 100)
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
		asked := 0
		for range 30 {
			tt.Advance(time.Second / 60)
			if tt.FramePending() {
				asked++
				tt.Frame()
			}
		}
		if asked > 1 {
			t.Fatalf("%s idle locate requested %d frames", stage, asked)
		}
	}
}

// Centering short lyrics must settle after layout, rather than asking for
// another measurement frame throughout playback or after a resize.
func TestShortLyricsFrameBudget(t *testing.T) {
	lines := []lyrics.Line{{Text: "纯音乐，请欣赏"}}
	a, tt := shortLyricsApp(lines, 420, 600)
	tt.SetPreferences(ui.Preferences{})
	for _, size := range [][2]int{{420, 600}, {280, 350}, {420, 600}} {
		tt.SetSize(size[0], size[1])
		if n := framesAt(a, tt, 30*time.Second); n > 3 {
			t.Fatalf("%v: short lyrics requested %d idle frames", size, n)
		}
	}
}

func hotCommentStageClock(style string, reduced bool, pos time.Duration) (*app, *ui.Tester, *int) {
	a, _, _ := quietApp(pos)
	a.settings.StageStyle = style
	builds := 0
	var start time.Time
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		a.pl.Hold(pos + c.Now().Sub(start))
		ui.Column(c).Fill().Children(func() { a.stage(c, a.pl.Current(), ui.RGB(255, 100, 100)) })
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return a, tt, &builds
}

// Reduce Motion must cover hot-comment entry, replacement and exit too:
// comments appear centered immediately, without fades, movement or frame loops.
func TestStageHotCommentsReducedMotion(t *testing.T) {
	for _, style := range stageStyles {
		t.Run(style.label, func(t *testing.T) {
			a, tt, builds := hotCommentStageClock(style.id, true, 0)
			elapsed := time.Duration(0)
			for _, step := range []struct {
				at time.Duration
				id int64
			}{{1250 * time.Millisecond, 1}, {10150 * time.Millisecond, 2}, {19050 * time.Millisecond, 0}} {
				tt.Advance(step.at - elapsed)
				elapsed = step.at
				card, shown := tt.Find("查看评论")
				if step.id == 0 {
					if shown {
						t.Fatal("comment lingered after the quiet ended")
					}
					continue
				}
				if !shown || a.quiet.cm.ID != step.id || math.Abs(float64(card.Y-(800-card.H)/2)) > 0.5 {
					t.Fatalf("comment did not appear immediately in place: id=%d bounds=%v shown=%v", step.id, card, shown)
				}
				before := *builds
				tt.Advance(200 * time.Millisecond)
				elapsed += 200 * time.Millisecond
				if *builds != before {
					t.Fatalf("static hot comment requested %d extra builds", *builds-before)
				}
			}
		})
	}
}
