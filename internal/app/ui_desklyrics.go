package app

import (
	"fmt"
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
// window drags it and its edges resize it. Locked, it takes no pointer at
// all, and the padlock that opens the lock stands in a window of its own, the
// only thing of it that is clicked. Where it was left and how large, it opens
// again.
type deskLyrics struct {
	a   *app
	win *mygo.Window

	pin *mygo.Window // the padlock that opens a locked window, in a window of its own

	looked time.Time // when the window's place was last looked at
	shaped string    // what the window is laid out as, as the settings last made it
}

// pinSide is how large the padlock's window is: large enough to find, and
// small enough to leave what stands under the words usable.
const pinSide = 44

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

// lyricFonts are the fonts the lyrics may show in: name is what the settings
// call one, families the names its systems give it, the first one there
// being the one shown (Element.Font).
var lyricFonts = []struct{ name, families string }{
	{"跟随系统", ""},
	{"微软雅黑", "微软雅黑, Microsoft YaHei, PingFang SC, Noto Sans CJK SC"},
	{"苹方", "PingFang SC, Hiragino Sans GB, Microsoft YaHei"},
	{"思源黑体", "Source Han Sans SC, Noto Sans CJK SC, Noto Sans SC"},
	{"等线", "DengXian, DengXian Light"},
	{"黑体", "SimHei, Heiti SC"},
	{"宋体", "SimSun, Songti SC"},
	{"楷体", "KaiTi, Kaiti SC"},
	{"圆体", "Yuanti SC, YouYuan"},
	{"Arial", "Arial"},
	{"Segoe UI", "Segoe UI"},
	{"Georgia", "Georgia"},
}

// lyricFamily returns the families of the font named, or "" for the
// system's and for a name the app does not know.
func lyricFamily(name string) string {
	for _, f := range lyricFonts {
		if f.name == name {
			return f.families
		}
	}
	return ""
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

// lyricWindowPlace is where the window goes: where it was left, or over the
// foot of the screen when it was never placed, and inside the screen's work
// area all the same, which may have changed since it was left there.
func lyricWindowPlace(x, y int, w, h float32, area mygo.Rectangle) (int, int) {
	if x == 0 && y == 0 {
		x = area.X + (area.Width-int(w))/2
		y = area.Y + area.Height - int(h) - 64
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
	w, h := lyricWindowSize(s.LyricSize, s.LyricLines)
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title:  mygo.App.Name() + "桌面歌词",
		Panel:  true,
		Hidden: true,
		// Frameless, so that the window draws nothing of itself: no frame, no
		// shadow. Its edges still take the pointer to resize it.
		Frameless:     true,
		Transparent:   true,
		DisableShadow: true,
		SkipTaskbar:   true,
		AlwaysOnTop:   true,
		X:             x,
		Y:             y,
		Width:         int(w),
		Height:        int(h),
		Content:       ui.View(d.view),
	})
	// Frameless still leaves the window its edges to be resized by; what it
	// draws of itself is nothing, its shadow gone with the frame.
	d.win.SetHasShadow(false)
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
	// The padlock is a window of its own, standing where the words begin: of
	// a locked window it is the only thing that takes the pointer, so that
	// everything under the rest of the words can be used as if they were not
	// there, and it is always there to open the lock again.
	d.pin = mygo.NewWindow(mygo.WindowOptions{
		Title:         mygo.App.Name() + "桌面歌词解锁",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		DisableShadow: true,
		DisableResize: true,
		SkipTaskbar:   true,
		AlwaysOnTop:   true,
		Width:         pinSide,
		Height:        pinSide,
		Content:       ui.View(d.pinView),
	})
	d.pin.SetHasShadow(false)
	d.pin.OnClose(func(e *mygo.CloseEvent) {
		// Escape and the like close a panel: the lock stays openable.
		e.PreventDefault()
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
	// A locked window is left as it stands: keeping it in place is no reason
	// to make it larger, and its size was remembered when it was locked.
	if !s.LyricLocked {
		w, h := needW, needH
		if !reshaped {
			// As large as it was left, and never made smaller than it
			// stands: only the words growing make it grow.
			b := d.win.Bounds()
			w = max(w, float32(max(s.LyricWidth, b.Width)))
			h = max(h, float32(max(s.LyricHeight, b.Height)))
		}
		x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
		d.win.SetMinimumSize(int(needW), int(needH))
		d.win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)})
		if reshaped {
			// A new shape sets the size that is remembered from here on.
			s.LyricWidth, s.LyricHeight = int(w), int(h)
			s.save()
		}
	}
	// The edges stay resizable for the window's whole life: a locked window
	// takes no pointer, so it cannot be resized or moved either.
	d.win.SetMovable(!s.LyricLocked)
	d.win.SetIgnoreMouseEvents(s.LyricLocked)
	d.win.SetAlwaysOnTop(true)
	if s.LyricWindow {
		d.win.ShowInactive()
	} else {
		d.win.Hide()
	}
	d.win.Invalidate()
	d.placePin()
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
	d.win.SetIgnoreMouseEvents(locked)
	d.win.Invalidate()
	d.placePin()
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
	d.placePin()
}

// placePin stands the padlock's window at the middle of the words' top edge,
// and only while they are locked and shown: unlocked, there is nothing to
// open, and the window is not there to be found.
func (d *deskLyrics) placePin() {
	if d.pin == nil || d.win == nil {
		return // as in tests
	}
	s := &d.a.settings
	if !s.LyricWindow || !s.LyricLocked {
		d.pin.Hide()
		return
	}
	b := d.win.Bounds()
	d.pin.SetBounds(mygo.Rectangle{
		X:      b.X + (b.Width-pinSide)/2,
		Y:      b.Y + 2,
		Width:  pinSide,
		Height: pinSide,
	})
	d.pin.ShowInactive()
}

// pinView is the padlock that opens a locked window: a ring over whatever the
// words stand on, lit under the pointer and dim without it, and the one part
// of a locked window a click reaches.
func (d *deskLyrics) pinView(c *ui.Context) {
	c.Root().Background(ui.RGBA(0, 0, 0, 0))
	over := c.Root().Hovered()
	tint := white.Alpha(0.45)
	border := float32(1.2)
	if over {
		tint, border = white, 1.6
	}
	ring := ui.ButtonBase(c.Key("unlock")).Label("解锁").Padding(6).Radius(999).
		Border(border, tint).FocusRing(false)
	ring.Children(func() {
		padlock(c, true, tint, 20)
	})
	if ring.Clicked() {
		d.setLocked(false)
		return
	}
	if !over {
		// It lights up as the pointer comes to it, which is not a change it
		// hears from a window that sits under the pointer.
		c.After(200 * time.Millisecond)
	}
}

// toggleDeskLyrics shows the lyrics window or hides it, as its own button
// does and the menus.
func (a *app) toggleDeskLyrics() {
	a.settings.LyricWindow = !a.settings.LyricWindow
	a.changed()
}

// controls are what the window may be asked while it is not locked, and only
// while the pointer is over it: to be locked, to open the settings, or to go.
// Unlocking is not one of them: it is the padlock's window that says so.
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

func (d *deskLyrics) view(c *ui.Context) {
	a := d.a
	s := &a.settings
	pl := a.pl
	c.Root().Background(ui.RGBA(0, 0, 0, 0))

	// Over the window, the controls show. A locked window takes no pointer at
	// all, so it hears nothing of one: the padlock in its own window is where
	// the pointer is heard, and it is what opens the lock again.
	over := c.Root().Hovered() && !s.LyricLocked
	if d.win != nil && s.LyricWindow && !s.LyricLocked {
		// The window says nothing of a move or a resize: a look at where it
		// stands now and then keeps where it was left.
		if time.Since(d.looked) > 400*time.Millisecond {
			d.looked = time.Now()
			d.remember()
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

	// The window grows until every line shows whole: a line is never broken
	// off the end of the row.
	d.grow(c, shown, start, cur, size, weight, family, transFamily)

	// The window has no plate of its own, showing or not: what is under it is
	// what it stands on. Only the controls come over the words, and only
	// under the pointer, and they say themselves with a ring rather than a
	// background, which would show as a pane over whatever the window stands
	// on.
	if over {
		// Unlocked, anything of the window drags it: the lines take no
		// pointer themselves, so the window does.
		if !s.LyricLocked {
			c.Root().DragWindow()
		}
		// What it may be asked while it is not locked: to lock, to open the
		// settings, or to go.
		ui.Row(c).Absolute().Top(8).WidthPercent(100).Justify(ui.Center).Gap(10).Children(func() { d.controls(c) })
	}

	switch {
	case len(lines) == 0 && loaded:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "纯音乐，请欣赏").FontSize(size * 0.8).FontWeight(weight).TextColor(idle).PassThrough()
		})
	case len(shown) == 0:
	default:
		// The top is left to the buttons, so that they stand above the words
		// and not on them.
		col := ui.Column(c).Fill().Padding(46, 16, 8, 16).Gap(8).Center().PassThrough()
		col.Children(func() {
			for i, l := range shown {
				on := start+i == cur
				tint := idle
				if on {
					tint = played
				}
				line := ui.Column(c).Gap(4).AlignItems(ui.Center).PassThrough()
				line.Children(func() {
					text := ui.Text(c, l.Text).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(tint).PassThrough()
					if on {
						text.FontSize(size * 1.04)
					}
					if family != "" {
						text.Font(family)
					}
					if s.LyricTrans && l.Translation != "" {
						under := ui.Text(c, l.Translation).FontSize(size * 0.62).FontWeight(500).MaxLines(1).TextColor(trans).PassThrough()
						if transFamily != "" {
							under.Font(transFamily)
						}
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
	widest, tall := float32(0), float32(54)
	for i, l := range shown {
		lineSize := size
		if start+i == cur {
			lineSize = size * 1.04
		}
		widest = max(widest, textWidth(c, l.Text, family, lineSize, weight))
		tall += lineSize * 1.35
		if trans && l.Translation != "" {
			widest = max(widest, textWidth(c, l.Translation, transFamily, size*0.62, 500))
			tall += size * 0.62 * 1.4
		}
	}
	return max(w, widest+32), max(h, tall)
}

// grow makes the window large enough for the lines to show whole, and no
// larger: unlocked, the size it stands at is the listener's, so it only ever
// grows, and only when the words are what need the room.
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
}

// textWidth is how wide a line is set.
func textWidth(c *ui.Context, text, family string, size float32, weight int) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Font: family, Size: size, Weight: weight})
	return w
}

// padlock draws a padlock over the words: a body and the arm of it. Closed,
// the arm stands on the body with its foot behind the body's top edge. Open,
// the arm rises out of the body and comes down short of it, and the gap
// between its foot and the body is what says the lock is open.
func padlock(c *ui.Context, open bool, tint ui.Color, size float32) {
	ui.Box(c).Width(size).Height(size).PassThrough().Children(func() {
		bar := max(size*0.12, 1.5) // as thick as the arm is drawn
		fill := func(x, y, w, h float32) {
			ui.Box(c).Absolute().Left(size * x).Top(size * y).
				Width(size * w).Height(size * h).Radius(bar / 2).Background(tint)
		}
		// The body, its top edge where the arm comes down to it.
		ui.Box(c).Absolute().Left(size * 0.10).Top(size * 0.52).
			Width(size * 0.80).Height(size * 0.44).Radius(size * 0.15).Background(tint)
		if open {
			// The arm lifted out of the body: from its left up and over to
			// its right, coming down short of the body, the gap between
			// them what says the lock is open.
			fill(0.24, 0.12, 0.12, 0.44) // its left side
			fill(0.24, 0.12, 0.60, 0.12) // over the top
			fill(0.72, 0.12, 0.12, 0.30) // down, short of the body
			return
		}
		// Closed, the arm stands on the body, its foot behind the body's top.
		ui.Box(c).Absolute().Left(size*0.20).Top(size*0.12).
			Width(size*0.60).Height(size*0.54).Radius(size*0.17).Border(bar, tint)
	})
}
