package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
)

type shimmer = components.Shimmer

func (a *app) pageShimmer(c *ui.Context) shimmer { return components.PageShimmer(c, a.pal.Dark) }
func npShimmer(c *ui.Context) shimmer            { return components.PlayerShimmer(c) }
func (a *app) homeSkeleton(c *ui.Context, s shimmer) {
	a.views.page.HomeSkeleton(c, s, a.account.Profile != nil)
}
func (a *app) chartCardsSkeleton(c *ui.Context, s shimmer) { a.views.page.ChartCardsSkeleton(c, s) }
func (a *app) playlistCardsSkeleton(c *ui.Context, s shimmer, n int, min float32, freq bool) {
	a.views.page.PlaylistCardsSkeleton(c, s, n, min, freq)
}
func (a *app) trackSkeleton(c *ui.Context, s shimmer, covers bool, header func(*ui.Context)) {
	a.views.page.TrackSkeleton(c, s, covers, header)
}
func (a *app) playlistHeaderSkeleton(c *ui.Context, s shimmer) {
	a.views.page.PlaylistHeaderSkeleton(c, s)
}
