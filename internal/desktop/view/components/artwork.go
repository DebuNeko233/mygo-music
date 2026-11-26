/*
提供共享封面、图片和头像控件，统一占位、填充与圆角显示。
*/

package components

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

type Artwork struct {
	Palette    *Palette
	PlayerOpen func() bool
	Load       func(string, bool, ui.Element) *ui.Bitmap
	LoadList   func(string, ui.Element) *ui.Bitmap
}

// Cover shows an artwork, or a placeholder of the same size while it loads.
func (a *Artwork) Cover(c *ui.Context, url string, px int, size, radius float32) ui.Element {
	return a.Image(c, size, radius, func(box ui.Element) *ui.Bitmap {
		return a.Load(netease.Sized(url, px), false, box)
	})
}

// ListCover shows a row's cover as soon as it is ready, including disk restores.
// Its loader also records the visible viewport for bounded return-page retention.
func (a *Artwork) ListCover(c *ui.Context, url string, px int, size, radius float32) ui.Element {
	return a.Image(c, size, radius, func(box ui.Element) *ui.Bitmap {
		return a.LoadList(netease.Sized(url, px), box)
	}, ArtImmediate)
}

func (a *Artwork) Image(c *ui.Context, size, radius float32, load func(ui.Element) *ui.Bitmap, presentation ...ArtPresentation) ui.Element {
	p := *a.Palette
	bg, fg := p.Hover, p.Faint
	if a.PlayerOpen() {
		bg, fg = ui.RGB(255, 255, 255).Alpha(0.08), ui.RGB(255, 255, 255).Alpha(0.35)
	}
	box := ui.Box(c).Size(size, size).Radius(radius).Center()
	bmp := load(box)
	in := ArtIn(box, bmp, presentation...)
	box.Children(func() {
		CoverPlaceholder(c, size, radius, 1-in, bg, fg)
		ArtImage(c, bmp, radius, in)
	})
	return box
}

// coverFill fills its parent with a cover, its corners rounded by radius.
// The cover rounds itself rather than its parent clipping it: a clip moved
// by a transform, as a card lifting under the pointer, is drawn on the CPU.
func (a *Artwork) Fill(c *ui.Context, url string, px int, radius float32) {
	box := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius)
	bmp := a.Load(netease.Sized(url, px), false, box)
	in := ArtIn(box, bmp)
	if in < 1 {
		box.Background(a.Palette.Hover)
	}
	box.Children(func() { ArtImage(c, bmp, radius, in) })
}

// Avatar shows a listener's picture, round, over empty while it loads.
func (a *Artwork) Avatar(c *ui.Context, url string, size float32, empty ui.Color) {
	box := ui.Box(c).Size(size, size).Radius(size / 2).Shrink(0)
	bmp := a.Load(netease.Sized(url, int(size*3)), false, box)
	in := ArtIn(box, bmp)
	if in < 1 {
		box.Background(empty)
	}
	box.Children(func() { ArtImage(c, bmp, size/2, in) })
}
