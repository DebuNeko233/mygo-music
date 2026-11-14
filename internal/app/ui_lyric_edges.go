package app

import (
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The same 13-tap Gaussian runs horizontally and vertically. Adjacent
// weights are grouped into seven bilinear sample positions per pass.
// Both edges reach the same blur. The current row is above center, so
// the longer run below it softens more slowly than the run above it.
const lyricGaussianKernel = `
// Active holds the top and bottom of the clear row in texture pixels.
var Active vec3
var Height float
var Top float
var Scale float
var ContentSize vec2

func edge(pos vec2) float {
	y := (pos.y-Top)/Scale
	band := min(96.0, Height*0.20)
	return 1.0-smoothstep(0.0, band, min(y, Height-y))
}

func blurSigma(pos vec2) float {
	if Active.z == 0 {
		return mix(1.5, 3.5, edge(pos))
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
	// Skip the center of the clear row, leaving a halo for the vertical pass.
	if Active.z > 0 && pos.y >= Active.x+8.0*Scale && pos.y <= Active.y-8.0*Scale {
		return vec4(0)
	}
	return gaussian(pos, vec2(stride, 0.0))
}
`

const lyricGaussianVertical = "package main\n" + lyricGaussianKernel + `
func LayerEffect(pos vec2) vec4 {
	y := pos.y*Scale+Top
	if Active.z > 0 && y >= Active.x && y <= Active.y {
		return layerAt(pos)
	}
	pixel := vec2(pos.x*Scale+Left, y)
	fade := edge(pixel)
	stride := blurSigma(pixel)*Scale/2.8
	return gaussian(pixel, vec2(0.0, stride))*(1.0-fade)
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
	viewport, active ui.Element
	incoming         ui.Element
	blend            float32
	pass             **ui.Canvas
}

func (a *app) lyricEdges(scroll, active, intro, first ui.Element, introOpacity float32) {
	fx := lyricEdgeFade{viewport: scroll, active: active, pass: &a.lyricBlurPass}
	if intro.Valid() {
		// Move the clear band from the dots to the first line as they fade.
		fx.active, fx.incoming, fx.blend = intro, first, 1-introOpacity
	}
	scroll.Effect(fx)
}

func (lyricEdgeFade) EffectMargin() ui.Point {
	// The viewport clips drawing at its bounds. Pixels outside it are
	// transparent, so the shader only needs a viewport-sized layer.
	return ui.Point{}
}

func (fx lyricEdgeFade) PaintEffect(p *ui.Painter, box ui.Rect, bitmap *ui.Bitmap, inset ui.Point) {
	scale := p.Scale()
	row := func(e ui.Element) [3]float32 {
		bounds := e.LayoutBounds()
		if bounds.H <= 0 {
			return [3]float32{}
		}
		x, y := e.LocalToWindow(0, 0)
		_, top, ok := fx.viewport.WindowToLocal(x, y)
		x, y = e.LocalToWindow(0, bounds.H)
		_, bottom, okBottom := fx.viewport.WindowToLocal(x, y)
		if ok && okBottom {
			return [3]float32{top*scale + inset.Y, bottom*scale + inset.Y, 1}
		}
		return [3]float32{}
	}
	active := row(fx.active)
	if fx.blend > 0 {
		coming := row(fx.incoming)
		if active[2] == 0 {
			active = coming
		} else if coming[2] != 0 {
			for i := range 2 {
				active[i] += (coming[i] - active[i]) * fx.blend
			}
		}
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
	uniforms := map[string]any{
		"Active": active, "Height": box.H, "Top": inset.Y, "Scale": scale,
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
