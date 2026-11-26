/*
使用首页模型呈现推荐内容和排行榜，连接卡片操作与失败重试。
*/

// Home and chart views use their shared ViewModel.
package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
	homevm "netease-music-mygo/internal/ui/viewmodel/home"
)

// View borrows published feature data and the window's widget state for one build.
// Existing artwork and animated cards are composed here without owning their resources.
type Home struct {
	Model                                           *homevm.Home
	Palette                                         components.Palette
	Nickname                                        string
	Offline                                         bool
	Retry                                           func()
	DailyCard                                       func(*ui.Context, time.Time)
	PlaylistCard, ChartCard                         func(*ui.Context, *netease.Playlist)
	HomeSkeleton, ChartsSkeleton, PlaylistsSkeleton func(*ui.Context)
}

func (v Home) Home(c *ui.Context, state *shell.State) {
	loading := len(v.Model.Featured) == 0 && len(v.Model.Charts) == 0
	if loading && (!v.Model.Loading || v.Model.Error != "") {
		shell.Centered(c, v.Palette, v.Offline, v.Model.Error, v.Retry)
		return
	}
	now := c.Now()
	top, bottom := shell.Insets(c)
	ui.Scroll(c.Key("home")).TrackScroll(&state.PageScroll).Grow(1).Padding(top+shell.PageGap, shell.PagePad, bottom+48, shell.PagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		state.StartMark(c)
		defer state.EndMark(c)
		hello := components.Greeting(now)
		if v.Nickname != "" {
			hello += "，" + v.Nickname
		}
		components.Heading(c, v.Palette, hello, fmt.Sprintf("%d月%d日 %s", now.Month(), now.Day(), Weekday(now)))
		if loading {
			v.HomeSkeleton(c)
			return
		}
		v.DailyCard(c, now)

		if len(v.Model.Recommended) > 0 {
			components.Section(c, "今日为你推荐")
			ui.Grid(c).Columns(shell.Columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range v.Model.Recommended[:min(len(v.Model.Recommended), shell.Columns(c, 168)*2)] {
					v.PlaylistCard(c.Key(v.Model.Recommended[i].ID), &v.Model.Recommended[i])
				}
			})
		}

		components.Section(c, "官方榜单")
		v.officialChartGrid(c)

		components.Section(c, "推荐歌单")
		ui.Grid(c).Columns(shell.Columns(c, 168)).GapX(20).GapY(28).Children(func() {
			for i := range v.Model.Featured {
				v.PlaylistCard(c.Key(v.Model.Featured[i].ID), &v.Model.Featured[i])
			}
		})
	})
}

// officialChartGrid shows the leading charts on the home and charts pages.
func (v Home) officialChartGrid(c *ui.Context) {
	official := v.Model.Charts[:min(4, len(v.Model.Charts))]
	cols := min(4, max(2, shell.Columns(c, 260)))
	ui.Grid(c).Columns(cols).Gap(20).Children(func() {
		for i := range official {
			v.ChartCard(c.Key(official[i].ID), &official[i])
		}
	})
}

// Weekday names the day on both the home greeting and daily recommendation card.
func Weekday(t time.Time) string {
	return [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}[t.Weekday()]
}

func (v Home) Charts(c *ui.Context, state *shell.State) {
	loading := len(v.Model.Charts) == 0
	if loading && (!v.Model.Loading || v.Model.Error != "") {
		shell.Centered(c, v.Palette, v.Offline, v.Model.Error, v.Retry)
		return
	}
	top, bottom := shell.Insets(c)
	ui.Scroll(c.Key("charts")).TrackScroll(&state.PageScroll).Grow(1).Padding(top+shell.PageGap, shell.PagePad, bottom+48, shell.PagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		state.StartMark(c)
		defer state.EndMark(c)
		components.Heading(c, v.Palette, "排行榜", "每日更新")
		if loading {
			v.ChartsSkeleton(c)
			components.Section(c, "全部榜单")
			v.PlaylistsSkeleton(c)
			return
		}
		v.officialChartGrid(c)
		if len(v.Model.Charts) > 4 {
			components.Section(c, "全部榜单")
			rest := v.Model.Charts[4:]
			ui.Grid(c).Columns(shell.Columns(c, 150)).GapX(20).GapY(28).Children(func() {
				for i := range rest {
					v.PlaylistCard(c.Key(rest[i].ID), &rest[i])
				}
			})
		}
	})
}
