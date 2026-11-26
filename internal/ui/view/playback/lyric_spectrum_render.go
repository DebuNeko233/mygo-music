/*
将频谱轮廓编码为纹理，绘制频谱填充、亮度和外沿辉光。
*/

package playback

import (
	"image"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

const (
	lyricSpectrumFeather       = 10 // DIPs across the upper edge
	lyricSpectrumTip           = 20 // DIPs below the upper edge
	lyricSpectrumBody          = 56 // DIPs for the fill to darken into the base
	lyricSpectrumGlowX         = 64 // DIPs to either side of a bright band
	lyricSpectrumGlowY         = 44 // DIPs above and below the peak
	lyricSpectrumShade         = 0.20
	lyricSpectrumSamples       = 384
	lyricSpectrumRaster        = 480
	lyricSpectrumGlowThreshold = 0.52
)

// The texture rows describe the curve and the wider glow around it.
// Both store height in red and green, and light intensity in blue.
const lyricSpectrumSource = `package main

var Height float
var Feather float
var TipDepth float
var BodyDepth float
var BodyShade float
var GlowRadius float
var Tint vec4
var Light vec4

func height(sample vec3) float {
	return (sample.r*256.0 + sample.g)/257.0*Height
}

func Fragment(dst vec4, pos vec2) vec4 {
	size := rectSize()
	x := 0.5 + clamp(pos.x/size.x, 0.0, 1.0)*(image0Size().x-1.0)
	sample := image0Sample(vec2(x, 0.5)).rgb
	y := size.y-pos.y
	distance := height(sample) - y
	coverage := smoothstep(-Feather/2.0, Feather/2.0, distance)
	shade := 1.0 - BodyShade*smoothstep(0.0, BodyDepth, distance)
	light := 0.0
	if Light.a > 0.0 {
		halo := image0Sample(vec2(x, 1.5)).rgb
		tip := sample.b*(1.0 - smoothstep(0.0, TipDepth, abs(distance)))
		glow := halo.b*(1.0 - smoothstep(0.0, GlowRadius, abs(height(halo)-y)))
		light = Light.a*(0.68*tip + 0.30*glow)
	}
	body := Tint.a*coverage*(1.0-light)
	return vec4(Tint.rgb*(shade*body) + Light.rgb*light, body+light)
}
`

var LyricSpectrumShader = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(lyricSpectrumSource) })

type spectrumUniforms struct {
	height, feather, tipDepth, bodyDepth, bodyShade, glowRadius, tint, light shader.Uniform
}

var lyricSpectrumUniforms = sync.OnceValue(func() spectrumUniforms {
	s := LyricSpectrumShader()
	return spectrumUniforms{
		height: s.Uniform("Height"), feather: s.Uniform("Feather"), tipDepth: s.Uniform("TipDepth"),
		bodyDepth: s.Uniform("BodyDepth"), bodyShade: s.Uniform("BodyShade"), glowRadius: s.Uniform("GlowRadius"),
		tint: s.Uniform("Tint"), light: s.Uniform("Light"),
	}
})

type spectrumRenderer struct {
	Mask                    *ui.Bitmap
	pixels                  [2]*image.RGBA
	write                   int
	values                  *shader.Values
	paintTint, paintLight   ui.Color
	paintWidth, paintHeight float32
}

func (st *spectrumRenderer) draw(p *ui.Painter, r ui.Rect, curveHeight float32, bands []float32, peak float32, tint, light ui.Color, changed bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	// Software keeps a small raster mask instead of interpreting the
	// shader per pixel. Both paths reuse the bitmap and its two buffers.
	gpu := p.Accelerated()
	if peak <= lyricSpectrumGlowThreshold {
		light.A = 0
	}
	w, h := lyricSpectrumSamples, 2
	changed = changed || st.paintWidth != r.W
	st.paintWidth = r.W
	if !gpu {
		w = min(lyricSpectrumRaster, int(math.Ceil(float64(r.W)/2)))
		h = int(math.Ceil(float64(r.H) / 2))
		changed = changed || st.paintTint != tint || st.paintLight != light || st.paintHeight != r.H
		st.paintTint, st.paintLight, st.paintHeight = tint, light, r.H
	}
	bounds := image.Rect(0, 0, w, h)
	if st.pixels[0] == nil || st.pixels[0].Bounds() != bounds {
		st.pixels = [2]*image.RGBA{image.NewRGBA(bounds), image.NewRGBA(bounds)}
		changed = true
	}
	if changed {
		pixels := st.pixels[st.write]
		var columns [lyricSpectrumRaster]SpectrumGlow
		glow := columns[:w]
		SpectrumGlowColumns(glow, bands, peak, r.W)
		if gpu {
			paintLyricSpectrumHeights(pixels, bands, peak, glow)
		} else {
			PaintLyricSpectrumFill(pixels, bands, peak, curveHeight, float32(h)/r.H, glow, tint, light)
		}
		if st.Mask == nil {
			st.Mask = ui.NewBitmap(pixels)
		} else {
			st.Mask.Update(pixels)
		}
		st.write = 1 - st.write
	}
	if !gpu {
		p.ImageRegion(st.Mask, ui.Rect{}, r, ui.ImageOptions{})
		return
	}
	// When there is no light, the draw only needs room for the feather.
	margin := float32(lyricSpectrumFeather / 2)
	if light.A > 0 {
		margin = lyricSpectrumGlowY
	}
	drawHeight := min(r.H, curveHeight*0.96*peak+margin)
	r.Y += r.H - drawHeight
	r.H = drawHeight
	u := lyricSpectrumUniforms()
	if st.values == nil {
		st.values = LyricSpectrumShader().NewValues().
			Float(u.feather, lyricSpectrumFeather).Float(u.tipDepth, lyricSpectrumTip).
			Float(u.bodyDepth, lyricSpectrumBody).Float(u.bodyShade, lyricSpectrumShade).Float(u.glowRadius, lyricSpectrumGlowY)
	}
	st.values.Float(u.height, curveHeight).Color(u.tint, tint).Color(u.light, light)
	LyricSpectrumShader().Draw(p, r, shader.Options{Values: st.values, Images: [4]*ui.Bitmap{st.Mask}})
}

type SpectrumGlow struct {
	height, strength, weight float32
}

// Spread the bright bands horizontally in texture space once per frame.
// Weight their heights together so overlapping halos stay continuous.
func SpectrumGlowColumns(dst []SpectrumGlow, bands []float32, peak, width float32) {
	if peak <= lyricSpectrumGlowThreshold {
		return
	}
	radius := max(1, float32(len(dst)-1)*lyricSpectrumGlowX/width)
	for i, level := range bands {
		brightness := lyricSpectrumBrightness(level)
		if brightness == 0 {
			continue
		}
		center := float32(i*(len(dst)-1)) / float32(len(bands)-1)
		height := spectrumHeight(level, peak)
		start, end := max(0, int(center-radius)), min(len(dst)-1, int(center+radius))
		for x := start; x <= end; x++ {
			d := (float32(x) - center) / radius
			strength := brightness * spectrumEase(1-d*d)
			weight := strength * strength
			g := &dst[x]
			g.height += height * weight
			g.weight += weight
			g.strength = max(g.strength, strength)
		}
	}
	for i := range dst {
		if g := &dst[i]; g.weight > 0 {
			g.height /= g.weight
		}
	}
}

func spectrumHeight(level, peak float32) float32 {
	return 0.28*peak + 0.68*level
}

func paintLyricSpectrumHeights(dst *image.RGBA, bands []float32, peak float32, glow []SpectrumGlow) {
	w := dst.Bounds().Dx()
	for x := range w {
		level := lyricSpectrumLevel(bands, x, w)
		putSpectrumTexel(dst.Pix[x*4:], spectrumHeight(level, peak), lyricSpectrumBrightness(level))
		putSpectrumTexel(dst.Pix[dst.Stride+x*4:], glow[x].height, glow[x].strength)
	}
}

func putSpectrumTexel(pix []byte, height, strength float32) {
	value := uint16(height*65535 + 0.5)
	pix[0], pix[1], pix[2], pix[3] = uint8(value>>8), uint8(value), uint8(strength*255+0.5), 255
}

// Keep the software render in sync with the shader for previews and
// machines that use the CPU renderer.
func PaintLyricSpectrumFill(dst *image.RGBA, bands []float32, peak, height, scale float32, glow []SpectrumGlow, tint, light ui.Color) {
	clear(dst.Pix)
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	feather, tipDepth, bodyDepth := lyricSpectrumFeather*scale, lyricSpectrumTip*scale, lyricSpectrumBody*scale
	radius := lyricSpectrumGlowY * scale
	bodyAlpha, lightAlpha := float32(tint.A)/255, float32(light.A)/255
	plate := bodyAlpha * (1 - lyricSpectrumShade)
	r, g, b := float32(tint.R)*plate, float32(tint.G)*plate, float32(tint.B)*plate
	rr, gg, bb := uint8(r+0.5), uint8(g+0.5), uint8(b+0.5)
	for x := range w {
		level := lyricSpectrumLevel(bands, x, w)
		edge := float32(h) - spectrumHeight(level, peak)*height*scale
		tipStrength, haloStrength := lyricSpectrumBrightness(level), glow[x].strength
		if lightAlpha == 0 {
			tipStrength, haloStrength = 0, 0
		}
		halo := float32(h) - glow[x].height*height*scale
		top := edge - feather/2
		end := edge + max(feather/2, tipDepth, bodyDepth)
		start := top
		if tipStrength > 0 {
			start = min(start, edge-tipDepth)
		}
		if haloStrength > 0 {
			start, end = min(start, halo-radius), max(end, halo+radius)
		}
		solid := min(h, int(math.Ceil(float64(end))))
		for y := max(0, int(start)); y < solid; y++ {
			center := float32(y) + 0.5
			distance := center - edge
			coverage := float32(1)
			if center < top+feather {
				coverage = spectrumEase((center - top) / feather)
			}
			shade := 1 - lyricSpectrumShade*spectrumEase(distance/bodyDepth)
			var tip, bloom float32
			if tipStrength > 0 {
				tip = tipStrength * (1 - spectrumEase(float32(math.Abs(float64(distance)))/tipDepth))
			}
			if haloStrength > 0 {
				bloom = haloStrength * (1 - spectrumEase(float32(math.Abs(float64(center-halo)))/radius))
			}
			lit := lightAlpha * (0.68*tip + 0.30*bloom)
			body := bodyAlpha * coverage * (1 - lit)
			fill := shade * body
			i := y*dst.Stride + x*4
			dst.Pix[i] = uint8(float32(tint.R)*fill + float32(light.R)*lit + 0.5)
			dst.Pix[i+1] = uint8(float32(tint.G)*fill + float32(light.G)*lit + 0.5)
			dst.Pix[i+2] = uint8(float32(tint.B)*fill + float32(light.B)*lit + 0.5)
			dst.Pix[i+3] = uint8((body+lit)*255 + 0.5)
		}
		for y := solid; y < h; y++ {
			i := y*dst.Stride + x*4
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = rr, gg, bb, tint.A
		}
	}
}

func lyricSpectrumBrightness(level float32) float32 {
	return spectrumEase((level - lyricSpectrumGlowThreshold) / 0.34)
}

func lyricSpectrumLevel(bands []float32, x, width int) float32 {
	// A clamped Catmull–Rom curve joins the bands without inventing
	// peaks between them.
	last := len(bands) - 1
	band := float32(x) * float32(last) / float32(max(1, width-1))
	i := min(int(band), last-1)
	t := band - float32(i)
	before, left := bands[max(0, i-1)], bands[i]
	right, after := bands[i+1], bands[min(last, i+2)]
	startSlope, endSlope := (right-before)/2, (after-left)/2
	value := left + t*(startSlope+t*(3*(right-left)-2*startSlope-endSlope+t*(2*(left-right)+startSlope+endSlope)))
	return min(max(left, right), max(min(left, right), value))
}
