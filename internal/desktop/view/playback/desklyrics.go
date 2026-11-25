/*
管理桌面歌词窗口，处理歌词布局、逐字绘制、位置记忆和锁定交互。
*/

package playback

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/lifetime"
	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/lyrics"
)

// DeskLyrics is the song's lyrics in a window of their own, over every
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
type DeskLyrics struct {
	Lifetime lifetime.Subscriptions
	A        *View
	Win      *mygo.Window

	shaped string // what the window is laid out as, as the settings last made it
	shown  bool   // whether the window was shown at its last update
	Hot    bool   // the pointer over the padlock of a locked window
}

// lyricPoll is how often a locked window looks for its padlock: it takes no
// pointer elsewhere, so it hears no pointer arriving.
const lyricPoll = 60 * time.Millisecond

// iconUnlock is the open padlock, drawn as paths rather than assembled from
// boxes, so its arm stays round and its gap clear at every size.
var iconUnlock = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/></svg>`))

// lyricColors are the colours the lyrics may show in.
var LyricColors = []struct {
	Name string
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

// LyricColor returns the colour named, or fallback for a name it does not
// know: settings.json may say anything, and a colour it cannot read would
// take the window down with it.
func LyricColor(name string, fallback ui.Color) ui.Color {
	for _, lc := range LyricColors {
		if lc.Name == name {
			return lc.c
		}
	}
	return fallback
}

// LyricFamily returns the selected system family, or an empty family for
// the system default.
func LyricFamily(name string) string {
	if name == "" || name == LyricFontSystem {
		return ""
	}
	return name
}

// The sizes the lines may stand at, in points, as the settings offer them.
const (
	LyricSizeMin = components.LyricSizeMin
	LyricSizeMax = components.LyricSizeMax
)

// LyricWindowSize is how large the window stands before anything is measured
// in it: wide and short, larger with larger type and with more lines than
// one. What the words themselves take is measured as they are set, and the
// window is grown to hold them.
func LyricWindowSize(size float64, lines int) (w, h float32) {
	size = min(max(size, LyricSizeMin), LyricSizeMax)
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

// LyricWindowLines are the lines the window shows, from start, with the one
// at cur among them lit: that one and the ones after it, as many as the
// window shows. Before the first line, the first ones.
func LyricWindowLines(lines []lyrics.Line, cur int, count int) (shown []lyrics.Line, start int) {
	if len(lines) == 0 {
		return nil, 0
	}
	n := min(max(count, 1), lyricLinesMax)
	start = max(cur, 0)
	return lines[start:min(len(lines), start+n)], start
}

// LyricWindowArea returns the work area where the saved window has most of
// its area, so its screen coordinates select the display, not the primary one.
// A window wholly off the connected displays belongs to the nearest display.
func LyricWindowArea(x, y int, w, h float32, displays []mygo.Display) mygo.Rectangle {
	if x == 0 && y == 0 || len(displays) == 0 {
		if len(displays) > 0 {
			return displays[0].WorkArea
		}
		return mygo.Rectangle{}
	}
	bounds := mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)}
	best, bestArea := 0, -1
	for i, d := range displays {
		a := d.WorkArea
		ow := min(a.X+a.Width, bounds.X+bounds.Width) - max(a.X, bounds.X)
		oh := min(a.Y+a.Height, bounds.Y+bounds.Height) - max(a.Y, bounds.Y)
		if area := max(ow, 0) * max(oh, 0); area > bestArea {
			best, bestArea = i, area
		}
	}
	if bestArea > 0 {
		return displays[best].WorkArea
	}
	// MyGo's DisplayNearestPoint matches an edge-inclusive point to a display;
	// use the saved window's centre once its display has been removed.
	cx, cy := x+int(w)/2, y+int(h)/2
	bestDist := -1
	for i, d := range displays {
		b := d.Bounds
		dx := max(b.X-cx, 0, cx-(b.X+b.Width))
		dy := max(b.Y-cy, 0, cy-(b.Y+b.Height))
		if dist := dx*dx + dy*dy; bestDist < 0 || dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return displays[best].WorkArea
}

// LyricWindowPlace centres a newly opened window, its centre three quarters
// of the way down the screen's work area. It keeps the window on that screen.
func LyricWindowPlace(x, y int, w, h float32, area mygo.Rectangle) (int, int) {
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

// NewDeskLyrics makes the window, showing it if the listener asked for it.
func NewDeskLyrics(a *View) *DeskLyrics {
	d := &DeskLyrics{A: a}
	s := a.Config
	// Use saved size if available, otherwise calculate from settings
	w, h := float32(s.LyricWidth), float32(s.LyricHeight)
	if w == 0 || h == 0 {
		w, h = LyricWindowSize(s.LyricSize, s.LyricLines)
	}
	x, y := LyricWindowPlace(s.LyricX, s.LyricY, w, h, LyricWindowArea(s.LyricX, s.LyricY, w, h, mygo.Screen.Displays()))
	d.Win = mygo.NewWindow(mygo.WindowOptions{
		Title:           mygo.App.Name() + "桌面歌词",
		Hidden:          true,
		AutoHideMenuBar: true,
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
		Content:       ui.View(d.View),
	})
	clearFrame(d.Win)
	// Escape and the like close a panel: these lyrics stay, until the
	// listener turns them off or the app quits.
	d.Win.OnClose(func(e *mygo.CloseEvent) {
		if a.Quitting() {
			return
		}
		e.PreventDefault()
		s.LyricWindow = false
		s.SaveSoon()
		d.Apply()
	})
	// A move or a size change comes from the window, not from polling it.
	d.Win.OnMove(func() {
		if !a.Quitting() && !s.LyricLocked {
			d.Remember()
		}
	})
	d.Win.OnResize(func() {
		if !a.Quitting() && !s.LyricLocked {
			d.Remember()
		}
	})
	// A screen gone, or another one added, may leave the window off them.
	d.Lifetime.Listen(d.Win.OnClosed(d.Close))
	d.Lifetime.Listen(mygo.Screen.OnDisplaysChanged(d.Apply))
	d.Apply()
	return d
}

func (d *DeskLyrics) Close() {
	d.Lifetime.Close()
	d.Win = nil
	d.shown, d.Hot = false, false
}

// Apply makes the window as the settings say it stands: as large as they ask
// or as it was left, where it was left, locked or not, and showing or not.
func (d *DeskLyrics) Apply() {
	if d.Lifetime.Closed || d.Win == nil {
		return // as in tests
	}
	s := d.A.Config
	needW, needH := LyricWindowSize(s.LyricSize, s.LyricLines)
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
		x, y := LyricWindowPlace(s.LyricX, s.LyricY, w, h, LyricWindowArea(s.LyricX, s.LyricY, w, h, mygo.Screen.Displays()))
		d.Win.SetMinimumSize(int(needW), int(needH))
		d.Win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)})
		d.Win.SetHasShadow(false)
		clearFrame(d.Win)
		if reshaped {
			// A new shape sets the size that is remembered from here on.
			s.LyricWidth, s.LyricHeight = int(w), int(h)
			s.SaveSoon()
		}
	}
	// The transparent edges resize only while unlocked; a locked window
	// takes no pointer, so it cannot be resized or moved either.
	d.Win.SetMovable(!s.LyricLocked)
	d.Hot = false
	d.Win.SetIgnoreMouseEvents(s.LyricLocked)
	clearFrame(d.Win)
	d.Win.SetAlwaysOnTop(true)
	d.shown = s.LyricWindow
	if s.LyricWindow {
		d.Win.ShowInactive()
		clearFrame(d.Win)
	} else {
		d.Win.Hide()
	}
	d.Win.Invalidate()
}

// setLocked locks the window in place or lets it go, without making it again:
// its size and place are kept as they stand, so that locking it does not
// nudge it and unlocking it does not set it back.
func (d *DeskLyrics) setLocked(locked bool) {
	s := d.A.Config
	if locked {
		d.Remember()
	}
	s.LyricLocked = locked
	s.SaveSoon()
	if d.Win == nil {
		return // as in tests
	}
	d.Win.SetMovable(!locked)
	d.Hot = false
	d.Win.SetIgnoreMouseEvents(locked)
	clearFrame(d.Win)
	d.Win.Invalidate()
}

// Remember keeps where the window stands and how large it was left, for the
// next time the app opens, and for the window to be found again on a screen
// that changed.
func (d *DeskLyrics) Remember() {
	if d.Win == nil {
		return // as in tests
	}
	s := d.A.Config
	b := d.Win.Bounds()
	if b.X == s.LyricX && b.Y == s.LyricY && b.Width == s.LyricWidth && b.Height == s.LyricHeight {
		return
	}
	s.LyricX, s.LyricY = b.X, b.Y
	s.LyricWidth, s.LyricHeight = b.Width, b.Height
	s.SaveSoon()
}

// unlockBox is where the padlock stands in a locked window: at the middle of
// its top edge, in the same window as the words.
func (d *DeskLyrics) unlockBox() ui.Rect {
	if d.Win == nil {
		return ui.Rect{} // as in tests
	}
	b := d.Win.Bounds()
	return ui.Rect{X: float32(b.Width-44) / 2, Y: 2, W: 44, H: 44}
}

// overUnlock reports whether the pointer is over the padlock. A locked window
// takes no pointer anywhere else, so this is the only part to watch.
func (d *DeskLyrics) overUnlock() bool {
	if d.Win == nil {
		return false // as in tests
	}
	pt := mygo.Screen.CursorScreenPoint()
	b := d.Win.Bounds()
	r := d.unlockBox()
	x, y := float32(pt.X-b.X), float32(pt.Y-b.Y)
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// ToggleDeskLyrics shows the lyrics window or hides it, as its own button
// does and the menus.
func (a *View) ToggleDeskLyrics() {
	a.Config.LyricWindow = !a.Config.LyricWindow
	a.SettingsChanged()
}

// controls are what the window may be asked while it is not locked, and only
// while the pointer is over it: to be locked, to open the settings, or to go.
func (d *DeskLyrics) controls(c *ui.Context) {
	a := d.A
	button := func(key, label string, on func()) {
		b := ui.ButtonBase(c.Key(key)).Padding(3, 9).Radius(999).
			Border(1, White.Alpha(0.6)).FocusRing(false)
		b.Children(func() {
			text := ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(White)
			if a.Config.UIFont != "" {
				text.Font(a.Config.UIFont)
			}
		})
		if b.Clicked() {
			on()
		}
	}
	button("lock", "锁定", func() { d.setLocked(true) })
	button("settings", "设置", func() { a.OpenSettings() })
	button("off", "关闭", func() { a.ToggleDeskLyrics() })
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

// paintDesktopWords reuses the lyric page's timing and selection geometry,
// adding the desktop's outline and centre alignment.
func (d *DeskLyrics) paintDesktopWords(c *ui.Context, line *lyrics.Line, size float32, weight int, family, lang string, played, idle, outline ui.Color, outlineWidth float32) {
	st := LinesStyle{Size: size, Weight: weight, family: family, lang: lang}
	font := LyricFont(c, st)
	width, _ := c.Size()
	if width <= 0 {
		width = 10000 // an unconstrained first pass; the laid-out pass gets the window width
	}
	width -= 2 * outlineWidth
	layout := measureLyricWords(line, width, font, c.Direction())
	w, h := layout.Size()
	clips := lyricWordClips(line.Words, layout)
	w += 2 * outlineWidth
	h += 2 * outlineWidth
	ui.Box(c).Width(w).Height(h).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		r.X += outlineWidth
		r.Y += outlineWidth
		drawOutlinedLyricWords(p, r, line.Words, layout, clips, d.A.Player().Position(), d.A.Player().Playing(), played, idle, outline, outlineWidth)
	})
}

// lyricLineAlignment applies the current line's setting. In two-line mode
// i is zero for its first line; in one-line mode it is the shared setting.
func lyricLineAlignment(choice int) ui.Align {
	switch choice {
	case 0:
		return ui.Start
	case 2:
		return ui.End
	default:
		return ui.Center
	}
}

func (d *DeskLyrics) View(c *ui.Context) {
	a := d.A
	s := a.Config
	pl := a.Player()
	c.Root().Background(ui.RGBA(0, 0, 0, 0))

	// Over an unlocked window, a light-dark plate shows where it ends. A locked window watches the
	// one part that takes a pointer, its padlock, and lets the rest through.
	over := c.Root().Hovered()
	if !s.LyricLocked && d.Win != nil {
		// Include the window's edges (non-client area) where Windows shows
		// the resize cursor: the grey background should appear there too.
		pt := mygo.Screen.CursorScreenPoint()
		b := d.Win.Bounds()
		over = over || (pt.X >= b.X && pt.X < b.X+b.Width && pt.Y >= b.Y && pt.Y < b.Y+b.Height)
	}
	if s.LyricLocked {
		over = d.Hot
		if d.Win != nil && s.LyricWindow {
			if hot := d.overUnlock(); hot != d.Hot {
				d.Hot = hot
				d.Win.SetIgnoreMouseEvents(!hot)
				d.Win.SetHasShadow(false)
				clearFrame(d.Win)
			}
			// Wayland applies the input shape on the next surface commit. Keep
			// committing while locked so the unlock button cannot stay click-through.
			d.Win.Invalidate()
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
	size := float32(min(max(s.LyricSize, LyricSizeMin), LyricSizeMax))
	weight := s.LyricWeight
	played := LyricColor(s.LyricPlayed, White)
	idle := LyricColor(s.LyricIdle, White.Alpha(0.55))
	trans := LyricColor(s.LyricTransColor, White.Alpha(0.7))
	family := LyricFamily(s.LyricFont)
	transFamily := LyricFamily(s.LyricTransFont)
	shown, start := LyricWindowLines(lines, cur, s.LyricLines)
	lang := pl.LyricLanguage()

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
		words = ui.Column(c).Absolute().Top(0).Bottom(0).WidthPercent(100).Justify(ui.Center).
			Gap(float32(s.LyricLineGap)).PassThrough()
	}
	if over {
		if s.LyricLocked {
			// The only control that can be clicked: the same window takes
			// the pointer over its padlock and nowhere else.
			r := d.unlockBox()
			if d.Win == nil {
				r = ui.Rect{X: float32(c.Root().Bounds().W-44) / 2, Y: 2, W: 44, H: 44}
			}
			button := ui.ButtonBase(c.Key("unlock")).Label("解锁").Padding(9).Radius(999).
				Border(1.5, White.Alpha(0.9)).FocusRing(false).Absolute().Left(r.X).Top(r.Y)
			button.Children(func() {
				ui.Icon(c, iconUnlock).Size(20, 20).TextColor(White)
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
	case len(lines) == 0 && pl.LyricsError() != "":
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, pl.LyricsError()).FontSize(size * .8).FontWeight(weight).TextColor(idle).PassThrough()
		})
	case len(lines) == 0 && loaded:
		// Instrumental and unavailable notices use the current line's alignment,
		// just as the first of two sung lines and the one shared line do.
		ui.Column(c).Fill().AlignItems(lyricLineAlignment(s.LyricAlign)).Justify(ui.Center).Children(func() {
			display, _ := pl.LyricsDisplay()
			ui.Text(c, display.EmptyText()).FontSize(size * 0.8).FontWeight(weight).TextColor(idle).PassThrough()
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
				choice := s.LyricAlign
				if i > 0 {
					choice = s.LyricNextAlign
				}
				alignment := lyricLineAlignment(choice)
				line := ui.Column(c).Gap(4).AlignItems(alignment).PassThrough()
				line.Children(func() {
					fontSize := size
					if on {
						fontSize = size * 1.04
					}
					outlineColor := LyricColor(s.LyricOutlineColor, ui.Hex("#16161a"))
					// Clamp outline width to prevent layout issues from corrupted settings
					outlineWidth := float32(max(0, min(2.5, s.LyricOutlineWidth)))
					if on && s.LyricKaraoke && len(l.Words) > 0 {
						d.paintDesktopWords(c, &l, fontSize, weight, family, lang, played, idle, outlineColor, outlineWidth)
					} else {
						outlinedText(c, l.Text, fontSize, weight, family, tint, outlineColor, outlineWidth, lang)
					}
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
