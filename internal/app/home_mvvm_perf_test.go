package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func homePerfApp(count int) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 7, Nickname: "小明"}
	a.nav.Current = route{Page: pageHome}
	for i := range 30 {
		a.home.Featured = append(a.home.Featured, netease.Playlist{ID: int64(i + 1), Name: fmt.Sprintf("推荐歌单 %02d", i+1)})
	}
	for i := range 12 {
		a.home.Charts = append(a.home.Charts, netease.Playlist{ID: int64(i + 100), Name: fmt.Sprintf("榜单 %02d", i+1)})
	}
	for i := range 6 {
		a.home.Recommended = append(a.home.Recommended, netease.Playlist{ID: int64(i + 200), Name: fmt.Sprintf("今日推荐 %02d", i+1)})
	}
	for i := range count {
		a.home.Daily = append(a.home.Daily, netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("每日歌曲 %04d", i+1), Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000})
	}
	return a
}

// Home and daily pages reuse their published lists while building frames.
func BenchmarkHomeDailyViewFrame(b *testing.B) {
	for _, scene := range []string{"home", "daily-30", "daily-1000"} {
		b.Run(scene, func(b *testing.B) {
			count := 30
			if scene == "daily-1000" {
				count = 1000
			}
			a := homePerfApp(count)
			if scene != "home" {
				a.nav.Current = route{Page: pageDaily}
			}
			tt := ui.NewTester(func(c *ui.Context) {
				a.pal = paletteFor(c.Theme())
				if scene == "home" {
					a.homePage(c)
				} else {
					a.views.daily.Build(c)
				}
			}, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Completed, paused home/daily pages stay asleep when input and content do not change.
func TestHomeDailyIdleFrameBudget(t *testing.T) {
	for _, daily := range []bool{false, true} {
		t.Run(fmt.Sprint(daily), func(t *testing.T) {
			a := homePerfApp(30)
			if daily {
				a.nav.Current = route{Page: pageDaily}
			}
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) {
				builds++
				a.pal = paletteFor(c.Theme())
				if daily {
					a.views.daily.Build(c)
				} else {
					a.homePage(c)
				}
			}, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			before := builds
			tt.Advance(2 * time.Second)
			if builds != before {
				t.Fatalf("idle page requested %d extra builds", builds-before)
			}
		})
	}
}

// Loaded, cached, empty, error and skeleton surfaces can be inspected in both themes across the refactor.
func TestHomeDailyMVVMFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save home/daily surfaces")
	}
	for _, scene := range []string{"home", "daily", "charts", "signed-out", "cached-refresh", "empty-daily", "error", "loading"} {
		for _, dark := range []bool{false, true} {
			a := homePerfApp(30)
			switch scene {
			case "daily", "empty-daily":
				a.nav.Current = route{Page: pageDaily}
				if scene == "empty-daily" {
					a.home.Daily = nil
				}
			case "charts":
				a.nav.Current = route{Page: pageCharts}
			case "signed-out":
				a.account.Profile = nil
				a.home.Daily = nil
				a.home.Recommended = nil
			case "cached-refresh":
				a.home.Loading = true
				a.network.offline = true
			case "error", "loading":
				a.home.Featured = nil
				a.home.Charts = nil
				a.home.Daily = nil
				a.home.Recommended = nil
				if scene == "error" {
					a.home.Error = "内容加载失败"
				} else {
					a.home.Loading = true
				}
			}
			tt := ui.NewTesterWith(func(c *ui.Context) {
				a.pal = paletteFor(c.Theme())
				switch a.nav.Current.Page {
				case pageDaily:
					a.views.daily.Build(c)
				case pageCharts:
					a.chartsPage(c)
				default:
					a.homePage(c)
				}
			}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			tt.SetDark(dark)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("home-daily-%s-dark=%v.png", scene, dark)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Loading measures request orchestration and main-thread publication with the same four existing providers, excluding disk/network.
func BenchmarkHomeLoad(b *testing.B) {
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 7}
	posts := testPosts(a)
	oldFeatured, oldCharts, oldDaily, oldRecommended := featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists
	b.Cleanup(func() {
		featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists = oldFeatured, oldCharts, oldDaily, oldRecommended
	})
	lists := []netease.Playlist{{ID: 1}}
	tracks := []netease.Track{{ID: 1}}
	featuredPlaylists = func() ([]netease.Playlist, error) { return lists, nil }
	homeCharts = featuredPlaylists
	recommendedPlaylists = featuredPlaylists
	dailySongs = func() ([]netease.Track, error) { return tracks, nil }
	b.ReportAllocs()
	for b.Loop() {
		a.loadHome()
		for a.home.Loading {
			(<-posts)()
		}
	}
}
