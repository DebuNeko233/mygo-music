package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The page under Now Playing is not built while Now Playing covers it, and
// shows again where it was scrolled once Now Playing closes.
func TestCoveredPageKeepsItsPlace(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.settings = defaultSettings()
	tracks := make([]netease.Track, 200)
	for i := range tracks {
		tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("曲目%d", i+1)}
	}
	a.playlists[42] = &netease.Playlist{ID: 42, Name: "飙升榜", Tracks: tracks}
	for i := range 40 {
		a.charts = append(a.charts, netease.Playlist{ID: int64(100 + i), Name: fmt.Sprintf("榜单%d", i+1)})
	}
	a.pl.Cue([]netease.Track{{ID: 9999, Name: "一张相片"}})
	tt := ui.NewTester(a.view, 1200, 800)

	for _, r := range []route{{page: pagePlaylist, id: 42}, {page: pageCharts}} {
		a.open(r)
		tt.Frame()
		var names []string
		if r.page == pagePlaylist {
			for _, tr := range tracks {
				names = append(names, tr.Name)
			}
		} else {
			for _, chart := range a.charts {
				names = append(names, chart.Name)
			}
		}
		anchor := func() (string, ui.Rect) {
			t.Helper()
			for _, name := range names {
				if item, ok := tt.AccessByRole(ui.RoleButton, name); ok && !item.Offscreen && item.Bounds.Y >= buttonsHeight && item.Bounds.Y+item.Bounds.H <= 800-playerBarHeight {
					return name, item.Bounds
				}
			}
			t.Fatal("no fully visible content item")
			return "", ui.Rect{}
		}
		startName, start := anchor()
		tt.Wheel(700, 400, 0, 2400)
		tt.Advance(time.Second)
		tt.Frame()
		name, before := anchor()

		a.nowPlaying = true
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		if tt.HasText("为你推荐") {
			t.Errorf("%+v: the sidebar is built under Now Playing", r)
		}

		a.nowPlaying = false
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		// Compare the content item itself. A decorative heart can remount
		// with a ~0.00003 DIP change on amd64; integer text snapshots rounded
		// that to a whole pixel and failed despite the list keeping its place.
		after, ok := tt.AccessByRole(ui.RoleButton, name)
		if !ok || after.Offscreen {
			t.Fatalf("%+v: content anchor disappeared", r)
		}
		for _, d := range []float32{after.Bounds.X - before.X, after.Bounds.Y - before.Y, after.Bounds.W - before.W, after.Bounds.H - before.H} {
			if math.Abs(float64(d)) > 0.01 {
				t.Errorf("%+v: content moved after Now Playing: %+v -> %+v", r, before, after.Bounds)
				break
			}
		}
		if name == startName && math.Abs(float64(before.Y-start.Y)) < 1 {
			t.Errorf("%+v: the wheel did not scroll the content", r)
		}
	}
}
