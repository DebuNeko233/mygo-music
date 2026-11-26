//go:build darwin && !ios

/*
通过 macOS 原生图层更新歌曲行电平，避免为电平动画重绘整个窗口。
*/

package playback

import (
	"math"
	"structs"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	_ "github.com/egoist/mygo/native" // opt in to ui.NativeView hosting

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/components"
)

// Native row bars update only their AppKit layers rather than repainting
// the music window. All state belongs to the main thread; each app creates
// its view once, outside UI construction.
type nativeBarMeter struct {
	a             *View
	view, link    objc.ID
	layers        [4]objc.ID
	frames        [4]ui.Rect
	origin        ui.Rect
	scale         float32
	tint, bg      ui.Color
	paintTint     ui.Color
	paintBG       ui.Color
	ticks, writes uint64
	hidden        uint64
	paused        bool
	suspended     bool
	off           []func()
}

type barNativeRect struct {
	_          structs.HostLayout
	X, Y, W, H float64
}

type barNativePoint struct {
	_    structs.HostLayout
	X, Y float64
}

type barRateRange struct{ Minimum, Maximum, Preferred float32 }

var (
	nativeBarsOnce sync.Once
	nativeBarClass objc.Class
	nativeMeters   = map[objc.ID]*nativeBarMeter{}
	nativeApps     = map[*View]*nativeBarMeter{}
	barSelectors   = map[string]objc.SEL{}
	barMsgSend     uintptr
	barPoolPush    uintptr
	barPoolPop     uintptr
	barSetRect     func(objc.ID, objc.SEL, barNativeRect)
	barSetFloat    func(objc.ID, objc.SEL, float64)
	barSetRange    func(objc.ID, objc.SEL, barRateRange)
	barColor       func(objc.ID, objc.SEL, float64, float64, float64, float64) objc.ID
)

func init() {
	setupNativeRowBars = func(a *View) {
		if nativeApps[a] != nil {
			return
		}
		loadNativeBars()
		// NSView display links require macOS 14; older supported systems use
		// the ordinary painter rather than receiving an unknown selector.
		if barSend(objc.ID(objc.GetClass("NSView")), "instancesRespondToSelector:", uintptr(barSelectors["displayLinkWithTarget:selector:"])) == 0 {
			return
		}
		m := &nativeBarMeter{a: a}
		m.view = barSend(objc.ID(nativeBarClass), "new")
		nativeMeters[m.view], nativeApps[a] = m, m
		barSend(m.view, "setWantsLayer:", 1)
		root := barSend(m.view, "layer")
		barSend(root, "setMasksToBounds:", 1)
		for i := range m.layers {
			m.layers[i] = barSend(objc.ID(objc.GetClass("CALayer")), "new")
			barSend(root, "addSublayer:", uintptr(m.layers[i]))
			curve := objc.ID(objc.GetClass("NSString")).Send(barSelectors["stringWithUTF8String:"], "continuous")
			barSend(m.layers[i], "setCornerCurve:", uintptr(curve))
		}
		m.off = []func(){
			a.Window().OnClosed(m.close), a.Window().OnHide(func() { m.pause(true) }),
			a.Window().OnMinimize(func() { m.pause(true) }),
			a.Window().OnShow(func() { m.pause(false) }), a.Window().OnRestore(func() { m.pause(false) }),
		}
		a.ListenNative(m.close)
	}
	nativeRowBars = func(a *View, c *ui.Context, background ui.Color) bool {
		m := nativeApps[a]
		// A view's opaque background cannot reproduce a user picture or a
		// wide-gamut accent through a transparent hole; retain the normal
		// renderer for these cases until a matching native path exists.
		if m == nil || len(a.Config.BackdropHome) != 0 || a.Palette.Accent != a.Palette.Accent.SRGB() || background != background.SRGB() {
			return false
		}
		m.tint, m.bg = a.Palette.Accent, background
		m.suspended = c.WindowHidden()
		ui.NativeView(c, m).Size(16, 14).TabStop(false).PassThrough().AccessHidden(true).
			Children(func() { ui.Box(c).Absolute().Fill().AccessHidden(true) }).
			Draw(func(p *ui.Painter, r ui.Rect) {
				m.origin, m.scale = r, p.Scale()
				m.tick()
				if a.Player().Playing() && m.paused {
					m.pause(false)
				}
			})
		return true
	}
}

func loadNativeBars() {
	nativeBarsOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		barMsgSend, err = purego.Dlsym(lib, "objc_msgSend")
		if err != nil {
			panic(err)
		}
		barPoolPush, _ = purego.Dlsym(lib, "objc_autoreleasePoolPush")
		barPoolPop, _ = purego.Dlsym(lib, "objc_autoreleasePoolPop")
		for _, s := range []string{"new", "release", "setWantsLayer:", "layer", "setMasksToBounds:", "addSublayer:", "window", "displayLinkWithTarget:selector:", "meterTick:", "retain", "setPaused:", "addToRunLoop:forMode:", "mainRunLoop", "stringWithUTF8String:", "setPreferredFrameRateRange:", "isHiddenOrHasHiddenAncestor", "isVisible", "isMiniaturized", "begin", "setDisableActions:", "setBackgroundColor:", "CGColor", "colorWithSRGBRed:green:blue:alpha:", "setFrame:", "setCornerRadius:", "commit", "invalidate", "removeFromSuperlayer", "setCornerCurve:", "instancesRespondToSelector:", "screen", "maximumFramesPerSecond"} {
			barSelectors[s] = objc.RegisterName(s)
		}
		purego.RegisterFunc(&barSetRect, barMsgSend)
		purego.RegisterFunc(&barSetFloat, barMsgSend)
		purego.RegisterFunc(&barSetRange, barMsgSend)
		purego.RegisterFunc(&barColor, barMsgSend)
		nativeBarClass, err = objc.RegisterClass("MyGoMusicRowMeter", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("hitTest:"), Fn: func(self objc.ID, _ objc.SEL, point barNativePoint) objc.ID { return 0 }},
			{Cmd: objc.RegisterName("viewDidMoveToWindow"), Fn: func(self objc.ID, cmd objc.SEL) {
				self.SendSuper(cmd)
				if m := nativeMeters[self]; m != nil {
					m.attach()
				}
			}},
			{Cmd: objc.RegisterName("viewDidHide"), Fn: func(self objc.ID, cmd objc.SEL) {
				self.SendSuper(cmd)
				if m := nativeMeters[self]; m != nil {
					m.pause(true)
				}
			}},
			{Cmd: objc.RegisterName("viewDidUnhide"), Fn: func(self objc.ID, cmd objc.SEL) {
				self.SendSuper(cmd)
				if m := nativeMeters[self]; m != nil {
					m.pause(false)
				}
			}},
			{Cmd: barSelectors["meterTick:"], Fn: func(self objc.ID, _ objc.SEL, link objc.ID) {
				if m := nativeMeters[self]; m != nil {
					m.tick()
				}
			}},
		})
		if err != nil {
			panic(err)
		}
	})
}

// Integer-only calls avoid per-frame reflection in the Objective-C bridge.
func barSend(obj objc.ID, name string, args ...uintptr) objc.ID {
	var words [6]uintptr
	words[0], words[1] = uintptr(obj), uintptr(barSelectors[name])
	n := copy(words[2:], args)
	r, _, _ := purego.SyscallN(barMsgSend, words[:n+2]...)
	return objc.ID(r)
}

func (m *nativeBarMeter) NativeView() uintptr { return uintptr(m.view) }

func (m *nativeBarMeter) attach() {
	if m.link != 0 || barSend(m.view, "window") == 0 {
		return
	}
	m.link = barSend(m.view, "displayLinkWithTarget:selector:", uintptr(m.view), uintptr(barSelectors["meterTick:"]))
	barSend(m.link, "retain")
	// Follow this window's display, matching the painter's display cadence.
	rate := float32(barSend(barSend(barSend(m.view, "window"), "screen"), "maximumFramesPerSecond"))
	if rate > 0 {
		barSetRange(m.link, barSelectors["setPreferredFrameRateRange:"], barRateRange{rate, rate, rate})
	}
	mode := objc.ID(objc.GetClass("NSString")).Send(barSelectors["stringWithUTF8String:"], "kCFRunLoopCommonModes")
	barSend(m.link, "addToRunLoop:forMode:", uintptr(barSend(objc.ID(objc.GetClass("NSRunLoop")), "mainRunLoop")), uintptr(mode))
	m.pause(false)
}

func (m *nativeBarMeter) pause(paused bool) {
	if m.link == 0 || m.view == 0 {
		return
	}
	if !paused {
		win := barSend(m.view, "window")
		if m.suspended || win == 0 || barSend(m.view, "isHiddenOrHasHiddenAncestor") != 0 || barSend(win, "isVisible") == 0 || barSend(win, "isMiniaturized") != 0 {
			return
		}
	}
	m.paused = paused
	var value uintptr
	if paused {
		value = 1
	}
	barSend(m.link, "setPaused:", value)
}

func (m *nativeBarMeter) tick() {
	win := barSend(m.view, "window")
	if m.suspended || win == 0 || barSend(m.view, "isHiddenOrHasHiddenAncestor") != 0 || barSend(win, "isVisible") == 0 || barSend(win, "isMiniaturized") != 0 {
		m.hidden++
		m.pause(true)
		return
	}
	m.ticks++
	levels, live := m.a.Player().Levels()
	m.a.RowBars.Step(time.Now(), levels, live)
	defer func() {
		if !live && m.a.RowBars.Group(0, len(levels)) <= 0.01 {
			m.pause(true)
		}
	}()
	var frames [4]ui.Rect
	var radii [4]float32
	i := 0
	scale := m.scale
	if scale <= 0 {
		scale = 1
	}
	components.EachBarRect(ui.Rect{W: 16, H: 14}, &m.a.RowBars, 4, 2.5, func(r ui.Rect) {
		// Match Painter.snap exactly, including the element's fractional origin.
		snap := func(v float32) float32 { return float32(math.Round(float64(v*scale))) / scale }
		x0, y0 := snap(m.origin.X+r.X), snap(m.origin.Y+r.Y)
		x1, y1 := snap(m.origin.X+r.X+r.W), snap(m.origin.Y+r.Y+r.H)
		radii[i] = r.W / 2
		frames[i] = ui.Rect{X: x0 - snap(m.origin.X), Y: y0 - snap(m.origin.Y), W: x1 - x0, H: y1 - y0}
		i++
	})
	if frames == m.frames && m.tint == m.paintTint && m.bg == m.paintBG {
		return
	}
	pool, _, _ := purego.SyscallN(barPoolPush)
	defer purego.SyscallN(barPoolPop, pool)
	tx := objc.ID(objc.GetClass("CATransaction"))
	barSend(tx, "begin")
	barSend(tx, "setDisableActions:", 1) // no extra lag or default 250 ms easing
	if m.bg != m.paintBG {
		barSend(barSend(m.view, "layer"), "setBackgroundColor:", uintptr(nativeBarColor(m.bg)))
	}
	for i, r := range frames {
		if m.tint != m.paintTint {
			barSend(m.layers[i], "setBackgroundColor:", uintptr(nativeBarColor(m.tint)))
		}
		if r != m.frames[i] {
			barSetRect(m.layers[i], barSelectors["setFrame:"], barNativeRect{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)})
			barSetFloat(m.layers[i], barSelectors["setCornerRadius:"], float64(radii[i]))
		}
	}
	barSend(tx, "commit")
	m.frames, m.paintTint, m.paintBG = frames, m.tint, m.bg
	m.writes++
}

func nativeBarColor(c ui.Color) objc.ID {
	ns := barColor(objc.ID(objc.GetClass("NSColor")), barSelectors["colorWithSRGBRed:green:blue:alpha:"], float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, float64(c.A)/255)
	return barSend(ns, "CGColor")
}

func (m *nativeBarMeter) close() {
	if m.view == 0 {
		return
	}
	for _, off := range m.off {
		off()
	}
	barSend(m.link, "invalidate")
	barSend(m.link, "release")
	for _, layer := range m.layers {
		barSend(layer, "removeFromSuperlayer")
		barSend(layer, "release")
	}
	delete(nativeMeters, m.view)
	delete(nativeApps, m.a)
	barSend(m.view, "release")
	m.view, m.link = 0, 0
}
