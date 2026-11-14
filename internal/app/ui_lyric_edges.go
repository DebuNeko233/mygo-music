package app

import (
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

const lyricEdgeMask = `
func edge(y, height float) float {
	band := min(96.0, height*0.20)
	return 1.0-smoothstep(0.0, band, min(y, height-y))
}

func withinRow(y float, row vec3) bool {
	return row.z > 0 && y >= row.x && y <= row.y
}
`

const lyricBrowseFadeSource = "package main\n" + lyricEdgeMask + `
func ColorEffect(pos vec2, color vec4) vec4 {
	return color*(1.0-edge(pos.y, rectSize().y))
}
`

var lyricBrowseFade = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(lyricBrowseFadeSource) })

// The same 13-tap Gaussian runs horizontally and vertically. Adjacent
// weights are grouped into seven bilinear sample positions per pass.
// Both edges reach the same blur. The current row is above center, so
// the longer run below it softens more slowly than the run above it.
const lyricGaussianKernel = lyricEdgeMask + `
// Active holds the top and bottom of the clear row in texture pixels.
var Active vec3
var Height float
var Top float
var Scale float
var ContentSize vec2
var Hover vec3
var Credits vec3

func blurSigma(pos vec2) float {
	if Active.z == 0 {
		return mix(1.5, 3.5, edge((pos.y-Top)/Scale, Height))
	}
	y := pos.y
	bottom := Top+Height*Scale
	if y < Active.x {
		return 3.5*clamp((Active.x-y)/max(1.0, Active.x-Top), 0.0, 1.0)
	}
	return 3.5*clamp((y-Active.y)/max(1.0, bottom-Active.y), 0.0, 1.0)
}

func sampleContent(pos vec2) vec4 {
	// The intermediate canvas can be larger than the painted viewport.
	// Clamp to the actual content in both passes, independent of allocation.
	return image0Sample(clamp(pos, vec2(0.5), ContentSize-vec2(0.5)))
}

func gaussian(pos, stride vec2) vec4 {
	a := stride*1.452314
	b := stride*3.390210
	c := stride*5.331473
	return 0.14532848*sampleContent(pos) +
		0.24895539*(sampleContent(pos+a)+sampleContent(pos-a)) +
		0.13424386*(sampleContent(pos+b)+sampleContent(pos-b)) +
		0.04413651*(sampleContent(pos+c)+sampleContent(pos-c))
}
`

const lyricGaussianHorizontal = "package main\n" + lyricGaussianKernel + `
func Fragment(dst vec4, pos vec2) vec4 {
	stride := blurSigma(pos)*Scale/2.8
	// Skip the interior of each clear band, leaving more than the
	// vertical kernel's maximum reach for samples from its neighbors.
	halo := vec2(8.0*Scale, -8.0*Scale)
	if withinRow(pos.y, vec3(Active.xy+halo, Active.z)) ||
		withinRow(pos.y, vec3(Hover.xy+halo, Hover.z)) ||
		withinRow(pos.y, vec3(Credits.xy+halo, Credits.z)) {
		return vec4(0)
	}
	return gaussian(pos, vec2(stride, 0.0))
}
`

const lyricGaussianVertical = "package main\n" + lyricGaussianKernel + `
func LayerEffect(pos vec2) vec4 {
	y := pos.y*Scale+Top
	if withinRow(y, Active) {
		return layerAt(pos)
	}
	fade := 1.0-edge(pos.y, Height)
	if withinRow(y, Hover) || withinRow(y, Credits) {
		// Keep the strokes sharp while they pass through the edge fade.
		return layerAt(pos)*fade
	}
	pixel := vec2(pos.x*Scale+Left, y)
	stride := blurSigma(pixel)*Scale/2.8
	return gaussian(pixel, vec2(0.0, stride))*fade
}

var Left float
`

var lyricGaussian = sync.OnceValue(func() [2]*shader.Shader {
	return [2]*shader.Shader{
		shader.MustCompile(lyricGaussianHorizontal),
		shader.MustCompile(lyricGaussianVertical),
	}
})

type lyricEdgeFade struct {
	clearBand  func(scale, insetY float32) [3]float32
	hoverBand  func(scale, insetY float32) [3]float32
	creditBand func(scale, insetY float32) [3]float32
	pass       **ui.Canvas
	browsing   bool
}

func addLyricCreditRow(rows *[2]ui.Element, row ui.Element) {
	if !rows[0].Valid() {
		rows[0] = row
	}
	rows[1] = row
}

func (a *app) lyricEdges(scroll, active, hovered, intro, first ui.Element, introOpacity float32, browsing bool, credits [2]ui.Element) {
	var incoming ui.Element
	var blend float32
	if intro.Valid() {
		// Move the clear band from the dots to the first line as they fade.
		active, incoming, blend = intro, first, 1-introOpacity
	}
	fx := lyricEdgeFade{clearBand: lyricClearBand(scroll, active, incoming, blend), pass: &a.lyricBlurPass, browsing: browsing}
	if !browsing && hovered.Valid() {
		fx.hoverBand = lyricClearBand(scroll, hovered, ui.Element{}, 0)
	}
	if !browsing && credits[0].Valid() {
		top := lyricClearBand(scroll, credits[0], ui.Element{}, 0)
		bottom := lyricClearBand(scroll, credits[1], ui.Element{}, 0)
		fx.creditBand = func(scale, insetY float32) [3]float32 {
			band := top(scale, insetY)
			if end := bottom(scale, insetY); end[2] != 0 {
				band[1] = end[1]
			}
			return band
		}
	}
	scroll.Effect(fx)
}

func (lyricEdgeFade) EffectMargin() ui.Point {
	// The viewport clips drawing at its bounds. Pixels outside it are
	// transparent, so the shader only needs a viewport-sized layer.
	return ui.Point{}
}

// Bind the lookup to this viewport's paint pass so it follows the live pose
// on repaint-only scroll frames.
func lyricClearBand(viewport, active, incoming ui.Element, blend float32) func(float32, float32) [3]float32 {
	return func(scale, insetY float32) [3]float32 {
		row := func(e ui.Element) [3]float32 {
			bounds := e.LayoutBounds()
			if bounds.H <= 0 {
				return [3]float32{}
			}
			x, y := e.LocalToWindow(0, 0)
			_, top, ok := viewport.WindowToLocal(x, y)
			x, y = e.LocalToWindow(0, bounds.H)
			_, bottom, okBottom := viewport.WindowToLocal(x, y)
			if ok && okBottom {
				return [3]float32{top*scale + insetY, bottom*scale + insetY, 1}
			}
			return [3]float32{}
		}
		band := row(active)
		if blend > 0 {
			coming := row(incoming)
			if band[2] == 0 {
				band = coming
			} else if coming[2] != 0 {
				for i := range 2 {
					band[i] = lerp(band[i], coming[i], blend)
				}
			}
		}
		return band
	}
}

func (fx lyricEdgeFade) PaintEffect(p *ui.Painter, box ui.Rect, bitmap *ui.Bitmap, inset ui.Point) {
	var hover, credits [3]float32
	if fx.browsing {
		// The manual fade works in DIPs and needs no Gaussian canvas.
		lyricBrowseFade().Effect(shader.Options{}).PaintEffect(p, box, bitmap, inset)
		return
	}
	scale := p.Scale()
	var active [3]float32
	if fx.clearBand != nil {
		active = fx.clearBand(scale, inset.Y)
	}
	w, h := bitmap.Size()
	pass := *fx.pass
	// Reuse the intermediate texture during live resize.
	pw, ph := (w+63)/64*64, (h+63)/64*64
	cw, ch := 0, 0
	if pass != nil {
		cw, ch = pass.Size()
	}
	if cw < w || ch < h || cw-w >= 128 || ch-h >= 128 {
		pass = ui.NewCanvas(pw, ph)
		*fx.pass = pass
	}
	kernel := lyricGaussian()
	if fx.hoverBand != nil {
		hover = fx.hoverBand(scale, inset.Y)
	}
	if fx.creditBand != nil {
		credits = fx.creditBand(scale, inset.Y)
	}
	uniforms := map[string]any{
		"Active": active, "Height": box.H, "Top": inset.Y, "Scale": scale,
		"Hover": hover, "Credits": credits,
		"ContentSize": [2]float32{float32(w), float32(h)},
	}
	pass.Draw(p, func(cp *ui.Painter) {
		kernel[0].Draw(cp, ui.Rect{W: float32(w), H: float32(h)}, shader.Options{
			Uniforms: uniforms, Images: [4]*ui.Bitmap{bitmap},
		})
	})
	uniforms["Left"] = inset.X
	kernel[1].Effect(shader.Options{Uniforms: uniforms, Images: [4]*ui.Bitmap{pass.Bitmap()}}).
		PaintEffect(p, box, bitmap, inset)
}
