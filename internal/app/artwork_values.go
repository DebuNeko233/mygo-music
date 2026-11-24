package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
)

// bitmap returns the cover of art, nil while it loads.
func bitmap(art components.Image) *ui.Bitmap   { return components.Bitmap(art) }
func backdrop(art components.Image) *ui.Bitmap { return components.Blurred(art) }

func (a *artwork) ImageBitmap() *ui.Bitmap {
	if a == nil {
		return nil
	}
	return a.bmp
}
func (a *artwork) ImageBackdrop() *ui.Bitmap {
	if a == nil {
		return nil
	}
	return a.backdrop
}
func (a *artwork) ImageTint() ui.Color {
	if a == nil {
		return ui.Color{}
	}
	return a.tint
}
func (a *artwork) ImageFailed() bool { return a == nil || a.failed }
func viewImage(art *artwork) components.Image {
	if art == nil {
		return nil
	}
	return art
}
