package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// Enabling Reduce Motion during a hot comment's normal entrance must snap
// the existing card into place and stop its animation without leaving Stage.
func TestStageHotCommentPreferenceChange(t *testing.T) {
	_, tt, builds := hotCommentStageClock("tilt", false, 5*time.Second)
	first, ok := tt.Find("查看评论")
	if !ok {
		t.Fatal("no entering comment")
	}
	tt.Advance(80 * time.Millisecond)
	moving, _ := tt.Find("查看评论")
	if first.Y == moving.Y {
		t.Fatal("normal comment entrance stopped animating")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	still, ok := tt.Find("查看评论")
	if !ok || math.Abs(float64(still.Y-(800-still.H)/2)) > 0.5 {
		t.Fatal("preference change did not snap the card into place")
	}
	before := *builds
	tt.Advance(200 * time.Millisecond)
	if *builds != before {
		t.Fatalf("reduced-motion comment kept rebuilding: %d", *builds-before)
	}
}

// stageClock advances playback with the Tester's clock, counting actual
// view builds, including those due to After rather than FramePending.
func stageClock(style string, reduced, playing bool, pos time.Duration) (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.HotComments = false
	a.settings.StageStyle, a.settings.StageSparks = style, true
	a.pl.Cue([]netease.Track{{ID: 7, Duration: 30000}})
	a.pl.SetLyrics([]lyrics.Line{
		{At: 2 * time.Second, End: 7 * time.Second, Text: "一 二", Words: []lyrics.Word{
			{Text: "一 ", Start: 2 * time.Second, End: 4 * time.Second},
			{Text: "二", Start: 6 * time.Second, End: 7 * time.Second},
		}},
		{At: 20 * time.Second, End: 21 * time.Second, Text: "三", Words: []lyrics.Word{
			{Text: "三", Start: 20 * time.Second, End: 21 * time.Second},
		}},
	})
	builds := 0
	var start time.Time
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		if playing {
			a.pl.Hold(pos + c.Now().Sub(start))
		} else {
			a.pl.Seek(pos)
		}
		ui.Column(c).Fill().Children(func() { a.stage(c, a.pl.Current(), ui.RGB(255, 100, 100)) })
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return a, tt, &builds
}

// Reduce Motion used to keep rebuilding at display rate while hand-written
// word animations continued. All styles now wake only where text changes.
func TestStageReducedMotionBoundaries(t *testing.T) {
	for _, style := range stageStyles {
		t.Run(style.label, func(t *testing.T) {
			a, tt, builds := stageClock(style.id, true, true, 0)
			for _, step := range []struct {
				advance time.Duration
				change  bool
			}{
				{time.Second, false}, // intro
				{time.Second, true},  // first line and word
				{time.Second, false}, // held word
				{time.Second, true},  // word ends
				{time.Second, false}, // between words
				{time.Second, true},  // second word
				{time.Second, true},  // second word ends
				{time.Second, true},  // interlude
				{11 * time.Second, false},
				{time.Second, true}, // next line
				{time.Second, true}, // last word ends
				{5 * time.Second, false},
			} {
				before := *builds
				tt.Advance(step.advance)
				n := *builds - before
				if step.change && (n == 0 || n > 2) || !step.change && n != 0 {
					t.Fatalf("after %v: %d builds, change=%v", a.pl.Position(), n, step.change)
				}
			}
		})
	}
}

// The normal stage still moves, a reduced-motion held word or interlude
// sleeps, and a paused stage asks for no rebuilds under either preference.
func TestStageMotionFrameBudget(t *testing.T) {
	for _, style := range stageStyles {
		for _, reduced := range []bool{false, true} {
			for _, playing := range []bool{false, true} {
				for _, pos := range []time.Duration{2500 * time.Millisecond, 12 * time.Second} {
					_, tt, builds := stageClock(style.id, reduced, playing, pos)
					before := *builds
					tt.Advance(time.Second)
					n := *builds - before
					t.Logf("%s reduced=%v playing=%v pos=%v: %d builds/s", style.label, reduced, playing, pos, n)
					if reduced || !playing {
						if n != 0 {
							t.Errorf("stationary stage rebuilt %d times", n)
						}
					} else if n == 0 {
						t.Error("normal stage stopped updating")
					}
				}
			}
		}
	}
}

// Changing the system preference while Stage is open starts and stops its
// motion without needing to leave the pane or seek.
func TestStageMotionPreferenceChanges(t *testing.T) {
	_, tt, builds := stageClock("tilt", true, true, 2200*time.Millisecond)
	tt.SetPreferences(ui.Preferences{})
	before := *builds
	tt.Advance(200 * time.Millisecond)
	if *builds == before {
		t.Fatal("motion did not resume")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(100 * time.Millisecond) // let an already queued frame finish
	before = *builds
	tt.Advance(200 * time.Millisecond)
	if n := *builds - before; n != 0 {
		t.Fatalf("reduced motion kept rebuilding: %d", n)
	}
}
