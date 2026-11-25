package artwork

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"netease-music-mygo/internal/netease"
)

// pixelBytes is how many bytes a bitmap's pixels take, with those of the
// smaller copies it shows from, about a third as many again.
func pixelBytes(b *ui.Bitmap) int {
	if b == nil {
		return 0
	}
	w, h := b.Size()
	if w <= 0 || h <= 0 {
		return 0
	}
	pixels := 4 * w * h
	return pixels + pixels/3
}

// LoadURL fetches the cover at url, as a bitmap to draw and an image to
// measure, with the bytes of its data.
func LoadURL(url string) (*ui.Bitmap, image.Image, int, error) {
	b, err := netease.Fetch(url)
	if err != nil {
		return nil, nil, 0, err
	}
	return DecodeSized(b, netease.CoverRequestSize(url))
}

func DecodeSized(b []byte, limit int) (*ui.Bitmap, image.Image, int, error) {
	b = stillCover(b)
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, nil, 0, err
	}
	bounds := img.Bounds()
	if w, h := bounds.Dx(), bounds.Dy(); limit > 0 && max(w, h) > limit {
		long := max(w, h)
		small := image.NewRGBA(image.Rect(0, 0, max(1, w*limit/long), max(1, h*limit/long)))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, bounds, xdraw.Src, nil)
		// The small bitmap owns its pixels. It need not keep the large
		// encoded download around to reconstruct them.
		return ui.NewBitmap(small), small, 0, nil
	}
	bmp, err := ui.DecodeBitmap(b)
	return bmp, img, len(b), err
}

func DecodeImage(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(stillCover(b)))
	return img, err
}

// stillCover returns the data of a cover that the decoders read. Some
// covers are animated WebP, whatever their name says, which they cannot:
// those show their first frame.
func stillCover(b []byte) []byte {
	if still := firstWebPFrame(b); still != nil {
		return still
	}
	return b
}

// firstWebPFrame returns the first frame of an animated WebP as a still
// WebP of its own, or nil if b is not one.
func firstWebPFrame(b []byte) []byte {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil
	}
	chunk := func(b []byte) (name string, body, rest []byte, ok bool) {
		if len(b) < 8 {
			return "", nil, nil, false
		}
		n := int(binary.LittleEndian.Uint32(b[4:8]))
		if n > len(b)-8 {
			return "", nil, nil, false
		}
		end := min(len(b), 8+n+n&1)
		return string(b[:4]), b[8 : 8+n], b[end:], true
	}
	riff := func(chunks ...[]byte) []byte {
		body := []byte("WEBP")
		for _, c := range chunks {
			body = append(body, c...)
		}
		out := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body)))
		return append(out, body...)
	}
	for rest := b[12:]; ; {
		name, body, next, ok := chunk(rest)
		if !ok {
			return nil
		}
		rest = next
		if name != "ANMF" || len(body) < 16 {
			continue
		}
		// A frame: where it sits, its size, how long it shows, then its
		// image as the chunks of a still WebP, its alpha first if any.
		w, h := body[6:9], body[9:12]
		frame := body[16:]
		var alpha []byte
		for len(frame) > 0 {
			name, _, after, ok := chunk(frame)
			if !ok {
				return nil
			}
			whole := frame[:len(frame)-len(after)]
			switch name {
			case "ALPH":
				alpha = whole
			case "VP8L":
				return riff(whole)
			case "VP8 ":
				if alpha == nil {
					return riff(whole)
				}
				// Alpha needs the extended header, which carries the size.
				vp8x := []byte{'V', 'P', '8', 'X', 10, 0, 0, 0, 0x10, 0, 0, 0}
				vp8x = append(append(vp8x, w...), h...)
				return riff(vp8x, alpha, whole)
			}
			frame = after
		}
		return nil
	}
}

// shrink averages img down to n×n pixels.
func shrink(img image.Image, n int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			x0, x1 := b.Min.X+x*b.Dx()/n, b.Min.X+(x+1)*b.Dx()/n
			y0, y1 := b.Min.Y+y*b.Dy()/n, b.Min.Y+(y+1)*b.Dy()/n
			var r, g, bl, cnt uint32
			for yy := y0; yy < max(y1, y0+1); yy += max(1, (y1-y0)/4) {
				for xx := x0; xx < max(x1, x0+1); xx += max(1, (x1-x0)/4) {
					cr, cg, cb, _ := img.At(xx, yy).RGBA()
					r, g, bl, cnt = r+cr>>8, g+cg>>8, bl+cb>>8, cnt+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / cnt), uint8(g / cnt), uint8(bl / cnt), 255})
		}
	}
	return out
}

// DominantColor returns the most vivid color that covers much of img: the
// hue that its saturated pixels share most, at their mean.
func DominantColor(img image.Image) ui.Color {
	small := shrink(img, 24)
	const buckets = 18
	var weight [buckets]float64
	var sum [buckets][3]float64
	var gray [3]float64
	for i := 0; i < len(small.Pix); i += 4 {
		r, g, b := float64(small.Pix[i])/255, float64(small.Pix[i+1])/255, float64(small.Pix[i+2])/255
		h, s, v := HSV(r, g, b)
		gray[0], gray[1], gray[2] = gray[0]+r, gray[1]+g, gray[2]+b
		w := s * s * v
		if s < 0.15 || v < 0.15 {
			continue
		}
		k := int(h/360*buckets) % buckets
		weight[k] += w
		sum[k][0], sum[k][1], sum[k][2] = sum[k][0]+r*w, sum[k][1]+g*w, sum[k][2]+b*w
	}
	best := 0
	for k := range weight {
		if weight[k] > weight[best] {
			best = k
		}
	}
	if weight[best] < 2 {
		n := float64(len(small.Pix) / 4)
		return ui.RGB(uint8(gray[0]/n*255), uint8(gray[1]/n*255), uint8(gray[2]/n*255))
	}
	w := weight[best]
	return ui.RGB(uint8(sum[best][0]/w*255), uint8(sum[best][1]/w*255), uint8(sum[best][2]/w*255))
}

// Blur returns img shrunk and blurred, more saturated, to fill a
// backdrop behind what plays, scaled up as smooth as it is.
func Blur(img image.Image) *image.RGBA {
	const n = 48
	small := shrink(img, n)
	px := make([][3]float64, n*n)
	for i := range px {
		r, g, b := float64(small.Pix[4*i]), float64(small.Pix[4*i+1]), float64(small.Pix[4*i+2])
		// More saturated, as light through colored glass.
		l := 0.3*r + 0.59*g + 0.11*b
		px[i] = [3]float64{l + (r-l)*1.5, l + (g-l)*1.5, l + (b-l)*1.5}
	}
	tmp := make([][3]float64, n*n)
	for range 3 {
		boxBlur(px, tmp, n, 5)
	}
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	for i, p := range px {
		for c := range 3 {
			out.Pix[4*i+c] = uint8(math.Max(0, math.Min(255, p[c])))
		}
		out.Pix[4*i+3] = 255
	}
	return out
}

// boxBlur blurs px of n×n pixels by a box of radius r, across then down,
// clamping at the edges.
func boxBlur(px, tmp [][3]float64, n, r int) {
	at := func(i int) int { return max(0, min(n-1, i)) }
	for y := range n {
		for x := range n {
			var s [3]float64
			for k := -r; k <= r; k++ {
				p := px[y*n+at(x+k)]
				s[0], s[1], s[2] = s[0]+p[0], s[1]+p[1], s[2]+p[2]
			}
			d := float64(2*r + 1)
			tmp[y*n+x] = [3]float64{s[0] / d, s[1] / d, s[2] / d}
		}
	}
	for y := range n {
		for x := range n {
			var s [3]float64
			for k := -r; k <= r; k++ {
				p := tmp[at(y+k)*n+x]
				s[0], s[1], s[2] = s[0]+p[0], s[1]+p[1], s[2]+p[2]
			}
			d := float64(2*r + 1)
			px[y*n+x] = [3]float64{s[0] / d, s[1] / d, s[2] / d}
		}
	}
}

func (a *Image) ImageBitmap() *ui.Bitmap {
	if a == nil {
		return nil
	}
	return a.Bitmap
}

func (a *Image) ImageBackdrop() *ui.Bitmap {
	if a == nil {
		return nil
	}
	return a.Backdrop
}

func (a *Image) ImageTint() ui.Color {
	if a == nil {
		return ui.Color{}
	}
	return a.Tint
}

func (a *Image) ImageFailed() bool { return a == nil || a.Failed }
