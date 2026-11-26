/*
构建首页、卡片和歌单列表加载时的骨架布局。
*/

package page

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
)

func (a *Page) HomeSkeleton(c *ui.Context, s components.Shimmer, signedIn bool) {
	s.Bone(c.Key("daily"), components.DailyRadius).Height(components.DailyHeight)
	cards := a.Columns(c, 168)
	if signedIn {
		a.Section(c, "今日为你推荐")
		a.PlaylistCardsSkeleton(c.Key("rec"), s, cards, 168, false)
	}
	a.Section(c, "官方榜单")
	a.ChartCardsSkeleton(c, s)
	a.Section(c, "推荐歌单")
	a.PlaylistCardsSkeleton(c.Key("featured"), s, cards*2, 168, false)
}

func (a *Page) ChartCardsSkeleton(c *ui.Context, s components.Shimmer) {
	cols := min(4, max(2, a.Columns(c, 260)))
	ui.Grid(c).Columns(cols).Gap(20).Children(func() {
		for i := range 4 {
			ui.Row(c.Key(i)).Gap(14).Padding(12).Radius(14).AlignItems(ui.Center).Background(a.Palette.Hover).Children(func() {
				s.Bone(c, 9).Size(84, 84)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
					s.Text(c, 14, 1.3).WidthPercent(46)
					for j := range 3 {
						s.Text(c, 12, 1.3).WidthPercent(components.BoneWidth(i*3 + j))
					}
				})
			})
		}
	})
}

// PlaylistCardsSkeleton is n of playlistCard's cards, in columns at least
// min DIPs wide, with a line for how often they update if freq.
func (a *Page) PlaylistCardsSkeleton(c *ui.Context, s components.Shimmer, n int, min float32, freq bool) {
	ui.Grid(c).Columns(a.Columns(c, min)).GapX(20).GapY(28).Children(func() {
		for i := range n {
			ui.Column(c.Key(i)).Gap(10).AlignItems(ui.Stretch).Children(func() {
				s.Bone(c, 10).AspectRatio(1)
				ui.Column(c).Gap(0).Children(func() {
					s.Text(c, 13, 1.35).WidthPercent(92)
					s.Text(c, 13, 1.35).WidthPercent(components.BoneWidth(i))
				})
				if freq {
					s.Text(c, 12, 1.3).WidthPercent(40).Margin(-6, 0, 0, 0)
				}
			})
		}
	})
}

// TrackSkeleton is trackRows while its tracks load: rows of blocks under
// the header, which may hold blocks of its own.
func (a *Page) TrackSkeleton(c *ui.Context, s components.Shimmer, covers bool, header func(c *ui.Context)) {
	w, h := c.Size()
	wide := w-shell.SidebarWidth > 820
	top, bottom := a.Insets(c)
	a.ScrollState.UnderTop, a.ScrollState.UnderBottom = false, true
	ui.Column(c.Key("skeleton")).Grow(1).Padding(top+shell.PageGap, shell.PagePad-12, bottom+32, shell.PagePad-12).Children(func() {
		a.ScrollState.StartMark(c)
		header(c)
		for i := range max(8, int(h/54)) {
			ui.Row(c.Key(i)).Height(54).Shrink(0).PaddingX(12).Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(26).Center().Children(func() { s.Text(c, 13, 1.3).Width(12) })
				if covers {
					s.Bone(c, 6).Size(38, 38)
				}
				ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
					s.Text(c, 14, 1.3).WidthPercent(components.BoneWidth(i) * 0.8)
					s.Text(c, 12.5, 1.3).WidthPercent(components.BoneWidth(i+5) * 0.55)
				})
				if wide {
					ui.Box(c).Grow(2).Basis(0).MinWidth(0).Children(func() {
						s.Text(c, 12.5, 1.3).WidthPercent(components.BoneWidth(i+3) * 0.8)
					})
				}
				ui.Box(c).Size(32, 32)
				ui.Row(c).Width(44).Justify(ui.End).Children(func() { s.Text(c, 12.5, 1.3).Width(30) })
			})
		}
	})
}

// PlaylistHeaderSkeleton is playlistHeader while the playlist loads.
func (a *Page) PlaylistHeaderSkeleton(c *ui.Context, s components.Shimmer) {
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		s.Bone(c, 14).Size(212, 212)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			s.Text(c, 12, 1.3).Width(36)
			s.Text(c, 30, 1.2).WidthPercent(48)
			s.Text(c, 13, 1.3).Width(180)
			ui.Column(c).Margin(2, 0, 0, 0).Children(func() {
				s.Text(c, 12.5, 1.45).WidthPercent(78)
				s.Text(c, 12.5, 1.45).WidthPercent(52)
			})
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				s.Bone(c, 18).Size(92, 36)
				s.Bone(c, 18).Size(120, 36)
			})
		})
	})
}
