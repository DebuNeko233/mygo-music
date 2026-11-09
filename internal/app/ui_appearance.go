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
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
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

// backdropPicture is one of the pictures a page stands on.
type backdropPicture struct {
	bmp  *ui.Bitmap
	w, h float32 // its pixels
}

// backdropSide is how long the longer side of a picture is kept, in
// pixels: larger costs that many more bytes in the memory, and no display
// shows more.
const backdropSide = 2560

// backdropLimit is how many pictures stay in the memory at a time: a
// listener who chose many sees them all, reading one again when it comes
// back after others pushed it out.
const backdropLimit = 8

// backdropRelease is how long a picture keeps its pixels once read: past
// the frames that first show it.
const backdropRelease = 5 * time.Second

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

// pictures returns the pictures of the paths that could be read, skipping
// those that could not, and starts reading the rest.
func (a *app) pictures(paths []string) []*backdropPicture {
	out := make([]*backdropPicture, 0, len(paths))
	for _, path := range paths {
		pic, done := a.pics[path]
		if !done {
			if a.pics == nil {
				a.pics = map[string]*backdropPicture{}
			}
			// On its way: the pages standing on it show it as it lands.
			a.pics[path] = nil
			go a.readPicture(path)
			continue
		}
		if pic != nil {
			out = append(out, pic)
		}
	}
	a.forgetPictures(paths)
	return out
}

// forgetPictures drops the pictures no page stands on anymore, and those
// past backdropLimit that the page drawn now does not show: choosing other
// pictures must not keep every one ever chosen in the memory.
func (a *app) forgetPictures(showing []string) {
	if len(a.pics) == 0 {
		return
	}
	chosen := make(map[string]bool, len(showing))
	for _, path := range showing {
		chosen[path] = true
	}
	for _, path := range a.settings.BackdropHome {
		chosen[path] = true
	}
	for _, path := range a.settings.BackdropPlaying {
		chosen[path] = true
	}
	for path, pic := range a.pics {
		// A picture still on its way stays: what it lands in is looked at
		// again.
		if !chosen[path] || pic != nil && len(a.pics) > backdropLimit && !slices.Contains(showing, path) {
			delete(a.pics, path)
		}
	}
}

// readPicture reads and decodes the picture at path, off the main thread,
// and keeps it, or that it cannot be read.
func (a *app) readPicture(path string) {
	pic := decodePicture(path)
	a.post(func() {
		if a.pics == nil {
			a.pics = map[string]*backdropPicture{}
		}
		a.pics[path] = pic
		if pic != nil {
			// Shown by the frames after it lands, the picture drops the
			// pixels the GPU uploaded: its bytes decode them again if they
			// are needed.
			time.AfterFunc(backdropRelease, func() { mygo.RunOnMain(pic.bmp.ReleasePixels) })
		}
		a.redraw()
	})
}

// decodePicture reads and decodes the picture at path, kept no larger than a
// picture shows, or returns nil for one that cannot be read. The bitmap
// keeps the bytes its pixels decode from again, so that they can go once
// the GPU has them: what stays costs a fraction of the pixels.
func decodePicture(path string) *backdropPicture {
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
	return &backdropPicture{bmp: bmp, w: float32(bounds.Dx()), h: float32(bounds.Dy())}
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

// backdropShown returns which of the pictures shows while tr plays: the
// listener who chose many sees them all, changing with the song.
func backdropShown(pics []*backdropPicture, tr *netease.Track) *backdropPicture {
	if len(pics) == 0 {
		return nil
	}
	if len(pics) == 1 || tr == nil {
		return pics[0]
	}
	return pics[int(tr.ID%int64(len(pics)))]
}

// drawPicture paints pic over r, covering it, as opaque as alpha says.
func drawPicture(p *ui.Painter, r ui.Rect, pic *backdropPicture, alpha float32) {
	if pic == nil || alpha <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	p.ImageRegion(pic.bmp, coverSource(r, pic.w, pic.h), r, ui.ImageOptions{Tint: white.Alpha(alpha)})
}

// coverSource returns the part of a w×h picture that lands in r when the
// picture covers r, centered.
func coverSource(r ui.Rect, w, h float32) ui.Rect {
	if w <= 0 || h <= 0 || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}
	}
	s := max(r.W/w, r.H/h)
	return ui.Rect{X: w/2 - r.W/(2*s), Y: h/2 - r.H/(2*s), W: r.W / s, H: r.H / s}
}
