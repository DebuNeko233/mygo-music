package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// deskLyrics is the song's lyrics in a window of their own, over every
// other window: the line being sung lit, the ones after it with it, and the
// translation under each if the listener asks for it.
//
// Nothing of the window shows but the words: no plate, no border, so what
// is under it is what it stands on. The window is never smaller than the
// lines need, and it grows until each line is shown whole, so a line is
// never broken off the end of the row.
//
// The controls come back under the pointer only: unlocked, anything of the
// window drags it and its edges resize it. Locked, it takes no pointer
// except over its padlock, which is in this window too: the lock opens
// without another window. Where it was left and how large, it opens again.
type deskLyrics struct {
	a   *app
	win *mygo.Window

	shaped string // what the window is laid out as, as the settings last made it
	shown  bool   // whether the window was shown at its last update
	hot    bool   // the pointer over the padlock of a locked window
	resize *lyricResize
}

// lyricPoll is how often a locked window looks for its padlock: it takes no
// pointer elsewhere, so it hears no pointer arriving.
const lyricPoll = 60 * time.Millisecond

// lyricResize remembers where a transparent resize edge was grabbed. Windows
// has no native resize edge without a thick frame, whose DWM shadow it draws.
type lyricResize struct {
	bounds        mygo.Rectangle
	x, y          float32 // screen coordinates at grab
	left, top     bool
	right, bottom bool
}

// moved resizes from the edges held, never smaller than the words allow.
// x, y are the pointer's current screen coordinates.
func (r *lyricResize) moved(x, y float32, minW, minH int) mygo.Rectangle {
	b := r.bounds
	dx, dy := int(x-r.x), int(y-r.y)
	if r.right {
		b.Width = max(minW, b.Width+dx)
	}
	if r.bottom {
		b.Height = max(minH, b.Height+dy)
	}
	if r.left {
		w := max(minW, b.Width-dx)
		b.X += b.Width - w
		b.Width = w
	}
	if r.top {
		h := max(minH, b.Height-dy)
		b.Y += b.Height - h
		b.Height = h
	}
	return b
}

// iconUnlock is the open padlock, drawn as paths rather than assembled from
// boxes, so its arm stays round and its gap clear at every size.
var iconUnlock = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/></svg>`))

// lyricColors are the colours the lyrics may show in.
var lyricColors = []struct {
	name string
	c    ui.Color
}{
	{"白色", ui.Hex("#ffffff")},
	{"银灰", ui.Hex("#d2d3da")},
	{"天蓝", ui.Hex("#8fd8ff")},
	{"湖蓝", ui.Hex("#5cb3ff")},
	{"薄荷", ui.Hex("#71e3c4")},
	{"青柠", ui.Hex("#c2ec74")},
	{"暖黄", ui.Hex("#ffd97a")},
	{"珊瑚", ui.Hex("#ff9b78")},
	{"玫红", ui.Hex("#ff86b0")},
	{"紫罗兰", ui.Hex("#c2a8ff")},
	{"墨黑", ui.Hex("#16161a")},
}

// lyricColor returns the colour named, or fallback for a name it does not
// know: settings.json may say anything, and a colour it cannot read would
// take the window down with it.
func lyricColor(name string, fallback ui.Color) ui.Color {
	for _, lc := range lyricColors {
		if lc.name == name {
			return lc.c
		}
	}
	return fallback
}

// lyricFontSystem is the choice standing for the font the system takes for
// text of its own, which an unset setting picks as well.
const lyricFontSystem = "跟随系统"

// lyricFontNames lists the fonts the settings offer: the system's own first,
// then the families this system has, which the lines are set in as every
// other word of the app is. MyGo lists them once, as they stay the same
// while the app runs, and a system whose text it has no engine for lists
// none, leaving the settings the system's own alone.
var lyricFontNames = sync.OnceValue(func() []string {
	names := []string{lyricFontSystem}
	for _, f := range ui.FontFamilies() {
		if f.Name != "" && f.Name != lyricFontSystem {
			names = append(names, f.Name)
		}
	}
	return names
})

// lyricOldFonts are the fonts that settings files written before the
// settings listed the system's own names call by the app's names, and the
// families each one stood for: an older choice keeps showing in the fonts
// it did, and the settings keep offering it as it is until it is replaced.
var lyricOldFonts = map[string]string{
	"微软雅黑": "微软雅黑, Microsoft YaHei, PingFang SC, Noto Sans CJK SC",
	"苹方":   "PingFang SC, Hiragino Sans GB, Microsoft YaHei",
	"思源黑体": "Source Han Sans SC, Noto Sans CJK SC, Noto Sans SC",
	"等线":   "DengXian, DengXian Light",
	"黑体":   "SimHei, Heiti SC",
	"宋体":   "SimSun, Songti SC",
	"楷体":   "KaiTi, Kaiti SC",
	"圆体":   "Yuanti SC, YouYuan",
}

// lyricFamily returns the families the font named stands for: the system's
// own for an empty name and for the choice meaning it, the families of an
// older name for one of those, and the name itself for a family the system
// gives, which a system without such a family sets in the system's own.
func lyricFamily(name string) string {
	if name == "" || name == lyricFontSystem {
		return ""
	}
	if families, ok := lyricOldFonts[name]; ok {
		return families
	}
	return name
}

// The sizes the lines may stand at, in points, as the settings offer them.
const (
	lyricSizeMin = 14
	lyricSizeMax = 48
)

// lyricWindowSize is how large the window stands before anything is measured
// in it: wide and short, larger with larger type and with more lines than
// one. What the words themselves take is measured as they are set, and the
// window is grown to hold them.
func lyricWindowSize(size float64, lines int) (w, h float32) {
	size = min(max(size, lyricSizeMin), lyricSizeMax)
	n := float64(min(max(lines, 1), lyricLinesMax))
	// A line per row, each with its translation under it.
	return 840, float32(36 + size*2.6*n)
}

// lyricShape is what the window is laid out as: at this size, showing this
// many lines. A shape changed makes the window again, as large as the new one
// needs; locking it or not leaves its size alone.
func lyricShape(size float64, lines int) string {
	return fmt.Sprintf("%v/%d", size, lines)
}

// lyricLinesMax is as many lines as the window shows at once.
const lyricLinesMax = 2

// lyricWindowLines are the lines the window shows, from start, with the one
// at cur among them lit: that one and the ones after it, as many as the
// window shows. Before the first line, the first ones.
func lyricWindowLines(lines []lyrics.Line, cur int, count int) (shown []lyrics.Line, start int) {
	if len(lines) == 0 {
		return nil, 0
	}
	n := min(max(count, 1), lyricLinesMax)
	start = max(cur, 0)
	return lines[start:min(len(lines), start+n)], start
}

// lyricWindowPlace centres a newly opened window, its centre three quarters
// of the way down the screen's work area. It keeps the window on that screen.
func lyricWindowPlace(x, y int, w, h float32, area mygo.Rectangle) (int, int) {
	if x == 0 && y == 0 {
		x = area.X + (area.Width-int(w))/2
		y = area.Y + area.Height*3/4 - int(h)/2
	}
	if area.Width > int(w) {
		x = min(max(x, area.X), area.X+area.Width-int(w))
	} else {
		x = area.X
	}
	if area.Height > int(h) {
		y = min(max(y, area.Y), area.Y+area.Height-int(h))
	} else {
		y = area.Y
	}
	return x, y
}

// newDeskLyrics makes the window, showing it if the listener asked for it.
func newDeskLyrics(a *app) *deskLyrics {
	d := &deskLyrics{a: a}
	s := &a.settings
	// Use saved size if available, otherwise calculate from settings
	w, h := float32(s.LyricWidth), float32(s.LyricHeight)
	if w == 0 || h == 0 {
		w, h = lyricWindowSize(s.LyricSize, s.LyricLines)
	}
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title:  mygo.App.Name() + "桌面歌词",
		Hidden: true,
		// Frameless, so that the window draws nothing of itself: no frame, no
		// shadow. Its edges still take the pointer to resize it.
		Frameless:     true,
		Transparent:   true,
		DisableShadow: true,
		SkipTaskbar:   true,
		DisableResize: false, // Windows handles resize via WS_THICKFRAME
		AlwaysOnTop:   true,
		X:             x,
		Y:             y,
		Width:         int(w),
		Height:        int(h),
		Content:       ui.View(d.view),
	})
	// No native frame: resizeEdges supplies the invisible resizing edges.
	clearFrame(d.win)
	// Escape and the like close a panel: these lyrics stay, until the
	// listener turns them off or the app quits.
	d.win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		s.LyricWindow = false
		s.save()
		d.apply()
	})
	// A move or a size change comes from the window, not from polling it.
	d.win.OnMove(func() {
		if !s.LyricLocked {
			d.remember()
		}
	})
	d.win.OnResize(func() {
		if !s.LyricLocked {
			d.remember()
		}
	})
	// A screen gone, or another one added, may leave the window off them.
	mygo.Screen.OnDisplaysChanged(d.apply)
	d.apply()
	return d
}

// apply makes the window as the settings say it stands: as large as they ask
// or as it was left, where it was left, locked or not, and showing or not.
func (d *deskLyrics) apply() {
	if d.win == nil {
		return // as in tests
	}
	s := &d.a.settings
	needW, needH := lyricWindowSize(s.LyricSize, s.LyricLines)
	shape := lyricShape(s.LyricSize, s.LyricLines)
	reshaped := shape != d.shaped
	d.shaped = shape
	// Only opening places and sizes the window. Playing, dragging and resizing
	// must not put it back at a position or size remembered earlier.
	if !s.LyricLocked && (!d.shown || reshaped) {
		// Use saved size if available, otherwise use calculated default
		w, h := float32(s.LyricWidth), float32(s.LyricHeight)
		if w == 0 || h == 0 {
			w, h = needW, needH
		}
		x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
		// Use saved position if available, otherwise center on first open
		if s.LyricX == 0 && s.LyricY == 0 {
			x, y = lyricWindowPlace(0, 0, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
		}
		d.win.SetMinimumSize(int(needW), int(needH))
		d.win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)})
		d.win.SetHasShadow(false)
		clearFrame(d.win)
		if reshaped {
			// A new shape sets the size that is remembered from here on.
			s.LyricWidth, s.LyricHeight = int(w), int(h)
			s.save()
		}
	}
	// The transparent edges resize only while unlocked; a locked window
	// takes no pointer, so it cannot be resized or moved either.
	d.win.SetMovable(!s.LyricLocked)
	d.hot = false
	d.win.SetIgnoreMouseEvents(s.LyricLocked)
	clearFrame(d.win)
	d.win.SetAlwaysOnTop(true)
	d.shown = s.LyricWindow
	if s.LyricWindow {
		d.win.ShowInactive()
		clearFrame(d.win)
	} else {
		d.win.Hide()
	}
	d.win.Invalidate()
}

// setLocked locks the window in place or lets it go, without making it again:
// its size and place are kept as they stand, so that locking it does not
// nudge it and unlocking it does not set it back.
func (d *deskLyrics) setLocked(locked bool) {
	s := &d.a.settings
	if locked {
		d.remember()
	}
	s.LyricLocked = locked
	s.save()
	if d.win == nil {
		return // as in tests
	}
	d.win.SetMovable(!locked)
	d.hot = false
	d.win.SetIgnoreMouseEvents(locked)
	clearFrame(d.win)
	d.win.Invalidate()
}

// remember keeps where the window stands and how large it was left, for the
// next time the app opens, and for the window to be found again on a screen
// that changed.
func (d *deskLyrics) remember() {
	if d.win == nil {
		return // as in tests
	}
	s := &d.a.settings
	b := d.win.Bounds()
	if b.X == s.LyricX && b.Y == s.LyricY && b.Width == s.LyricWidth && b.Height == s.LyricHeight {
		return
	}
	s.LyricX, s.LyricY = b.X, b.Y
	s.LyricWidth, s.LyricHeight = b.Width, b.Height
	s.save()
}

// unlockBox is where the padlock stands in a locked window: at the middle of
// its top edge, in the same window as the words.
func (d *deskLyrics) unlockBox() ui.Rect {
	if d.win == nil {
		return ui.Rect{} // as in tests
	}
	b := d.win.Bounds()
	return ui.Rect{X: float32(b.Width-44) / 2, Y: 2, W: 44, H: 44}
}

// overUnlock reports whether the pointer is over the padlock. A locked window
// takes no pointer anywhere else, so this is the only part to watch.
func (d *deskLyrics) overUnlock() bool {
	if d.win == nil {
		return false // as in tests
	}
	pt := mygo.Screen.CursorScreenPoint()
	b := d.win.Bounds()
	r := d.unlockBox()
	x, y := float32(pt.X-b.X), float32(pt.Y-b.Y)
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// toggleDeskLyrics shows the lyrics window or hides it, as its own button
// does and the menus.
func (a *app) toggleDeskLyrics() {
	a.settings.LyricWindow = !a.settings.LyricWindow
	a.changed()
}

// controls are what the window may be asked while it is not locked, and only
// while the pointer is over it: to be locked, to open the settings, or to go.
func (d *deskLyrics) controls(c *ui.Context) {
	a := d.a
	button := func(key, label string, on func()) {
		b := ui.ButtonBase(c.Key(key)).Padding(3, 9).Radius(999).
			Border(1, white.Alpha(0.6)).FocusRing(false)
		b.Children(func() {
			ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(white)
		})
		if b.Clicked() {
			on()
		}
	}
	button("lock", "锁定", func() { d.setLocked(true) })
	button("settings", "设置", func() { a.openSettings() })
	button("off", "关闭", func() { a.toggleDeskLyrics() })
}

// outlinedText creates a text element with an outline by drawing multiple
// offset copies of the text in the outline color behind the main text.
// This is a common technique when true text stroke is not available.
func outlinedText(c *ui.Context, text string, size float32, weight int, family string, color, outlineColor ui.Color, outlineWidth float32, lang string) {
	if outlineWidth <= 0 {
		// No outline: just draw a simple text element.
		e := ui.Text(c, text).Lang(lang).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(color).PassThrough()
		if family != "" {
			e.Font(family)
		}
		return
	}

	// Measure the text to size the container.
	w, h := c.MeasureText(0, ui.Span{Text: text, Font: family, Size: size, Weight: weight, Lang: lang})
	// Add padding for the outline on all sides.
	w += outlineWidth * 2
	h += outlineWidth * 2

	// Draw outline in 8 directions (N, NE, E, SE, S, SW, W, NW).
	offsets := []struct{ dx, dy float32 }{
		{0, -outlineWidth},                         // N
		{outlineWidth * 0.7, -outlineWidth * 0.7},  // NE
		{outlineWidth, 0},                          // E
		{outlineWidth * 0.7, outlineWidth * 0.7},   // SE
		{0, outlineWidth},                          // S
		{-outlineWidth * 0.7, outlineWidth * 0.7},  // SW
		{-outlineWidth, 0},                         // W
		{-outlineWidth * 0.7, -outlineWidth * 0.7}, // NW
	}

	// Create a container with explicit size for absolute positioning.
	ui.Box(c).Size(w, h).Children(func() {
		// Draw outline layers first, centered with offset.
		for _, off := range offsets {
			ui.Box(c).Absolute().Left(outlineWidth + off.dx).Top(outlineWidth + off.dy).Children(func() {
				e := ui.Text(c, text).Lang(lang).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(outlineColor).PassThrough()
				if family != "" {
					e.Font(family)
				}
			})
		}
		// Draw the main text on top, centered.
		ui.Box(c).Absolute().Left(outlineWidth).Top(outlineWidth).Children(func() {
			e := ui.Text(c, text).Lang(lang).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(color).PassThrough()
			if family != "" {
				e.Font(family)
			}
		})
	})
}

func (d *deskLyrics) view(c *ui.Context) {
	a := d.a
	s := &a.settings
	pl := a.pl
	c.Root().Background(ui.RGBA(0, 0, 0, 0))

	// Over an unlocked window, a light-dark plate shows where it ends. A locked window watches the
	// one part that takes a pointer, its padlock, and lets the rest through.
	over := c.Root().Hovered()
	if !s.LyricLocked && d.win != nil {
		// Include the window's edges (non-client area) where Windows shows
		// the resize cursor: the grey background should appear there too.
		pt := mygo.Screen.CursorScreenPoint()
		b := d.win.Bounds()
		over = over || (pt.X >= b.X && pt.X < b.X+b.Width && pt.Y >= b.Y && pt.Y < b.Y+b.Height)
	}
	if s.LyricLocked {
		over = d.hot
		if d.win != nil && s.LyricWindow {
			if hot := d.overUnlock(); hot != d.hot {
				d.hot = hot
				d.win.SetIgnoreMouseEvents(!hot)
				d.win.SetHasShadow(false)
				clearFrame(d.win)
				d.win.Invalidate()
			}
			c.After(lyricPoll)
		}
	}

	// Nothing to say before anything played.
	if pl.Current() == nil {
		return
	}
	lines, loaded := pl.Lyrics()
	pos := pl.Position()
	cur := -1
	if len(lines) > 0 {
		cur = lyrics.Index(lines, pos)
	}
	size := float32(min(max(s.LyricSize, lyricSizeMin), lyricSizeMax))
	weight := s.LyricWeight
	played := lyricColor(s.LyricPlayed, white)
	idle := lyricColor(s.LyricIdle, white.Alpha(0.55))
	trans := lyricColor(s.LyricTransColor, white.Alpha(0.7))
	family := lyricFamily(s.LyricFont)
	transFamily := lyricFamily(s.LyricTransFont)
	shown, start := lyricWindowLines(lines, cur, s.LyricLines)
	lang := lyrics.Lang(lines)

	if over && !s.LyricLocked {
		c.Root().Background(ui.RGBA(0, 0, 0, 0.2))
	}

	// The window has no plate of its own, showing or not: what is under it is
	// what it stands on. Only the controls come over the words, and only
	// under the pointer, and they say themselves with a ring rather than a
	// background, which would show as a pane over whatever the window stands
	// on.
	var words ui.Element
	if len(shown) > 0 {
		// An absolute group, centered in the window itself: the controls
		// hover above it, rather than making its padding uneven.
		words = ui.Column(c).Absolute().Top(0).Bottom(0).WidthPercent(100).Justify(ui.Center).Gap(8).PassThrough()
	}
	if over {
		if s.LyricLocked {
			// The only control that can be clicked: the same window takes
			// the pointer over its padlock and nowhere else.
			r := d.unlockBox()
			if d.win == nil {
				r = ui.Rect{X: float32(c.Root().Bounds().W-44) / 2, Y: 2, W: 44, H: 44}
			}
			button := ui.ButtonBase(c.Key("unlock")).Label("解锁").Padding(9).Radius(999).
				Border(1.5, white.Alpha(0.9)).FocusRing(false).Absolute().Left(r.X).Top(r.Y)
			button.Children(func() {
				ui.Icon(c, iconUnlock).Size(20, 20).TextColor(white)
			})
			if button.Clicked() {
				d.setLocked(false)
			}
		} else {
			// Windows handles resize natively via WS_THICKFRAME; no manual
			// edge tracking needed. Unlocked, anything of the window drags it:
			// the lines take no pointer themselves, so the window does.
			c.Root().DragWindow()
			ui.Row(c).Absolute().Top(8).WidthPercent(100).Justify(ui.Center).Gap(10).Children(func() { d.controls(c) })
		}
	}

	switch {
	case len(lines) == 0 && loaded:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "纯音乐，请欣赏").FontSize(size * 0.8).FontWeight(weight).TextColor(idle).PassThrough()
		})
	case len(shown) == 0:
	default:
		words.Children(func() {
			for i, l := range shown {
				on := start+i == cur
				tint := idle
				if on {
					tint = played
				}
				line := ui.Column(c).Gap(4).AlignItems(ui.Center).PassThrough()
				line.Children(func() {
					fontSize := size
					if on {
						fontSize = size * 1.04
					}
					outlineColor := lyricColor(s.LyricOutlineColor, ui.Hex("#16161a"))
					// Clamp outline width to prevent layout issues from corrupted settings
					outlineWidth := float32(max(0, min(2.5, s.LyricOutlineWidth)))
					outlinedText(c, l.Text, fontSize, weight, family, tint, outlineColor, outlineWidth, lang)
					if s.LyricTrans && l.Translation != "" {
						outlinedText(c, l.Translation, size*0.62, 500, transFamily, trans, outlineColor, outlineWidth*0.62, c.Locale())
					}
				})
			}
		})
	}

	// Repaint as the next line begins, and often enough that the next song
	// shows, an unlocked window's drag is kept, and a locked one finds the
	// pointer again.
	wait := 500 * time.Millisecond
	if pl.Playing() && cur >= 0 && cur+1 < len(lines) {
		wait = min(wait, lines[cur+1].At-pos+20*time.Millisecond)
	}
	if pl.Playing() || !s.LyricLocked {
		c.After(max(wait, 20*time.Millisecond))
	}
}

// resizeEdges gives the undecorated window invisible edges and corners to
// resize from. They do not cover any buttons, which sit further inside it.
func (d *deskLyrics) resizeEdges(c *ui.Context) {
	const edge = 5.0
	for _, side := range []struct {
		key                      string
		left, top, right, bottom bool
		cursor                   ui.Cursor
	}{
		{"top", false, true, false, false, ui.CursorResizeN},
		{"bottom", false, false, false, true, ui.CursorResizeS},
		{"left", true, false, false, false, ui.CursorResizeW},
		{"right", false, false, true, false, ui.CursorResizeE},
		{"topleft", true, true, false, false, ui.CursorResizeNWSE},
		{"bottomright", false, false, true, true, ui.CursorResizeNWSE},
		{"topright", false, true, true, false, ui.CursorResizeNESW},
		{"bottomleft", true, false, false, true, ui.CursorResizeNESW},
	} {
		e := ui.Box(c.Key("resize-" + side.key)).Absolute().Cursor(side.cursor)
		if side.left {
			e.Left(0).Width(edge)
		}
		if side.right {
			e.Right(0).Width(edge)
		}
		if side.top {
			e.Top(0).Height(edge)
		}
		if side.bottom {
			e.Bottom(0).Height(edge)
		}
		if !side.left && !side.right {
			e.Left(edge).Right(edge)
		}
		if !side.top && !side.bottom {
			e.Top(edge).Bottom(edge)
		}
		side := side
		e.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputPointerDown:
				if ev.Button != 0 || d.win == nil {
					return false
				}
				b := d.win.Bounds()
				pt := mygo.Screen.CursorScreenPoint()
				d.resize = &lyricResize{bounds: b, x: float32(pt.X), y: float32(pt.Y),
					left: side.left, top: side.top, right: side.right, bottom: side.bottom}
				return true
			case ui.InputPointerMove:
				if d.resize == nil {
					return false
				}
				r := d.resize
				w, h := lyricWindowSize(d.a.settings.LyricSize, d.a.settings.LyricLines)
				pt := mygo.Screen.CursorScreenPoint()
				d.win.SetBounds(r.moved(float32(pt.X), float32(pt.Y), int(w), int(h)))
				return true
			case ui.InputPointerUp:
				d.resize = nil
				return true
			}
			return false
		})
	}
}

// needed is as large as the window must stand to show the lines whole: as wide
// as the widest of them and as tall as they stand together. Never smaller than
// the settings ask for.
func (d *deskLyrics) needed(c *ui.Context, shown []lyrics.Line, start, cur int, size float32, weight int, family, transFamily string) (w, h float32) {
	s := &d.a.settings
	w, h = lyricWindowSize(s.LyricSize, s.LyricLines)
	if len(shown) == 0 {
		return w, h
	}
	trans := s.LyricTrans
	lines, _ := d.a.pl.Lyrics()
	lang := lyrics.Lang(lines)
	widest, tall := float32(0), float32(16)
	for i, l := range shown {
		lineSize := size
		if start+i == cur {
			lineSize = size * 1.04
		}
		widest = max(widest, textWidth(c, l.Text, family, lineSize, weight, lang))
		tall += lineSize * 1.35
		if trans && l.Translation != "" {
			widest = max(widest, textWidth(c, l.Translation, transFamily, size*0.62, 500, c.Locale()))
			tall += size * 0.62 * 1.4
		}
	}
	return max(w, widest+32), max(h, tall)
}

// grow is only for opening a window over long words. Playing and hovering
// never change its size: it stays the size the listener dragged it to.
func (d *deskLyrics) grow(c *ui.Context, shown []lyrics.Line, start, cur int, size float32, weight int, family, transFamily string) {
	if d.win == nil || d.a.settings.LyricLocked {
		return // as in tests, and a locked window keeps the size it was locked at
	}
	needW, needH := d.needed(c, shown, start, cur, size, weight, family, transFamily)
	b := d.win.Bounds()
	w, h := max(b.Width, int(needW)), max(b.Height, int(needH))
	if w == b.Width && h == b.Height {
		return
	}
	x, y := lyricWindowPlace(b.X, b.Y, float32(w), float32(h), mygo.Screen.PrimaryDisplay().WorkArea)
	d.win.SetMinimumSize(int(needW), int(needH))
	d.win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: w, Height: h})
	// A new size may bring the window's frame and shadow back.
	clearFrame(d.win)
}

// textWidth is how wide a line is set.
func textWidth(c *ui.Context, text, family string, size float32, weight int, lang string) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Font: family, Size: size, Weight: weight, Lang: lang})
	return w
}
