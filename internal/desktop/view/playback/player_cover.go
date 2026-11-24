package playback

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func (a *View) PlayerBarArtwork(c *ui.Context, tr *netease.Track) {
	const size, radius = 48, 8
	p := a.palette()
	requested := a.BarArtwork(tr)
	hasCover := a.HasTrackCover(tr)
	ui.Box(c.Key("bar-cover")).Size(size, size).Radius(radius).Shadow(0, 2, 8, 0, p.CardShade).Children(func() {
		a.BarCover.draw(c, requested, hasCover, size, radius, p.Hover.Over(p.Background), p.Faint)
	})
}
