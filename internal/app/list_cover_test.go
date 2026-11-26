/*
验证歌曲列表小封面在多页浏览、垃圾回收及异步恢复后无需等待淡入。
*/

package app

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/netease"
)

func listCoverBitmap() *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	return ui.NewBitmap(img)
}

func assertListCoverRed(t *testing.T, view *ui.Tester) {
	t.Helper()
	r, ok := view.Find("列表封面")
	if !ok {
		t.Fatal("list cover was not built")
	}
	pixel := view.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
	if pixel.R < 240 || pixel.G > 20 || pixel.B > 20 {
		t.Fatalf("first available frame showed a placeholder/fade: %v", pixel)
	}
}

// A third recent list must keep its first frame through unrelated large images and GC.
func TestListCoverReturnFirstFrame(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	now := time.Unix(1e9, 0)
	a.art = artcache.New(artcache.Options{Post: func(func()) {}, Now: func() time.Time { return now }, Limit: 64 << 10,
		Load: func(string) (*ui.Bitmap, image.Image, int, error) {
			return nil, nil, 0, fmt.Errorf("unexpected reload")
		}})
	t.Cleanup(a.art.Close)
	tracks := make([]netease.Track, 3)
	for i := range tracks {
		tracks[i] = netease.Track{ID: int64(i + 1), Album: netease.Album{PicURL: fmt.Sprintf("https://p1.music.126.net/list-%d.jpg", i)}}
		a.art.Put(netease.Sized(tracks[i].Album.PicURL, 96), &artcache.Image{Bitmap: listCoverBitmap()})
	}
	a.nav.Current = route{Page: pagePlaylist, ID: 1}
	view := ui.NewTester(func(c *ui.Context) {
		a.art.PageFrame(a.nav.Current, true)
		if id := a.nav.Current.ID; a.nav.Current.Page == pagePlaylist {
			a.trackCover(c, &tracks[id-1], 96, 38, 6).Label("列表封面")
		}
	}, 200, 100)
	for _, id := range []int64{1, 2, 3} {
		a.nav.Current = route{Page: pagePlaylist, ID: id}
		view.Frame()
		view.Frame() // collect the committed visible viewport
		assertListCoverRed(t, view)
	}
	a.nav.Current = route{Page: pageAlbum, ID: 4}
	view.Frame()
	now = now.Add(time.Minute)
	a.art.Put("unrelated-thumbnail", &artcache.Image{Bitmap: listCoverBitmap()})
	a.art.Put("large-offscreen", &artcache.Image{Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 96, 96)))})
	runtime.GC()
	t.Logf("after pressure: %+v", a.art.Stats())
	a.nav.Current = route{Page: pagePlaylist, ID: 1}
	view.Frame()
	assertListCoverRed(t, view)
	if s := a.art.Stats(); s.Loads != 0 || s.ListPlaceholders != 0 || s.RetainedListEntries != 3 || s.Bytes > 64<<10 {
		t.Fatalf("return decoded again, showed a placeholder or lost bounded retention: %+v", s)
	}
}

// A list bitmap arriving after a placeholder is fully visible in its first frame.
func TestListCoverRecoveryDoesNotFade(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%v", local), func(t *testing.T) {
			isolateUserData(t)
			a := newTestApp()
			tr := netease.Track{ID: 1, Album: netease.Album{PicURL: "https://p1.music.126.net/restored.jpg"}}
			key := netease.Sized(tr.Album.PicURL, 96)
			if local {
				tr = netease.Track{LocalID: 1}
				song := localcatalog.Song{Track: tr, Cover: true, Path: filepath.Join(t.TempDir(), "fixture.flac"), Stamp: 1, Size: 4}
				a.local.Replace(localcatalog.Catalog{Songs: []localcatalog.Song{song}})
				key = artcache.LocalEmbeddedKey(song).ArtworkKey(96)
			}
			a.art = artcache.New(artcache.Options{Post: func(func()) {}, Load: func(string) (*ui.Bitmap, image.Image, int, error) {
				return nil, nil, 0, fmt.Errorf("held cache miss")
			}})
			t.Cleanup(a.art.Close)
			view := ui.NewTester(func(c *ui.Context) { a.trackCover(c, &tr, 96, 38, 6).Label("列表封面") }, 200, 100)
			view.Frame()
			a.art.Put(key, &artcache.Image{Bitmap: listCoverBitmap()})
			view.Frame()
			assertListCoverRed(t, view)
		})
	}
}

// Full virtual lists return with ready row pixels in both themes and at minimum/normal window sizes.
func TestListCoverNavigationFrames(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 440}, {1180, 780}} {
			t.Run(fmt.Sprintf("dark=%v/%dx%d", dark, size[0], size[1]), func(t *testing.T) {
				isolateUserData(t)
				a := newTestApp()
				a.settings = defaultSettings()
				now := time.Unix(1e9, 0)
				a.art = artcache.New(artcache.Options{Post: func(func()) {}, Now: func() time.Time { return now }, Limit: 256 << 10,
					Load: func(string) (*ui.Bitmap, image.Image, int, error) {
						return nil, nil, 0, fmt.Errorf("fixture cache miss")
					}})
				t.Cleanup(a.art.Close)
				for page := int64(1); page <= 3; page++ {
					pl := &netease.Playlist{ID: page, Name: fmt.Sprintf("歌单 %d", page)}
					for i := range 3 {
						tr := netease.Track{ID: page*10 + int64(i), Name: fmt.Sprintf("歌曲 %d-%d", page, i),
							Album: netease.Album{PicURL: fmt.Sprintf("https://p1.music.126.net/%d-%d.jpg", page, i)}}
						pl.Tracks = append(pl.Tracks, tr)
						a.art.Put(netease.Sized(tr.Album.PicURL, 96), &artcache.Image{Bitmap: listCoverBitmap()})
					}
					a.library.Playlists[page] = pl
				}
				a.nav.Current = route{Page: pagePlaylist, ID: 1}
				view := ui.NewTesterWith(a.view, size[0], size[1], ui.TesterOptions{Dark: dark})
				for page := int64(1); page <= 3; page++ {
					a.nav.Current.ID = page
					view.Frame()
					view.Frame()
				}
				a.nav.Current = route{Page: pageCharts}
				view.Frame()
				now = now.Add(time.Minute)
				for i := range 4 {
					a.art.Put(fmt.Sprintf("unrelated-%d", i), &artcache.Image{Bitmap: listCoverBitmap()})
				}
				a.art.Put("large", &artcache.Image{Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 192, 192)))})
				runtime.GC()
				a.nav.Current = route{Page: pagePlaylist, ID: 1}
				view.Frame()
				row, ok := view.AccessByRole(ui.RoleButton, "歌曲 1-0")
				if !ok {
					t.Fatal("returned list did not build its first song")
				}
				bounds := row.Bounds
				pixel := view.Image().RGBAAt(int(bounds.X)+12+26+14+19, int(bounds.Y+bounds.H/2))
				if pixel.R < 240 || pixel.G > 20 || pixel.B > 20 {
					t.Fatalf("returned row flashed its placeholder: %v, stats: %+v", pixel, a.art.Stats())
				}
				if dir := os.Getenv("NM_FRAMES"); dir != "" {
					file, err := os.Create(filepath.Join(dir, fmt.Sprintf("list-return-dark-%v-%dx%d.png", dark, size[0], size[1])))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, view.Image())
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("write frame: %v / %v", err, closeErr)
					}
				}
			})
		}
	}
}

// Refreshing a displayed playlist with another CDN host must never clear a ready row cover.
func TestListCoverPlaylistRefreshCDNHost(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 440}, {1180, 780}} {
			t.Run(fmt.Sprintf("dark=%v/%dx%d", dark, size[0], size[1]), func(t *testing.T) {
				isolateUserData(t)
				a := newTestApp()
				a.settings = defaultSettings()
				posts := make(chan func(), 16)
				requests := make(chan string, 16)
				a.art = artcache.New(artcache.Options{Post: func(fn func()) { posts <- fn }, Load: func(url string) (*ui.Bitmap, image.Image, int, error) {
					requests <- url
					img := image.NewRGBA(image.Rect(0, 0, 32, 32))
					draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
					return ui.NewBitmap(img), img, 0, nil
				}})
				t.Cleanup(a.art.Close)
				old := netease.Track{ID: 1, Name: "刷新的歌曲", Album: netease.Album{PicURL: "https://p2.music.126.net/fixture/cover.jpg"}}
				fresh := old
				fresh.Album.PicURL = "https://p1.music.126.net/fixture/cover.jpg"
				a.art.Put(netease.Sized(old.Album.PicURL, 96), &artcache.Image{Bitmap: listCoverBitmap()})
				a.nav.Current = route{Page: pagePlaylist, ID: 1}
				a.library.Playlists[1] = &netease.Playlist{ID: 1, Name: "缓存的歌单", Tracks: []netease.Track{old}}
				// Check every build, including intermediate visibility/layout builds
				// which Tester.Frame settles before returning its final pixels.
				load := a.views.artwork.LoadList
				builds, missing := 0, 0
				a.views.artwork.LoadList = func(url string, box ui.Element) *ui.Bitmap {
					bmp := load(url, box)
					builds++
					if bmp == nil {
						missing++
					}
					return bmp
				}
				view := ui.NewTesterWith(a.view, size[0], size[1], ui.TesterOptions{Dark: dark})
				assertRow := func(want color.RGBA) {
					t.Helper()
					row, ok := view.AccessByRole(ui.RoleButton, fresh.Name)
					if !ok {
						t.Fatal("refreshed row missing")
					}
					r := row.Bounds
					pixel := view.Image().RGBAAt(int(r.X)+12+26+14+19, int(r.Y+r.H/2))
					if pixel != want {
						t.Fatalf("refresh flashed placeholder: %v", pixel)
					}
				}
				assertRow(color.RGBA{R: 255, A: 255})
				a.library.Playlists[1] = &netease.Playlist{ID: 1, Name: "刷新的歌单", Tracks: []netease.Track{fresh}}
				for range 5 {
					view.Frame()
					assertRow(color.RGBA{R: 255, A: 255})
				}
				if got := wait(t, requests, "fresh CDN request"); got != netease.Sized(fresh.Album.PicURL, 96) {
					t.Fatalf("request identity changed: %s", got)
				}
				wait(t, posts, "fresh CDN decode")()
				for range 5 {
					view.Frame()
					assertRow(color.RGBA{B: 255, A: 255})
				}
				if builds == 0 || missing != 0 {
					t.Fatalf("cover disappeared during %d of %d builds", missing, builds)
				}
			})
		}
	}
}
