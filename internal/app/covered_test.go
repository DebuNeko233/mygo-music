package app

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A playlist keeps its on-screen covers ready when returning from either Now Playing or another page.
func TestPlaylistCoversSurviveReturningToPage(t *testing.T) {
	for _, mode := range []string{"now playing", "another playlist"} {
		t.Run(mode, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			page := route{page: pagePlaylist, id: 42}
			a.route = page
			tr := netease.Track{ID: 1, Name: "测试封面", Album: netease.Album{PicURL: "https://p1.music.126.net/test/row.jpg"}}
			a.library.Playlists[42] = &netease.Playlist{ID: 42, Name: "原歌单", Tracks: []netease.Track{tr}}
			a.library.Playlists[43] = &netease.Playlist{ID: 43, Name: "其他歌单"}
			a.pl.Cue([]netease.Track{{ID: 2, Name: "正在播放的歌曲"}})
			a.comments.track = 2
			a.pl.SetLyrics(nil)
			now := time.Unix(1e9, 0)
			a.art.now, a.art.limit = func() time.Time { return now }, 300
			img := image.NewRGBA(image.Rect(0, 0, 4, 4))
			draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
			key := netease.Sized(tr.Album.PicURL, 96)
			a.art.add(key, &artwork{bmp: ui.NewBitmap(img), size: 100})
			view := ui.NewTester(a.view, 1180, 780)
			coverVisible := func() bool {
				t.Helper()
				row, ok := view.AccessByRole(ui.RoleButton, tr.Name)
				if !ok {
					return false
				}
				b := row.Bounds
				r, g, blue, _ := view.Image().At(int(b.X)+12+26+14+19, int(b.Y+b.H/2)).RGBA()
				return r > 0xe000 && g < 0x4000 && blue < 0x4000
			}
			if !coverVisible() {
				t.Fatal("the fixture cover did not initially render")
			}
			if mode == "now playing" {
				a.nowPlaying = true
			} else {
				a.route = route{page: pagePlaylist, id: 43}
			}
			view.Frame()
			view.Advance(time.Second)
			view.Frame()
			now = now.Add(2 * time.Second)
			a.art.add("offscreen", &artwork{size: 110, used: now})
			a.art.add("latest", &artwork{size: 190, used: now})
			if a.art.art[key] == nil || a.art.art["offscreen"] != nil || a.art.size > a.art.limit {
				t.Fatal("the return page's cover was evicted despite room for it")
			}
			a.nowPlaying, a.route = false, page
			view.Frame()
			view.Advance(time.Second)
			view.Frame()
			if !coverVisible() {
				t.Fatal("returning to the playlist showed a placeholder")
			}
		})
	}
}

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
	a.library.Playlists[42] = &netease.Playlist{ID: 42, Name: "飙升榜", Tracks: tracks}
	for i := range 40 {
		a.home.Charts = append(a.home.Charts, netease.Playlist{ID: int64(100 + i), Name: fmt.Sprintf("榜单%d", i+1)})
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
			for _, chart := range a.home.Charts {
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
