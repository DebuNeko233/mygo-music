package shell

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/theme"
	"netease-music-mygo/internal/navigation"
)

func nothing(*ui.Context)                {}
func noBindings(*ui.Context, ui.Element) {}

// Sidebar choices and history buttons invoke the supplied navigator without owning a second history.
func TestNavigationCommands(t *testing.T) {
	var s State
	nav := navigation.Navigator{}
	last := navigation.Route{Page: navigation.Search, Query: "MyGO", ID: 9}
	opened := 0
	tt := ui.NewTester(func(c *ui.Context) {
		p := theme.For(c.Theme(), ui.Color{})
		n := Navigation{Current: nav.Current, LastSearch: &last, CanBack: len(nav.History) > 0, CanForward: len(nav.Forward) > 0,
			Open: func(r navigation.Route) { opened++; nav.Open(r) },
			Back: func() {
				if r, ok := nav.Previous(); ok {
					nav.Current = r
				}
			},
			Forward: func() {
				if r, ok := nav.Next(); ok {
					nav.Current = r
				}
			}}
		s.Build(c, Layout{Palette: p, Navigation: n, BindRoot: noBindings, Page: nothing, Player: nothing, NowPlaying: nothing,
			Sidebar: func(c *ui.Context) { s.Sidebar(c, p, n, Sidebar{Search: nothing, Library: nothing, Account: nothing}) }})
	}, 1000, 700)
	if err := tt.Click("排行榜"); err != nil {
		t.Fatal(err)
	}
	if nav.Current.Page != navigation.Charts || opened != 1 {
		t.Fatalf("choice bypassed navigator: %+v (%d)", nav, opened)
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if nav.Current.Page != navigation.Home || len(nav.Forward) != 1 {
		t.Fatalf("back lost history: %+v", nav)
	}
	if err := tt.Click("前进"); err != nil {
		t.Fatal(err)
	}
	if nav.Current.Page != navigation.Charts {
		t.Fatalf("forward opened %+v", nav.Current)
	}
	if err := tt.Click("搜索结果"); err != nil {
		t.Fatal(err)
	}
	if nav.Current != last || opened != 2 {
		t.Fatalf("search lost its route: %+v (%d)", nav.Current, opened)
	}
}

// Covering a page stops its builds, preserves both scroll positions, and keeps root shortcuts ahead of focused buttons.
func TestCoveredWidgetsAndRootShortcut(t *testing.T) {
	var s State
	covered := false
	pageBuilds, navBuilds, overlayBuilds, clicks, keys := 0, 0, 0, 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		s.Build(c, Layout{Palette: theme.For(c.Theme(), ui.Color{}), Covered: covered,
			BindRoot: func(c *ui.Context, root ui.Element) {
				root.OnShortcut(0, ui.KeySpace, func() { keys++ })
				c.OnShortcut(0, ui.KeySpace, func() { keys++ })
			},
			Sidebar: func(c *ui.Context) {
				navBuilds++
				ui.Scroll(c.Key("nav")).TrackScroll(&s.NavScroll).Fill().Children(func() {
					for i := range 50 {
						ui.Text(c, fmt.Sprint(i)).Height(30)
					}
				})
			},
			Page: func(c *ui.Context) {
				pageBuilds++
				ui.Scroll(c.Key("page")).TrackScroll(&s.PageScroll).Fill().Padding(60, 0, PlayerBarHeight, 0).Children(func() {
					ui.Button(c.Key("action"), "action").OnClick(func() { clicks++ })
					for i := range 100 {
						ui.Text(c, fmt.Sprint(i)).Height(30)
					}
				})
			}, Player: nothing, NowPlaying: func(c *ui.Context) { overlayBuilds++ }})
	}, 1000, 700)
	if err := tt.Click("action"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeySpace)
	if clicks != 1 || keys != 1 {
		t.Fatalf("focused button stole root command: clicks=%d keys=%d", clicks, keys)
	}
	tt.Scroll(100, 400, 0, 200)
	tt.Scroll(650, 400, 0, 400)
	tt.Advance(time.Second)
	tt.Frame()
	pageY, navY := s.PageScroll.Y, s.NavScroll.Y
	if pageY <= 0 || navY <= 0 {
		t.Fatal("fixture did not scroll")
	}
	covered = true
	tt.Frame()
	pageBefore, navBefore, overlayBefore := pageBuilds, navBuilds, overlayBuilds
	tt.Frame()
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	if pageBuilds != pageBefore || navBuilds != navBefore || overlayBuilds <= overlayBefore || keys != 2 {
		t.Fatalf("covered page rebuilt or commands stopped: page %d/%d sidebar %d/%d overlay %d/%d keys %d", pageBuilds, pageBefore, navBuilds, navBefore, overlayBuilds, overlayBefore, keys)
	}
	covered = false
	tt.Frame()
	if s.PageScroll.Y != pageY || s.NavScroll.Y != navY {
		t.Fatalf("return reset widget state: page %g/%g nav %g/%g", s.PageScroll.Y, pageY, s.NavScroll.Y, navY)
	}
}
