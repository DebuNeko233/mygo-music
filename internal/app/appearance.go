package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
)

// accent is a colour the app can take, named for the settings, in the
// lightness each appearance needs of it.
type accent struct {
	name        string
	dark, light ui.Color
}

// accents are the colours the app can take, the first being the one its
// appearance gives.
var accents = []accent{
	{"胭脂", ui.Oklch(0.66, 0.2, 22), ui.Oklch(0.6, 0.21, 24)},
	{"柑橘", ui.Oklch(0.68, 0.18, 55), ui.Oklch(0.62, 0.19, 52)},
	{"琥珀", ui.Oklch(0.72, 0.17, 90), ui.Oklch(0.64, 0.17, 88)},
	{"青苔", ui.Oklch(0.68, 0.17, 140), ui.Oklch(0.6, 0.16, 142)},
	{"松石", ui.Oklch(0.7, 0.14, 190), ui.Oklch(0.6, 0.13, 192)},
	{"湖蓝", ui.Oklch(0.68, 0.16, 230), ui.Oklch(0.6, 0.15, 232)},
	{"靛青", ui.Oklch(0.64, 0.19, 265), ui.Oklch(0.56, 0.19, 267)},
	{"紫罗兰", ui.Oklch(0.66, 0.19, 300), ui.Oklch(0.58, 0.18, 302)},
	{"玫瑰", ui.Oklch(0.68, 0.19, 340), ui.Oklch(0.6, 0.18, 342)},
	{"石墨", ui.Oklch(0.6, 0.02, 260), ui.Oklch(0.5, 0.02, 260)},
}

// accentChosen is the accent the listener chose, empty for the one its
// appearance gives. Set from the settings, on the main thread, before the
// pages are built.
var accentChosen string

// accentColor returns the colour called name, or zero for one that names
// none of them.
func accentColor(name string, dark bool) ui.Color {
	for _, a := range accents {
		if a.name != name {
			continue
		}
		if dark {
			return a.dark
		}
		return a.light
	}
	return ui.Color{}
}

// Each request owns an entry so a discarded read cannot overwrite a later
// request for the same path.
type backdropEntry struct {
	pic *playbackview.BackdropPicture // nil while loading or if decoding failed
}

// backdropSide is how long the longer side of a picture is kept, in
// pixels: larger costs that many more bytes in the memory, and no display
// shows more.
const backdropSide = 2560

// pictureExts are the extensions of the pictures a page can stand on: the
// ones the app's decoders read.
var pictureExts = []string{"png", "jpg", "jpeg", "webp", "gif"}

// isPicture reports whether path names a picture the app can read.
func isPicture(path string) bool {
	return slices.Contains(pictureExts, strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
}

// pictureList names the pictures chosen, as the settings row shows them.
func pictureList(paths []string) string {
	if len(paths) == 0 {
		return "未选择，也可以把图片拖到这里"
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	if len(names) > 3 {
		return fmt.Sprintf("已选 %d 张：%s 等", len(names), strings.Join(names[:3], "、"))
	}
	return "已选：" + strings.Join(names, "、")
}

// addPictures adds those of files a page can stand on to its pictures,
// once each, and keeps the settings.
func (a *app) addPictures(files []string, paths *[]string) {
	added := false
	for _, path := range files {
		if !isPicture(path) || slices.Contains(*paths, path) {
			continue
		}
		*paths = append(*paths, path)
		added = true
	}
	if added {
		a.changed()
	}
}

// preloadPictures starts reading the chosen backdrops before their first
// frame. The home and playing paths overlap at most once in the read queue.
func (a *app) preloadPictures() {
	for _, path := range slices.Concat(a.settings.BackdropHome, a.settings.BackdropPlaying) {
		a.loadPicture(path)
	}
}

func (a *app) loadPicture(path string) {
	if a.pics[path] != nil {
		return
	}
	if a.pics == nil {
		a.pics = make(map[string]*backdropEntry)
	}
	entry := &backdropEntry{}
	a.pics[path] = entry
	go a.readPicture(path, entry)
}

// pictures returns the pictures of the paths that could be read, skipping
// those that could not, and starts reading the rest.
func (a *app) pictures(paths []string) []*playbackview.BackdropPicture {
	out := make([]*playbackview.BackdropPicture, 0, len(paths))
	for _, path := range paths {
		if entry := a.pics[path]; entry == nil {
			a.loadPicture(path)
		} else if entry.pic != nil {
			out = append(out, entry.pic)
		}
	}
	return out
}

// Both pages can build in the same frame. Keep their configured pictures
// together so neither page forces the other to decode its pictures again.
func (a *app) forgetPictures() {
	for path := range a.pics {
		if !a.pictureChosen(path) {
			delete(a.pics, path)
		}
	}
}

func (a *app) pictureChosen(path string) bool {
	return slices.Contains(a.settings.BackdropHome, path) || slices.Contains(a.settings.BackdropPlaying, path)
}

// readPicture reads and decodes the picture at path, off the main thread,
// and keeps it, or that it cannot be read.
func (a *app) readPicture(path string, entry *backdropEntry) {
	pic := decodePicture(path)
	a.postIfChanged(func() bool {
		if a.pics[path] != entry {
			return false
		}
		entry.pic = pic
		return pic != nil
	})
}

// decodePicture reads and decodes the picture at path, kept no larger than a
// picture shows, or returns nil for one that cannot be read. The bitmap
// keeps the bytes its pixels decode from again, so that they can go once
// the GPU has them: what stays costs a fraction of the pixels.
func decodePicture(path string) *playbackview.BackdropPicture {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	img, err := decodeImage(b)
	if err != nil {
		return nil
	}
	img = shrinkPicture(img, backdropSide)
	bounds := img.Bounds()
	bmp, err := ui.DecodeBitmap(encodePicture(img))
	if err != nil {
		bmp = ui.NewBitmap(img)
	}
	return &playbackview.BackdropPicture{Bitmap: bmp, W: float32(bounds.Dx()), H: float32(bounds.Dy())}
}

// encodePicture returns data a picture's pixels decode from again once it
// drops them: JPEG for an opaque picture, which costs a tenth of what its
// pixels do, and PNG for one with transparency, which JPEG cannot keep.
func encodePicture(img image.Image) []byte {
	var buf bytes.Buffer
	if opaque, ok := img.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err == nil {
			return buf.Bytes()
		}
		buf.Reset()
	}
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
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

func paletteFor(t *ui.Theme) components.Palette {
	return components.PaletteFor(t, accentColor(accentChosen, t.Dark))
}
