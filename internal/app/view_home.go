package app

import (
	"github.com/egoist/mygo/ui"

	homeview "netease-music-mygo/internal/desktop/view"
	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/shell"
)

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
func (a *app) homePage(c *ui.Context)   { a.showHome(c, false) }
func (a *app) chartsPage(c *ui.Context) { a.showHome(c, true) }
