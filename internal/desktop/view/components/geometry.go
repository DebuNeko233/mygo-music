package components

import (
	"github.com/egoist/mygo/ui"
)

// CoverRect returns where an image of w×h goes to cover r, centered.
func CoverRect(r ui.Rect, w, h float32) ui.Rect {
	s := max(r.W/w, r.H/h)
	return ui.Rect{X: r.X + (r.W-w*s)/2, Y: r.Y + (r.H-h*s)/2, W: w * s, H: h * s}
}
func Clamp01(v float32) float32 { return min(1, max(0, v)) }

func BoolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
