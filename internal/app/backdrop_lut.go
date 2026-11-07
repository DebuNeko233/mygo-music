package app

import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// A still backdrop keeps its colors, not a window-sized image. Each color
// is 16-bit sRGB, split into two opaque RGBA8 rows; interpolating and then
// decoding them preserves that precision until the final screen pixel.
// 512 samples * 2 rows * 4 bytes = 4 KiB, independent of window size.
// This is the palette's payload, not the renderer's total GPU memory.
const backdropRampSize = 512

type stillBackdrop struct {
	art  *artwork
	tint ui.Color
	ramp *ui.Bitmap
}

// Sample the high and low bytes at their row centers, so filtering only
// interpolates along the ramp. A normalized byte sample is byte/255:
// (high*256 + low)/257 therefore reconstructs the original value/65535.
// Decode and blend the cover and previous song before adding dither;
// quantizing intermediate layers would introduce new band boundaries.
const stillBackdropSource = `package main

var Origin vec2
var Size vec2
var Previous float
var HasCover vec2
var Alpha float

func Fragment(dst vec4, pos vec2) vec4 {
	point := pos + Origin
	uv := point / Size
	// Half-texel centers keep the two packed rows from mixing.
	x := 0.5 + clamp(uv.y, 0, 1)*(image0Size().x-1.0)
	c := (image0Sample(vec2(x, 0.5)).rgb*256.0 + image0Sample(vec2(x, 1.5)).rgb)/257.0
	cover := (uv-vec2(0.5))*Size/max(Size.x, Size.y) + vec2(0.5)
	if HasCover.x > 0 {
		wash := image1Sample(cover*image1Size())
		c = wash.rgb*0.1 + c*(1.0-wash.a*0.1)
	}
	if Previous > 0 {
		old := (image2Sample(vec2(x, 0.5)).rgb*256.0 + image2Sample(vec2(x, 1.5)).rgb)/257.0
		if HasCover.y > 0 {
			wash := image3Sample(cover*image3Size())
			old = wash.rgb*0.1 + old*(1.0-wash.a*0.1)
		}
		c = mix(c, old, Previous)
	}
	c *= 1.0 - 0.18*clamp((uv.y-0.55)/0.45, 0, 1)
	// A fixed +/- half 8-bit step breaks up rounding boundaries. Use
	// physical window pixels, including when drawing only a wall strip:
	// the grain must neither shimmer with time nor jump across masks.
	noise := fract(52.9829189*fract(dot(floor(point*pixelScale()), vec2(0.06711056, 0.00583715)))) - 0.5
	c = clamp(c + vec3(noise/255.0), vec3(0), vec3(1))
	return vec4(c*Alpha, Alpha)
}
`

var stillBackdropShader = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(stillBackdropSource) })

type stillBackdropUniforms struct {
	origin, size, previous, hasCover, alpha shader.Uniform
}

var stillUniforms = sync.OnceValue(func() stillBackdropUniforms {
	s := stillBackdropShader()
	return stillBackdropUniforms{s.Uniform("Origin"), s.Uniform("Size"), s.Uniform("Previous"), s.Uniform("HasCover"), s.Uniform("Alpha")}
})

// prepareStill changes the palette only when its artwork or color changes.
// A resize changes the draw's coordinates, not these textures. Repeated
// builds must not restart a fade; rapid song changes keep only the most
// recent previous state, rather than a chain of retained textures.
func (bd *backdropCache) prepareStill(art *artwork, tint ui.Color, now time.Time) {
	if bd.still != nil && bd.still.art == art && bd.still.tint == tint {
		return
	}
	var ramp *ui.Bitmap
	if bd.still != nil && bd.still.tint == tint {
		ramp = bd.still.ramp
	}
	if ramp == nil {
		ramp = ui.NewBitmap(backdropRamp(tint))
	}
	bd.previousStill = bd.still
	bd.still = &stillBackdrop{art: art, tint: tint, ramp: ramp}
	bd.stillAt = now
}

// stillWeight drops our reference to the previous palette after the
// crossfade; actual GPU resource reclamation belongs to the renderer.
func (bd *backdropCache) stillWeight(now time.Time) float32 {
	if bd.previousStill == nil {
		return 0
	}
	k := float32(now.Sub(bd.stillAt)) / float32(backdropFade)
	if k >= 1 {
		bd.previousStill = nil
		return 0
	}
	return (1 - k) * (1 - k) * (1 - k)
}

// drawStill also paints the cover wall's edge masks, using window-relative
// coordinates so their colors and their pixel dither meet without a seam.
func (bd *backdropCache) drawStill(p *ui.Painter, win, r ui.Rect, previous, alpha float32) {
	if bd.still == nil || win.W <= 0 || win.H <= 0 {
		return
	}
	// Shader.Draw copies the values into its draw command, so reusing this
	// storage for several wall strips cannot change an earlier command.
	if bd.stillValues == nil {
		bd.stillValues = stillBackdropShader().NewValues()
	}
	u, v := stillUniforms(), bd.stillValues
	var images [4]*ui.Bitmap
	images[0] = bd.still.ramp
	var cover, oldCover float32
	if art := bd.still.art; art != nil && art.backdrop != nil {
		images[1], cover = art.backdrop, 1
	}
	if previous > 0 && bd.previousStill != nil {
		images[2] = bd.previousStill.ramp
		if art := bd.previousStill.art; art != nil && art.backdrop != nil {
			images[3], oldCover = art.backdrop, 1
		}
	}
	v.Vec2(u.origin, r.X-win.X, r.Y-win.Y).Vec2(u.size, win.W, win.H).
		Float(u.previous, previous).Vec2(u.hasCover, cover, oldCover).Float(u.alpha, alpha)
	stillBackdropShader().Draw(p, r, shader.Options{Values: v, Images: images})
}

// backdropRamp interpolates in Oklab and converts to sRGB once per tint,
// rather than doing these conversions for every physical screen pixel.
// Packing happens at 16-bit precision; the shader delays the final 8-bit
// rounding until after its composition and physical-pixel dither.
func backdropRamp(tint ui.Color) *image.RGBA {
	top, foot := backdropColors(tint)
	a, b := backdropLab(top), backdropLab(foot)
	img := image.NewRGBA(image.Rect(0, 0, backdropRampSize, 2))
	srgb := func(v float64) float64 {
		if v <= 0.0031308 {
			return 12.92 * v
		}
		return 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	for x := range backdropRampSize {
		t := float64(x) / (backdropRampSize - 1)
		l0 := float64(a[0])*(1-t) + float64(b[0])*t
		a0 := float64(a[1])*(1-t) + float64(b[1])*t
		b0 := float64(a[2])*(1-t) + float64(b[2])*t
		l := l0 + 0.3963377774*a0 + 0.2158037573*b0
		m := l0 - 0.1055613458*a0 - 0.0638541728*b0
		s := l0 - 0.0894841775*a0 - 1.2914855480*b0
		l, m, s = l*l*l, m*m*m, s*s*s
		colors := [3]float64{
			srgb(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
			srgb(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
			srgb(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
		}
		for i, c := range colors {
			v := uint16(math.Round(max(0, min(1, c)) * 65535))
			img.Pix[x*4+i] = byte(v >> 8)
			img.Pix[img.Stride+x*4+i] = byte(v)
		}
		// Both rows are data, not translucent colors: opaque alpha avoids
		// premultiplication changing the stored high and low bytes.
		img.Pix[x*4+3], img.Pix[img.Stride+x*4+3] = 255, 255
	}
	return img
}
