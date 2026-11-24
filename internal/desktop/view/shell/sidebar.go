package shell

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/navigation"
)

// Navigation is a read-only projection and commands of the application's navigator.
// The shell never mutates history or coordinates feature requests.
type Navigation struct {
	Current                      navigation.Route
	LastSearch                   *navigation.Route
	Playing, CanBack, CanForward bool
	Open                         func(navigation.Route)
	Back, Forward                func()
}

// Sidebar fills the three feature-owned regions around the navigation links.
type Sidebar struct{ Search, Library, Account func(*ui.Context) }

func (s *State) Sidebar(c *ui.Context, p components.Palette, n Navigation, slots Sidebar) {
	// Under the window buttons where they sit over the sidebar, as the
	// traffic lights do; elsewhere, as close to the top as to the sides.
	top := float32(14)
	if c.TitleBar().Left > 0 {
		top = TopBar(c) + 4
	}
	// Where nothing in it takes the press, the sidebar moves the window, as
	// the title bar it reaches up into would.
	ui.Column(c).Fill().Padding(top, 14, 14, 14).Gap(4).DragWindow().Children(func() {
		slots.Search(c)

		ui.Scroll(c.Key("nav")).TrackScroll(&s.NavScroll).Grow(1).Margin(0, -6).Padding(0, 6).Gap(4).Children(func() {
			navItem(c, p, n, components.IconHome, "为你推荐", navigation.Route{Page: navigation.Home})
			navItem(c, p, n, components.IconDaily, "每日推荐", navigation.Route{Page: navigation.Daily})
			navItem(c, p, n, components.IconHistory, "最近播放", navigation.Route{Page: navigation.Recent})
			navItem(c, p, n, components.IconRadio, "私人 FM", navigation.Route{Page: navigation.FM})
			navItem(c, p, n, components.IconCharts, "排行榜", navigation.Route{Page: navigation.Charts})
			navItem(c, p, n, components.IconPodcast, "播客", navigation.Route{Page: navigation.Podcasts})
			navItem(c, p, n, components.IconNote, "本地歌曲", navigation.Route{Page: navigation.Local})
			navItem(c, p, n, components.IconCloud, "音乐网盘", navigation.Route{Page: navigation.Cloud})
			if n.LastSearch != nil && n.LastSearch.Query != "" {
				navItem(c, p, n, components.IconSearch, "搜索结果", *n.LastSearch)
			}
			slots.Library(c)
		})

		slots.Account(c)
	})
}

func navItem(c *ui.Context, p components.Palette, n Navigation, icon *ui.SVG, label string, r navigation.Route) {
	selected := n.Current.Page == r.Page && !n.Playing
	b := ui.ButtonBase(c.Key(label)).Height(34).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.Press
	case b.Hovered():
		bg = p.Hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	color := p.Text
	if selected {
		color = p.Accent
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17).TextColor(color)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Text)
	})
	if b.Clicked() {
		n.Open(r)
	}
}
