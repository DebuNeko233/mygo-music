package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Empty count space must not push visible tabs off their axis near a window edge, as Linux fonts did in narrow windows.
func TestNowPlayingTabsClampVisibleButtons(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.comments.loaded = true
	var left float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		anchor := ui.Box(c).Absolute().Left(left).Top(20).Size(280, npTabsHeight)
		a.npTabsRow(c, anchor).Children(func() { a.paneTabs(c) })
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
