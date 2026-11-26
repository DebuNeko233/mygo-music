/*
读取、缩放并编码用户背景图片，限制保存尺寸并提供图片类型判断。
*/

package artwork

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// backdropSide is how long the longer side of a picture is kept, in
// pixels: larger costs that many more bytes in the memory, and no display
// shows more.
const backdropSide = 2560

// PictureExtensions are the extensions of the pictures a page can stand on: the
// ones the app's decoders read.
var PictureExtensions = []string{"png", "jpg", "jpeg", "webp", "gif"}

// IsPicture reports whether path names a picture the app can read.
func IsPicture(path string) bool {
	return slices.Contains(PictureExtensions, strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
}

// DecodePicture reads and decodes the picture at path, kept no larger than a
// picture shows, or returns nil for one that cannot be read. The bitmap
// keeps the bytes its pixels decode from again, so that they can go once
// the GPU has them: what stays costs a fraction of the pixels.
func DecodePicture(path string) *Picture {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	configErr := err
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	cost := decodeByteLimit
	if configErr == nil {
		cost, err = decodeCost(cfg, backdropSide, info.Size())
		if err != nil {
			return nil
		}
	}
	// Animated WebP needs its first frame extracted before DecodeConfig
	// understands it. Unknown headers take the whole allowance, then use
	// the same decoder below; they never bypass admission or add a decoder.
	release := pictureDecodes.reserve(cost)
	defer release()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	// Read the size inspected above from the same open file, without an
	// incrementally growing buffer retaining another copy of a large PNG.
	b := make([]byte, info.Size())
	if _, err := io.ReadFull(f, b); err != nil {
		return nil
	}
	img, err := DecodeImage(b)
	if err != nil {
		return nil
	}
	img = shrinkPicture(img, backdropSide)
	bounds := img.Bounds()
	encoded := encodePicture(img)
	bmp, err := ui.DecodeBitmap(encoded)
	if err != nil {
		bmp = ui.NewBitmap(img)
	}
	return &Picture{Bitmap: bmp, W: float32(bounds.Dx()), H: float32(bounds.Dy()), encodedBytes: cap(encoded)}
}

// encodePicture returns data a picture's pixels decode from again once it
// drops them: JPEG for an opaque picture, which costs a tenth of what its
// pixels do, and PNG for one with transparency, which JPEG cannot keep.
func encodePicture(img image.Image) []byte {
	var buf bytes.Buffer
	if opaque, ok := img.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err == nil {
			return bytes.Clone(buf.Bytes())
		}
		buf.Reset()
	}
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return bytes.Clone(buf.Bytes())
}

// shrinkPicture scales img down so that its longer side is at most side
// pixels, averaging the pixels it lets go.
func shrinkPicture(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= side || w < 1 || h < 1 {
		return img
	}
	k := (long + side - 1) / side
	dw, dh := max(w/k, 1), max(h/k, 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var r, g, bl, al, n uint32
			for dy := 0; dy < k; dy++ {
				for dx := 0; dx < k; dx++ {
					cr, cg, cb, ca := img.At(min(b.Min.X+x*k+dx, b.Max.X-1), min(b.Min.Y+y*k+dy, b.Max.Y-1)).RGBA()
					r, g, bl, al, n = r+cr, g+cg, bl+cb, al+ca, n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(bl / n >> 8), A: uint8(al / n >> 8)})
		}
	}
	return dst
}

// Picture is one of the pictures a page stands on.
type Picture struct {
	Bitmap       *ui.Bitmap
	W, H         float32 // its pixels
	encodedBytes int
}
