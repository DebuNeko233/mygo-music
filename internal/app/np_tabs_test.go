package app

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Empty count space must not push visible tabs off their axis near a window edge, as Linux fonts did in narrow windows.
func TestNowPlayingTabsClampVisibleButtons(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.comments.Loaded = true
	var left float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		anchor := ui.Box(c).Absolute().Left(left).Top(20).Size(280, npTabsHeight)
		a.npTabsRow(c, anchor, true).Children(func() { a.paneTabs(c) })
	}, 640, 120, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	for _, left = range []float32{0, 50, 360} {
		for _, count := range []int{0, 32000, 12345678} {
			a.comments.Total = count
			a.playbackState.Pane = paneLyrics
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
			if a.playbackState.Pane != paneComments {
				t.Fatal("the comment button stopped taking clicks near the edge")
			}
		}
	}
}

// The idle timer must hide the same controls as leaving the window, including the close and stage-style buttons.
func TestNowPlayingIdlePointerFollowsWindowLeave(t *testing.T) {
	for _, tc := range []struct {
		name, activeTab string
		pane            int
	}{
		{"lyrics", "歌词", paneLyrics},
		{"stage", "律动", paneStage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.settings.Fog = false
			a.pl.Cue([]netease.Track{{ID: 1, Name: "Song", Duration: 200000}})
			a.pl.SetLyrics(nil)
			out := newSilentAudio()
			out.Play()
			defer out.Close()
			a.pl.Attach(out)
			a.playbackState.Open, a.playbackState.Pane = true, tc.pane
			tt := ui.NewTesterWith(a.view, 1120, 720, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			const scale = 0.5
			tt.SetScale(scale)
			tt.Move(680, 260)
			tt.Advance(npSettle)

			// The selected tab has a painted background even without CJK glyphs.
			const closeLabel = "关闭"
			labels := []string{tc.activeTab, closeLabel}
			if tc.pane == paneStage {
				labels = append(labels, "律动效果")
			}
			regions := make(map[string]image.Rectangle, len(labels))
			for _, label := range labels {
				box, ok := tt.Find(label)
				if !ok {
					t.Fatalf("can't locate %q", label)
				}
				regions[label] = image.Rect(int(box.X*scale), int(box.Y*scale), int((box.X+box.W)*scale), int((box.Y+box.H)*scale))
			}
			shown := tt.Image()
			tt.Move(-1, -1)
			tt.Advance(250 * time.Millisecond)
			left := tt.Image()
			check := func(want *image.RGBA) {
				t.Helper()
				frame := tt.Image()
				for _, label := range labels {
					if !sameNowPlayingRegion(frame, want, regions[label]) {
						t.Errorf("%q differs from the expected pointer state", label)
					}
				}
			}
			for _, label := range labels {
				if sameNowPlayingRegion(shown, left, regions[label]) {
					t.Errorf("%q did not change after the pointer left", label)
				}
			}
			const queueLabel = "待播清单"
			_, queueAccessibleOnLeave := tt.AccessByRole(ui.RoleButton, queueLabel)

			tt.Move(680, 260)
			tt.Advance(time.Second)
			check(shown)
			tt.Move(681, 260)
			tt.Advance(1900 * time.Millisecond)
			check(shown)
			tt.Advance(350 * time.Millisecond)
			check(left)
			if _, ok := tt.AccessByRole(ui.RoleButton, queueLabel); ok != queueAccessibleOnLeave {
				t.Error("the queue tab is accessible in only one of the two hidden states")
			}
			tt.Move(682, 260)
			tt.Advance(250 * time.Millisecond)
			check(shown)
			a.pl.TogglePlay()
			tt.Frame()
			tt.Advance(3 * time.Second)
			check(shown)

			a.pl.TogglePlay()
			a.playbackState.Pane = paneQueue
			tt.Frame()
			queueShown := tt.Image()
			tt.Advance(3 * time.Second)
			if !sameNowPlayingRegion(tt.Image(), queueShown, regions[closeLabel]) {
				t.Error("the close button faded while idling on the queue")
			}
		})
	}
}

func sameNowPlayingRegion(got, want *image.RGBA, region image.Rectangle) bool {
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}
