/*
验证首页重试仅由用户操作触发，且卡片命令使用最新发布的数据。
*/

package view

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
	homevm "netease-music-mygo/internal/ui/viewmodel/home"
)

// Retrying either missing surface delegates once to the existing command and never retries on rebuild alone.
func TestRetryMissingSurface(t *testing.T) {
	for _, charts := range []bool{false, true} {
		t.Run(map[bool]string{false: "home", true: "charts"}[charts], func(t *testing.T) {
			m := homevm.Home{Error: "unavailable"}
			var state shell.State
			retries := 0
			tt := ui.NewTester(func(c *ui.Context) {
				v := Home{Model: &m, Palette: components.PaletteFor(c.Theme(), ui.Color{}), Retry: func() { retries++; m.Loading = true; m.Error = "" },
					HomeSkeleton: func(*ui.Context) {}, ChartsSkeleton: func(*ui.Context) {}, PlaylistsSkeleton: func(*ui.Context) {}}
				if charts {
					v.Charts(c, &state)
				} else {
					v.Home(c, &state)
				}
			}, 1000, 700)
			tt.Frame()
			tt.Frame()
			if retries != 0 {
				t.Fatal("building failed surface retried automatically")
			}
			if err := tt.Click("重新加载内容"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			tt.Frame()
			if retries != 1 || !m.Loading {
				t.Fatalf("retry lost its command or repeated: %d %+v", retries, m)
			}
		})
	}
}

// Replacing published home data changes the cards' commands without leaving a stale selected item in the view.
func TestCardsUsePublishedData(t *testing.T) {
	for _, scene := range []struct {
		name   string
		charts bool
		set    func(*homevm.Home, netease.Playlist)
	}{
		{name: "featured", set: func(m *homevm.Home, p netease.Playlist) { m.Featured = []netease.Playlist{p} }},
		{name: "recommended", set: func(m *homevm.Home, p netease.Playlist) {
			m.Featured = []netease.Playlist{{ID: 100, Name: "featured list"}}
			m.Recommended = []netease.Playlist{p}
		}},
		{name: "official chart", charts: true, set: func(m *homevm.Home, p netease.Playlist) { m.Charts = []netease.Playlist{p} }},
		{name: "other chart", charts: true, set: func(m *homevm.Home, p netease.Playlist) {
			m.Charts = []netease.Playlist{{ID: 101, Name: "chart one"}, {ID: 102, Name: "chart two"}, {ID: 103, Name: "chart three"}, {ID: 104, Name: "chart four"}, p}
		}},
	} {
		t.Run(scene.name, func(t *testing.T) {
			m := homevm.Home{}
			var state shell.State
			selected := int64(0)
			scene.set(&m, netease.Playlist{ID: 1, Name: "first"})
			tt := ui.NewTester(func(c *ui.Context) {
				card := func(c *ui.Context, p *netease.Playlist) { ui.Button(c, p.Name).OnClick(func() { selected = p.ID }) }
				v := Home{Model: &m, Palette: components.PaletteFor(c.Theme(), ui.Color{}), PlaylistCard: card, ChartCard: card, DailyCard: func(*ui.Context, time.Time) {}}
				if scene.charts {
					v.Charts(c, &state)
				} else {
					v.Home(c, &state)
				}
			}, 1000, 700)
			if err := tt.Click("first"); err != nil {
				t.Fatal(err)
			}
			if selected != 1 {
				t.Fatalf("card opened %d", selected)
			}
			scene.set(&m, netease.Playlist{ID: 2, Name: "replacement"})
			tt.Frame()
			if err := tt.Click("replacement"); err != nil {
				t.Fatal(err)
			}
			if selected != 2 {
				t.Fatalf("replaced card kept stale command: %d", selected)
			}
		})
	}
}
