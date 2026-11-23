package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// cover shows an artwork, or a placeholder of the same size while it loads.
func (a *app) cover(c *ui.Context, url string, px int, size, radius float32) ui.Element {
	return a.coverImage(c, size, radius, func(box ui.Element) *artwork {
		return a.art.getInView(netease.Sized(url, px), false, box)
	})
}

func (a *app) coverImage(c *ui.Context, size, radius float32, load func(ui.Element) *artwork) ui.Element {
	p := a.pal
	bg, fg := p.Hover, p.Faint
	if a.playbackState.Open {
		bg, fg = white.Alpha(0.08), white.Alpha(0.35)
	}
	box := ui.Box(c).Size(size, size).Radius(radius).Center()
	bmp := bitmap(load(box))
	in := components.ArtIn(box, bmp)
	box.Children(func() {
		components.CoverPlaceholder(c, size, radius, 1-in, bg, fg)
		components.ArtImage(c, bmp, radius, in)
	})
	return box
}

// bitmap returns the cover of art, nil while it loads.
func bitmap(art *artwork) *ui.Bitmap {
	if art == nil {
		return nil
	}
	return art.bmp
}

// backdrop returns the blurred cover of art, nil while it loads.
func backdrop(art *artwork) *ui.Bitmap {
	if art == nil {
		return nil
	}
	return art.backdrop
}

// pageFoot ends a list loaded a page at a time, asking next for the next
// page once it is within a screen of coming into view: where the last
// failed, it says so, with a button to try again.
func (a *app) pageFoot(c *ui.Context, pg *viewmodel.Page, next func()) {
	foot := ui.Row(c.Key("foot")).Height(56).Gap(12).Center()
	foot.Children(func() {
		switch {
		case pg.Loading:
			ui.Text(c, "正在加载…").FontSize(12.5).TextColor(a.pal.Muted)
		case pg.Error != "":
			ui.Text(c, pg.Error).FontSize(12.5).TextColor(a.pal.Muted)
			if components.IconButton(c.Key("retry"), components.IconReload, 18, a.pal.Muted, "重新加载内容").Tooltip("重新加载内容").Clicked() {
				next()
			}
		}
	})
	_, h := c.Size()
	if b := foot.Bounds(); pg.More && !pg.Loading && pg.Error == "" && b.W > 0 && b.Y < 2*h {
		next()
	}
}
