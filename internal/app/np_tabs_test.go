package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Empty count space must not push visible tabs off their axis near a window edge, as Linux fonts did in narrow windows.
func TestNowPlayingTabsClampVisibleButtons(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.comments.loaded = true
	var left float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		anchor := ui.Box(c).Absolute().Left(left).Top(20).Size(280, npTabsHeight)
		a.npTabsRow(c, anchor, true).Children(func() { a.paneTabs(c) })
	}, 640, 120, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	for _, left = range []float32{0, 50, 360} {
		for _, count := range []int{0, 32000, 12345678} {
			a.comments.total = count
			a.npPane = paneLyrics
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			first, ok := tt.Find("歌词")
			if !ok {
				t.Fatal("no first tab")
			}
			last, ok := tt.Find(a.commentsTab())
			if !ok {
				t.Fatal("no comment tab")
			}
			width := last.X + last.W - first.X
			want := max(12+width/2, min(left+140, 640-12-width/2)) + 6
			if center := (first.X + last.X + last.W) / 2; math.Abs(float64(center-want)) > 1 {
				t.Fatalf("left %v, count %d: visible tabs centered at %v, want %v", left, count, center, want)
			}
			if err := tt.Click(a.commentsTab()); err != nil {
				t.Fatal(err)
			}
			if a.npPane != paneComments {
				t.Fatal("the comment button stopped taking clicks near the edge")
			}
		}
	}
}

// Navigation hides only during playback. Pausing reveals it and resuming starts a new idle period.
func TestNowPlayingTabsHideWhenPlayingAndPointerIsIdle(t *testing.T) {
	for _, tc := range []struct {
		name string
		pane int
	}{
		{"lyrics", paneLyrics},
		{"stage", paneStage},
		{"wall", paneWall},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.pl.Cue([]netease.Track{{ID: 1, Name: "Song", Duration: 200000}})
			a.pl.SetLyrics(nil)
			out := audio.NewPlayer(&silence{})
			out.SetVolume(0)
			out.Play()
			defer out.Close()
			a.pl.Attach(out)
			a.nowPlaying, a.npPane = true, tc.pane
			tt := ui.NewTesterWith(a.view, 1120, 720, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			tt.SetScale(0.25)
			tt.Move(680, 260)
			tt.Advance(npSettle)
			const label = "待播清单"
			button := func(wantVisible bool) ui.Rect {
				t.Helper()
				item, ok := tt.AccessByRole(ui.RoleButton, label)
				if (ok && !item.Offscreen) != wantVisible {
					t.Fatalf("queue button visibility = %v, want %v", ok && !item.Offscreen, wantVisible)
				}
				return item.Bounds
			}
			box := button(true)
			x, y := box.X+box.W/2, box.Y+box.H/2
			tt.Move(x, y)
			tt.Advance(2 * time.Second)
			button(true)
			x++
			tt.Move(x, y)
			tt.Advance(2900 * time.Millisecond)
			button(true)
			tt.Advance(100 * time.Millisecond)
			button(false)
			tt.ClickAt(x, y)
			if a.npPane != tc.pane {
				t.Fatal("clicked a hidden tab")
			}
			a.pl.TogglePlay()
			tt.Frame()
			button(true)
			tt.Advance(4 * time.Second)
			button(true)
			a.pl.TogglePlay()
			tt.Frame()
			tt.Advance(2900 * time.Millisecond)
			button(true)
			tt.Advance(100 * time.Millisecond)
			button(false)
			tt.Move(x+1, y)
			if got := button(true); got != box {
				t.Fatalf("the tabs moved from %v to %v", box, got)
			}
			tt.ClickAt(x+1, y)
			if a.npPane != paneQueue {
				t.Fatal("the revealed tab did not open the queue")
			}
		})
	}
}
