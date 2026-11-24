package components

import (
	"github.com/egoist/mygo/ui"
)

// Image is the read-only image surface shared by players and page renderers.
// Implementations remain owned by the artwork cache and are comparable pointers.
type Image interface {
	ImageBitmap() *ui.Bitmap
	ImageBackdrop() *ui.Bitmap
	ImageTint() ui.Color
	ImageFailed() bool
}

func Bitmap(image Image) *ui.Bitmap {
	if image == nil {
		return nil
	}
	return image.ImageBitmap()
}
func Blurred(image Image) *ui.Bitmap {
	if image == nil {
		return nil
	}
	return image.ImageBackdrop()
}

const BarCoverPixels = 120
const NowPlayingCoverPixels = 600
