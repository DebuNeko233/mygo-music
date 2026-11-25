/*
验证歌词视图共享来源，并通过菜单更新舞台样式、粒子和热评偏好。
*/

package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

// All three lyric views choose YRC for the whole song and fall back to LRC together.
func TestLyricViewsShareSelectedSource(t *testing.T) {
	const lrc = "[00:24.406]Fallback\n[00:24.980]Chorus"
	const translation = "[00:24.406]Opening translation\n[00:24.980]Shared translation"
	for _, tc := range []struct{ name, yrc, text, translated, omitted string }{
		{"YRC", "[25250,3930](25250,3930,0)Chorus", "Chorus", "Shared translation", "Fallback"},
		{"LRC", "[999999999,500](999999999,500,0)Invalid YRC", "Chorus", "Shared translation", "Invalid YRC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, pane := shortLyricsApp(nil, 360, 300)
			a.pl.RefreshLyrics(1, lrc, translation, tc.yrc)
			a.pl.Hold(26 * time.Second)
			pane.Frame()
			stage := ui.NewTesterWith(func(c *ui.Context) { a.views.playback.Stage(c, a.pl.Current(), playbackview.White) }, 360, 300,
				ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			desktop := ui.NewTester((&playbackview.DeskLyrics{A: &a.views.playback}).View, 480, 200)
			for _, view := range []*ui.Tester{pane, stage, desktop} {
				if !view.HasText(tc.text) || !view.HasText(tc.translated) || view.HasText(tc.omitted) {
					t.Fatalf("view used the wrong source or lost its translation: %q", view.Texts())
				}
			}
			if tc.name == "LRC" && !pane.HasText("Fallback") {
				t.Fatal("whole-song fallback lost the LRC-only row")
			}
		})
	}
}

// Choosing a style from the stage's menu sets its lyrics in it, and
// choosing 流光 goes back to the classic.
func TestStageStyleMenu(t *testing.T) {
	a, tt, _ := playingApp()
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneStage
	a.pl.Seek(2 * time.Second)
	tt.Frame()
	for _, c := range []struct{ item, want string }{{"云阶", "partita"}, {"倾诉", "tilt"}, {"流光", ""}} {
		if err := tt.Click("律动效果"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(c.item); err != nil {
			t.Fatalf("%v; the menu shows %q", err, tt.Menu())
		}
		tt.Frame()
		if a.settings.StageStyle != c.want {
			t.Errorf("chose %s: style %q, want %q", c.item, a.settings.StageStyle, c.want)
		}
	}
}

// 火花 in the stage's menu turns the sparks on, and again off, whatever
// the style.
func TestStageSparksMenu(t *testing.T) {
	a, tt, _ := playingApp()
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneStage
	a.settings.StageStyle = "tilt"
	tt.Frame()
	for _, want := range []bool{true, false} {
		if err := tt.Click("律动效果"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("火花"); err != nil {
			t.Fatalf("%v; the menu shows %q", err, tt.Menu())
		}
		tt.Frame()
		if a.settings.StageSparks != want || a.settings.StageStyle != "tilt" {
			t.Errorf("sparks %v in %q, want %v in tilt", a.settings.StageSparks, a.settings.StageStyle, want)
		}
	}
}

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
