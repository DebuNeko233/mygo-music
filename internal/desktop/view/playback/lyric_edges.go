package playback

import (
	"math"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"netease-music-mygo/internal/lyrics"
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
var Gutter float

func ColorEffect(pos vec2, color vec4) vec4 {
	if Gutter > 0 && pos.x >= rectSize().x-Gutter {
		return color
	}
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
var TextRight float
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
	return image0Sample(clamp(pos, vec2(0.5), vec2(TextRight, ContentSize.y)-vec2(0.5)))
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
	// The scrollbar stays in the source layer and never enters the blur pass.
	if pos.x >= TextRight {
		return vec4(0)
	}
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
	if Gutter > 0 && pos.x >= rectSize().x-Gutter {
		return layerAt(pos)
	}
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
var Gutter float
`

var lyricGaussian = sync.OnceValue(func() [2]*shader.Shader {
	return [2]*shader.Shader{
		shader.MustCompile(lyricGaussianHorizontal),
		shader.MustCompile(lyricGaussianVertical),
	}
})

type LyricEdgeFade struct {
	ClearBand  func(scale, insetY float32) [3]float32
	hoverBand  func(scale, insetY float32) [3]float32
	creditBand func(scale, insetY float32) [3]float32
	Pass       **ui.Canvas
	Browsing   bool
	Gutter     float32
}

// Both layouts collect the same rows for following and for the clear bands.
func (st LinesStyle) collectLyricRow(row ui.Element, hovered bool, i, current int, kind lyrics.LineKind,
	active, hover, firstSung *ui.Element, credits *[2]ui.Element,
) {
	if i == current {
		*active = row
	}
	if !st.Immersive {
		return
	}
	if i == st.introLine {
		*firstSung = row
	}
	openingCredit := i < st.introLine && kind == lyrics.Credit
	if openingCredit && st.hideOpeningCredits {
		row.Invisible()
		return
	}
	if st.browsing {
		return
	}
	if hovered {
		*hover = row
	}
	if current < 0 && openingCredit {
		if !credits[0].Valid() {
			credits[0] = row
		}
		credits[1] = row
	}
}

func (a *View) LyricEdges(scroll, active, hovered, intro, first ui.Element, introRemaining func() float32, browsing bool, credits [2]ui.Element, gutter float32) {
	var incoming ui.Element
	if intro.Valid() {
		// Move the clear band from the dots to the first line as they fade.
		active, incoming = intro, first
	}
	fx := LyricEdgeFade{ClearBand: LyricClearBand(scroll, active, incoming, introRemaining), Pass: &a.LyricBlurPass, Browsing: browsing, Gutter: gutter}
	if !browsing && hovered.Valid() {
		fx.hoverBand = LyricClearBand(scroll, hovered, ui.Element{}, nil)
	}
	if !browsing && credits[0].Valid() {
		top := LyricClearBand(scroll, credits[0], ui.Element{}, nil)
		bottom := LyricClearBand(scroll, credits[1], ui.Element{}, nil)
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

func (LyricEdgeFade) EffectMargin() ui.Point {
	// The viewport clips drawing at its bounds. Pixels outside it are
	// transparent, so the shader only needs a viewport-sized layer.
	return ui.Point{}
}

// Bind the lookup to this viewport's paint pass so it follows the live pose
// on repaint-only scroll frames.
func LyricClearBand(viewport, active, incoming ui.Element, introRemaining func() float32) func(float32, float32) [3]float32 {
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
		if introRemaining != nil {
			remaining := introRemaining()
			band[1] = lerp(band[0], band[1], remaining)
			blend := 1 - remaining
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

func (fx LyricEdgeFade) PaintEffect(p *ui.Painter, box ui.Rect, bitmap *ui.Bitmap, inset ui.Point) {
	var hover, credits [3]float32
	if fx.Browsing {
		// The manual fade works in DIPs and needs no Gaussian canvas.
		lyricBrowseFade().Effect(shader.Options{Uniforms: map[string]any{"Gutter": fx.Gutter}}).PaintEffect(p, box, bitmap, inset)
		return
	}
	scale := p.Scale()
	var active [3]float32
	if fx.ClearBand != nil {
		active = fx.ClearBand(scale, inset.Y)
	}
	w, h := bitmap.Size()
	textRight := float32(w)
	if fx.Gutter > 0 {
		textRight = max(1, min(textRight, float32(math.Floor(float64((box.W-fx.Gutter)*scale+inset.X)))))
	}
	pass := *fx.Pass
	// Reuse the intermediate texture during live resize.
	pw, ph := (w+63)/64*64, (h+63)/64*64
	cw, ch := 0, 0
	if pass != nil {
		cw, ch = pass.Size()
	}
	if cw < w || ch < h || cw-w >= 128 || ch-h >= 128 {
		pass = ui.NewCanvas(pw, ph)
		*fx.Pass = pass
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
		"ContentSize": [2]float32{float32(w), float32(h)}, "TextRight": textRight,
	}
	pass.DrawCached(p, func(cp *ui.Painter) {
		kernel[0].Draw(cp, ui.Rect{W: float32(w), H: float32(h)}, shader.Options{
			Uniforms: uniforms, Images: [4]*ui.Bitmap{bitmap},
		})
	})
	uniforms["Left"] = inset.X
	uniforms["Gutter"] = fx.Gutter
	kernel[1].Effect(shader.Options{Uniforms: uniforms, Images: [4]*ui.Bitmap{pass.Bitmap()}}).
		PaintEffect(p, box, bitmap, inset)
}
