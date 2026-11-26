/*
连接首页与排行榜模型的加载服务，并协调对应页面的打开与刷新。
*/

package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	homeview "netease-music-mygo/internal/ui/view"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/shell"
	homevm "netease-music-mygo/internal/ui/viewmodel/home"
)

var featuredPlaylists = func() ([]netease.Playlist, error) { return netease.FeaturedPlaylists(30) }

var homeCharts = netease.Charts

var dailySongs = netease.DailySongs

var recommendedPlaylists = netease.RecommendedPlaylists

func (a *app) homeServices() homevm.HomeServices {
	generation := a.account.Generation
	return homevm.HomeServices{
		Personal: a.account.Profile != nil, Cache: a.cacheRequest(""), Now: time.Now,
		Featured: featuredPlaylists, Charts: homeCharts, Daily: dailySongs, Recommended: recommendedPlaylists,
		Post: a.post, Valid: func() bool { return a.account.Generation == generation },
	}
}

func (a *app) loadHome() { a.home.Load(a.homeServices()) }

const pagePad = shell.PagePad

const trackPagePad = pagePad - 12

func (a *app) showHome(c *ui.Context, charts bool) {
	v := homeview.Home{Model: &a.home, Palette: a.pal, Offline: a.network.offline,
		Retry: a.loadHome, DailyCard: a.views.daily.Card, PlaylistCard: a.views.cards.Playlist, ChartCard: a.views.cards.Chart,
		HomeSkeleton: func(c *ui.Context) {
			a.views.page.HomeSkeleton(c, components.PageShimmer(c, a.pal.Dark), a.account.Profile != nil)
		},
		ChartsSkeleton: func(c *ui.Context) { a.views.page.ChartCardsSkeleton(c, components.PageShimmer(c, a.pal.Dark)) },
		PlaylistsSkeleton: func(c *ui.Context) {
			a.views.page.PlaylistCardsSkeleton(c, components.PageShimmer(c, a.pal.Dark), shell.Columns(c, 150)*2, 150, true)
		},
	}
	if a.account.Profile != nil {
		v.Nickname = a.account.Profile.Nickname
	}
	if charts {
		v.Charts(c, &a.shell)
	} else {
		v.Home(c, &a.shell)
	}
}

func (a *app) homePage(c *ui.Context) { a.showHome(c, false) }

func (a *app) chartsPage(c *ui.Context) { a.showHome(c, true) }
