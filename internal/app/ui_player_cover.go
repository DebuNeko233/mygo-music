package app

import "github.com/egoist/mygo/ui"

func (a *app) playerBarArtwork(c *ui.Context, url string) {
	const size, radius = 48, 8
	p := a.pal
	var requested *artwork
	hasCover := url != ""
	if tr := a.pl.Current(); tr != nil && tr.IsLocal() {
		requested, hasCover = a.trackArtwork(tr, barCoverPixels, false), a.hasTrackCover(tr)
	} else {
		requested = a.playbackArtwork(url, barCoverRequest)
	}
	ui.Box(c.Key("bar-cover")).Size(size, size).Radius(radius).Shadow(0, 2, 8, 0, p.cardShade).Children(func() {
		a.barCover.draw(c, requested, hasCover, size, radius, p.hover.Over(p.bg), p.faint)
	})
}
