package app

import (
	"image"
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

// The idle timer must hide the same controls as leaving the window, including the close and stage-style buttons.
func TestNowPlayingIdlePointerFollowsWindowLeave(t *testing.T) {
	for _, tc := range []struct {
		name string
		pane int
	}{
		{"lyrics", paneLyrics},
		{"stage", paneStage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.settings.Fog = false
			a.pl.Cue([]netease.Track{{ID: 1, Name: "Song", Duration: 200000}})
			a.pl.SetLyrics(nil)
			out := audio.NewPlayer(&silence{})
			out.SetVolume(0)
			out.Play()
			defer out.Close()
			a.pl.Attach(out)
			a.nowPlaying, a.npPane = true, tc.pane
			tt := ui.NewTesterWith(a.view, 1120, 720, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			const scale = 0.5
			tt.SetScale(scale)
			tt.Move(680, 260)
			tt.Advance(npSettle)

			labels := []string{"待播清单", "关闭"}
			if tc.pane == paneStage {
				labels = append(labels, "律动效果")
			}
			regions := make([]image.Rectangle, len(labels))
			for i, label := range labels {
				box, ok := tt.Find(label)
				if !ok {
					t.Fatalf("can't locate %q", label)
				}
				regions[i] = image.Rect(int(box.X*scale), int(box.Y*scale), int((box.X+box.W)*scale), int((box.Y+box.H)*scale))
			}
			shown := tt.Image()
			tt.Move(-1, -1)
			tt.Advance(250 * time.Millisecond)
			left := tt.Image()
			check := func(want *image.RGBA) {
				t.Helper()
				frame := tt.Image()
				for i, region := range regions {
					if !sameNowPlayingRegion(frame, want, region) {
						t.Errorf("%q differs from the expected pointer state", labels[i])
					}
				}
			}
			for i, region := range regions {
				if sameNowPlayingRegion(shown, left, region) {
					t.Fatalf("%q stayed visible after the pointer left", labels[i])
				}
			}
			_, queueAccessibleOnLeave := tt.AccessByRole(ui.RoleButton, labels[0])

			tt.Move(680, 260)
			tt.Advance(time.Second)
			check(shown)
			tt.Move(681, 260)
			tt.Advance(1900 * time.Millisecond)
			check(shown)
			tt.Advance(350 * time.Millisecond)
			check(left)
			if _, ok := tt.AccessByRole(ui.RoleButton, labels[0]); ok != queueAccessibleOnLeave {
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
			a.npPane = paneQueue
			tt.Frame()
			queueShown := tt.Image()
			tt.Advance(3 * time.Second)
			if !sameNowPlayingRegion(tt.Image(), queueShown, regions[1]) {
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
