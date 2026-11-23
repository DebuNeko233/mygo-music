package components

import (
	"time"

	"github.com/egoist/mygo/ui"
)

func CoverPlaceholder(c *ui.Context, size, radius, opacity float32, bg, fg ui.Color) {
	if opacity <= 0 {
		return
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius).
		Background(bg).Center().Opacity(opacity).Children(func() {
		ui.Icon(c, IconNote).Size(size*0.3, size*0.3).TextColor(fg)
	})
}

// ArtFade is how an artwork fades in over its placeholder.
var ArtFade = ui.Animation{Duration: 320 * time.Millisecond}

// ArtIn returns how far bmp has faded in on el, the element that shows it
// or its placeholder: over a moment once it loads while el shows, and at
// once if it was ready as el first showed.
func ArtIn(el ui.Element, bmp *ui.Bitmap) float32 {
	if bmp == nil {
		return el.Jump("art", 0)
	}
	return el.AnimateTo("art", 1, ArtFade)
}

// ArtImage fills its parent with bmp, its corners rounded by radius, as
// far faded in as in.
func ArtImage(c *ui.Context, bmp *ui.Bitmap, radius, in float32) {
	if bmp == nil || in <= 0 {
		return
	}
	img := ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Radius(radius)
	if in < 1 {
		img.Opacity(in)
	}
}
