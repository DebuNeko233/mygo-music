package app

import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

const (
	lyricSpectrumFeather = 10 // DIPs across the upper edge
	lyricSpectrumTip     = 18 // DIPs below the upper edge
	lyricSpectrumSilent  = 0.006
	lyricSpectrumSamples = 384
)

// One texture row carries 16-bit heights in red and green and the
// brightness boost in blue. The GPU filters before unpacking the height.
const lyricSpectrumSource = `package main

var Height float
var Feather float
var TipDepth float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	size := rectSize()
	x := 0.5 + clamp(pos.x/size.x, 0.0, 1.0)*(image0Size().x-1.0)
	sample := image0Sample(vec2(x, 0.5)).rgb
	edge := (sample.r*256.0 + sample.g)/257.0*Height
	distance := edge - (size.y-pos.y)
	alpha := Tint.a*smoothstep(-Feather/2.0, Feather/2.0, distance)
	tip := 1.0 - smoothstep(0.0, TipDepth, distance)
	// Limit all three channels by the same factor to keep the hue.
	gain := min(1.0 + sample.b*tip, 1.0 / max(max(Tint.r, Tint.g), max(Tint.b, 1.0/255.0)))
	return vec4(Tint.rgb*(gain*alpha), alpha)
}
`

var lyricSpectrumShader = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(lyricSpectrumSource) })

var lyricSpectrumUniforms = sync.OnceValue(func() [4]shader.Uniform {
	s := lyricSpectrumShader()
	return [4]shader.Uniform{s.Uniform("Height"), s.Uniform("Feather"), s.Uniform("TipDepth"), s.Uniform("Tint")}
})

type lyricSpectrum struct {
	levels, contour      [player.DetailBands]float32
	at                   time.Time
	peak                 float32
	tintFrom, tintTarget ui.Color
	tintSince            time.Time
	mask                 *ui.Bitmap
	pixels               [2]*image.RGBA
	write                int
	values               *shader.Values
	paintTint            ui.Color
}

func (a *app) lyricSpectrumLayer(c *ui.Context, tr *netease.Track, tint ui.Color, height float32) {
	st := &a.npSpectrum
	if !a.nowPlaying || (a.npPane != paneLyrics && a.npPane != paneStage) || tr.ProgramID != 0 ||
		!a.settings.LyricSpectrum || c.Preferences().ReduceMotion || c.WindowHidden() {
		*st = lyricSpectrum{}
		return
	}
	tint = tint.Mix(ui.RGB(117, 124, 192), 0.35).Mix(white, 0.13).Alpha(0.31)
	ui.Box(c.Key("lyric-spectrum")).Absolute().Left(0).Right(0).Bottom(0).
		Height(min(height*0.25, 220)).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		pl := a.pl
		now := p.Now()
		changed := now.After(st.at)
		playing := pl.Playing() && pl.Ready()
		if changed && (playing || st.peak > lyricSpectrumSilent) {
			// Pause, seek, and track loading keep the same envelope.
			// Missing samples let it settle until matching audio arrives.
			sample, live := pl.DetailedLevels()
			st.step(now, sample, live)
		}
		color := st.blendTint(now, tint)
		if st.peak > lyricSpectrumSilent {
			fade := clamp01((st.peak - lyricSpectrumSilent) / (0.10 - lyricSpectrumSilent))
			st.draw(p, r, color.Alpha(fade*fade*(3-2*fade)), changed)
		}
		if playing || st.peak > lyricSpectrumSilent {
			p.AnimationFrame()
		}
	})
}

// blendTint follows the backdrop's crossfade. A new cover can arrive
// during a fade, so start from the color already on screen.
func (st *lyricSpectrum) blendTint(now time.Time, target ui.Color) ui.Color {
	color := st.tintTarget
	if st.tintSince.IsZero() {
		color = target
	} else if t := float32(now.Sub(st.tintSince)) / float32(backdropFade); t < 1 {
		color = st.tintFrom.Mix(st.tintTarget, easeOutCubic(max(0, t)))
	}
	if target != st.tintTarget {
		st.tintFrom, st.tintTarget, st.tintSince = color, target, now
	}
	return color
}

func (st *lyricSpectrum) step(now time.Time, sample [player.DetailBands]float32, live bool) {
	// Keep the attack and release in seconds as the display changes rate.
	dt := 1.0 / 120
	if !st.at.IsZero() {
		dt = min(0.2, now.Sub(st.at).Seconds())
	}
	st.at = now
	release := 4.5
	if !live {
		release = 3.8
	}
	rise, fall := float32(1-math.Exp(-18*dt)), float32(1-math.Exp(-release*dt))
	for i, value := range st.levels {
		speed := rise
		if sample[i] < value {
			speed = fall
		}
		st.levels[i] += (sample[i] - value) * speed
	}
	st.smoothContour()
}

// Two passes over neighboring bands round isolated spikes. Keep a little
// of the measured contour so their position is still visible.
func (st *lyricSpectrum) smoothContour() {
	var scratch [player.DetailBands]float32
	spectrumAverage(&scratch, &st.levels)
	spectrumAverage(&st.contour, &scratch)
	st.peak = 0
	for i, v := range st.levels {
		st.contour[i] += 0.18 * (v - st.contour[i])
		st.peak = max(st.peak, st.contour[i])
	}
}

func spectrumAverage(dst, src *[player.DetailBands]float32) {
	last := len(src) - 1
	for i := range src {
		dst[i] = (src[max(0, i-1)] + src[i] + src[min(last, i+1)]) / 3
	}
}

func (st *lyricSpectrum) draw(p *ui.Painter, r ui.Rect, tint ui.Color, changed bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	// Software keeps a small raster mask instead of interpreting the
	// shader per pixel. Both paths reuse the bitmap and its two buffers.
	gpu := p.Accelerated()
	w, h := lyricSpectrumSamples, 1
	if !gpu {
		w = min(480, int(math.Ceil(float64(r.W)/2)))
		h = int(math.Ceil(float64(r.H) / 2))
		changed = changed || st.paintTint != tint
		st.paintTint = tint
	}
	bounds := image.Rect(0, 0, w, h)
	if st.pixels[0] == nil || st.pixels[0].Bounds() != bounds {
		st.pixels = [2]*image.RGBA{image.NewRGBA(bounds), image.NewRGBA(bounds)}
		changed = true
	}
	if changed {
		pixels := st.pixels[st.write]
		if gpu {
			paintLyricSpectrumHeights(pixels, st.contour[:], st.peak)
		} else {
			scale := float32(h) / r.H
			paintLyricSpectrumFill(pixels, st.contour[:], st.peak, lyricSpectrumFeather*scale, lyricSpectrumTip*scale, tint)
		}
		if st.mask == nil {
			st.mask = ui.NewBitmap(pixels)
		} else {
			st.mask.Update(pixels)
		}
		st.write = 1 - st.write
	}
	if !gpu {
		p.ImageRegion(st.mask, ui.Rect{}, r, ui.ImageOptions{})
		return
	}
	if st.values == nil {
		st.values = lyricSpectrumShader().NewValues()
	}
	// Skip the part above even the highest possible peak and its feather.
	height := r.H
	r.H = min(height, height*0.96*st.peak+lyricSpectrumFeather/2)
	r.Y += height - r.H
	u := lyricSpectrumUniforms()
	st.values.Float(u[0], height).Float(u[1], lyricSpectrumFeather).Float(u[2], lyricSpectrumTip).Color(u[3], tint)
	lyricSpectrumShader().Draw(p, r, shader.Options{Values: st.values, Images: [4]*ui.Bitmap{st.mask}})
}

func paintLyricSpectrumHeights(dst *image.RGBA, bands []float32, peak float32) {
	w := dst.Bounds().Dx()
	for x := range w {
		level := lyricSpectrumLevel(bands, x, w)
		value := uint16((0.28*peak+0.68*level)*65535 + 0.5)
		i := x * 4
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(value>>8), uint8(value), uint8(lyricSpectrumBrightness(level)*255+0.5), 255
	}
}

// paintLyricSpectrumFill fills each column from the feathered curve to
// the bottom. The strongest bands brighten only near the upper edge.
func paintLyricSpectrumFill(dst *image.RGBA, bands []float32, peak, feather, tipDepth float32, tint ui.Color) {
	clear(dst.Pix)
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	maxGain := 255 / float32(max(1, tint.R, tint.G, tint.B))
	alpha := float32(tint.A) / 255
	r, g, b := float32(tint.R)*alpha, float32(tint.G)*alpha, float32(tint.B)*alpha
	rr, gg, bb := uint8(r+0.5), uint8(g+0.5), uint8(b+0.5)
	for x := range w {
		level := lyricSpectrumLevel(bands, x, w)
		edge := float32(h) * (1 - 0.28*peak - 0.68*level)
		boost := lyricSpectrumBrightness(level)
		top := edge - feather/2
		end := edge + feather/2
		if boost > 0 {
			end = max(end, edge+tipDepth)
		}
		solid := min(h, int(math.Ceil(float64(end))))
		for y := max(0, int(top)); y < solid; y++ {
			center := float32(y) + 0.5
			f := min(1, max(0, (center-top)/feather))
			f = f * f * (3 - 2*f)
			d := min(1, max(0, (center-edge)/tipDepth))
			gain := min(1+boost*(1-d*d*(3-2*d)), maxGain) * f
			i := y*dst.Stride + x*4
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r*gain+0.5), uint8(g*gain+0.5), uint8(b*gain+0.5), uint8(float32(tint.A)*f+0.5)
		}
		for y := solid; y < h; y++ {
			i := y*dst.Stride + x*4
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = rr, gg, bb, tint.A
		}
	}
}

func lyricSpectrumBrightness(level float32) float32 {
	t := min(1, max(0, (level-0.50)/0.35))
	return 0.85 * t * t * (3 - 2*t)
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
