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
// translation under each if the listener asks for it. Set across, the
// window is wide and short; set down, it turns, standing its lines in
// columns as Word sets Chinese, upright or turned with the script.
//
// Nothing of the window shows but the words: no plate, no border, so what
// is under it is what it stands on. The window is never smaller than the
// lines need, and it grows until each line is shown whole, so a line is
// never broken off the end of a column.
//
// The controls come back under the pointer only: unlocked, anything of the
// window drags it and its edges resize it; locked, it takes no pointer at
// all, and a pointer resting on it is what brings the unlock back, one
// button in its middle. Where it was left and how large, it opens again.
type deskLyrics struct {
	a   *app
	win *mygo.Window

	looked  time.Time // when the window's place was last looked at
	watched time.Time // when the pointer's place was last looked at
	hot     bool      // whether the pointer stands on the padlock of a locked window
	shaped  string    // what the window is laid out as, as the settings last made it
}

// lyricWatch is how often a locked window looks at where the pointer is: it
// takes no pointer to hear it from.
const lyricWatch = 200 * time.Millisecond

// lyricPoll is how often a locked window asks whether the pointer stands on
// its padlock, the one place it hears the pointer at all.
const lyricPoll = 60 * time.Millisecond

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
// in it: wide and short with the lines set across, narrow and tall with them
// set down, larger with larger type and with more lines than one. What the
// words themselves take is measured as they are set, and the window is grown
// to hold them.
func lyricWindowSize(vertical bool, size float64, lines int) (w, h float32) {
	size = min(max(size, lyricSizeMin), lyricSizeMax)
	n := float64(min(max(lines, 1), lyricLinesMax))
	if vertical {
		// A column per line, each with its translation beside it.
		return float32(30 + size*2.7*n), float32(60 + size*10.5)
	}
	// A line per row, each with its translation under it.
	return 840, float32(36 + size*2.6*n)
}

// lyricShape is what the window is laid out as: set across or down, at this
// size, showing this many lines. A shape changed makes the window again, as
// large as the new one needs; locking it or not leaves its size alone.
func lyricShape(vertical bool, size float64, lines int) string {
	return fmt.Sprintf("%t/%v/%d", vertical, size, lines)
}

// lyricLinesMax is as many lines as the window shows at once.
const lyricLinesMax = 2

// lyricPad is what the window leaves clear at its edges.
const lyricPad = float32(8)

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

// lyricRun is a stretch of a line written one way: a character that stands
// as it is, or words that turn a quarter beside it.
type lyricRun struct {
	text    string
	upright bool
}

// lyricRuns splits a line as Word sets it down a column: a CJK character
// stands upright, one under the next, and a run of letters, digits or marks
// turns a quarter clockwise beside them.
func lyricRuns(text string) []lyricRun {
	var runs []lyricRun
	for _, r := range text {
		upright := uprightRune(r)
		if n := len(runs); n > 0 && runs[n-1].upright == upright && !upright {
			runs[n-1].text += string(r)
			continue
		}
		runs = append(runs, lyricRun{text: string(r), upright: upright})
	}
	return runs
}

// uprightRune reports whether r stands as it is in a column of vertical
// text: the CJK scripts and their marks do, where Latin letters, digits and
// the like turn a quarter.
func uprightRune(r rune) bool {
	// Japanese prolonged sound marks follow the line's direction: across a
	// row they are horizontal, and down a column they turn on their side.
	if r == '\u30fc' || r == '\uff70' {
		return false
	}
	switch {
	case r >= 0x1100 && r <= 0x115f, // Hangul Jamo
		r >= 0x2e80 && r <= 0xa4cf, // CJK marks, kana, ideographs
		r >= 0xac00 && r <= 0xd7a3, // Hangul syllables
		r >= 0xf900 && r <= 0xfaff, // CJK compatibility ideographs
		r >= 0xfe30 && r <= 0xfe4f, // CJK compatibility forms
		r >= 0xff00 && r <= 0xff60, // fullwidth forms
		r >= 0xffe0 && r <= 0xffe6:
		return true
	}
	return false
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
	w, h := lyricWindowSize(s.LyricVertical, s.LyricSize, s.LyricLines)
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         mygo.App.Name() + "桌面歌词",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		DisableResize: true,
		DisableShadow: true,
		SkipTaskbar:   true,
		AlwaysOnTop:   true,
		X:             x,
		Y:             y,
		Width:         int(w),
		Height:        int(h),
		Content:       ui.View(d.view),
	})
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
	needW, needH := lyricWindowSize(s.LyricVertical, s.LyricSize, s.LyricLines)
	shape := lyricShape(s.LyricVertical, s.LyricSize, s.LyricLines)
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
	// Keep the native resize border off for the window's whole life: on
	// Windows, toggling it changes the outer size, even for a frameless
	// window. Mouse pass-through already prevents resizing while locked.
	d.win.SetMovable(!s.LyricLocked)
	d.win.SetIgnoreMouseEvents(s.LyricLocked)
	// Locked anew or let go, the pointer is the desktop's but for the padlock.
	d.hot = false
	d.win.SetAlwaysOnTop(true)
	if s.LyricWindow {
		d.win.ShowInactive()
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
	d.hot = false
	if d.win == nil {
		return // as in tests
	}
	d.win.SetMovable(!locked)
	d.win.SetIgnoreMouseEvents(locked)
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

// unlockBox is where the padlock stands in the window: in the middle of its
// top edge set across, in the middle of its left edge set down, as large as
// the ring that holds it.
func (d *deskLyrics) unlockBox() ui.Rect {
	b := d.win.Bounds()
	const side = float32(44)
	if d.a.settings.LyricVertical {
		return ui.Rect{X: 8, Y: (float32(b.Height) - side) / 2, W: side, H: side}
	}
	return ui.Rect{X: (float32(b.Width) - side) / 2, Y: 8, W: side, H: side}
}

// overUnlock reports whether the pointer stands on the padlock of a locked
// window, the one place it hears the pointer: everywhere else the pointer is
// the desktop's, so that what is under the words can be used as it could were
// the words not there.
func (d *deskLyrics) overUnlock() bool {
	if d.win == nil {
		return false
	}
	b := d.win.Bounds()
	r := d.unlockBox()
	p := mygo.Screen.CursorScreenPoint()
	return p.X >= b.X+int(r.X) && p.X < b.X+int(r.X+r.W) &&
		p.Y >= b.Y+int(r.Y) && p.Y < b.Y+int(r.Y+r.H)
}

// toggleDeskLyrics shows the lyrics window or hides it, as its own button
// does and the menus.
func (a *app) toggleDeskLyrics() {
	a.settings.LyricWindow = !a.settings.LyricWindow
	a.changed()
}

// controls are what the window may be asked, and only while the pointer is
// over it: to be locked, to open the settings, to be closed, and once it is
// locked, to be unlocked again by the padlock that says it is.
func (d *deskLyrics) controls(c *ui.Context) {
	a := d.a
	s := &a.settings
	if s.LyricLocked {
		unlock := ui.ButtonBase(c.Key("unlock")).Label("解锁").Padding(10).Radius(999).
			Border(1.5, white.Alpha(0.9)).FocusRing(false)
		unlock.Children(func() {
			padlock(c, true, white, 22)
		})
		if unlock.Clicked() {
			d.setLocked(false)
		}
		return
	}
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

	// Where the pointer is: over the window, the controls show. A locked
	// window takes no pointer at all, so it looks at where the pointer is
	// instead, and hears it only over the padlock that opens the lock.
	over := c.Root().Hovered()
	if d.win != nil && s.LyricWindow {
		switch {
		case s.LyricLocked:
			if time.Since(d.watched) > lyricPoll {
				d.watched = time.Now()
				if hot := d.overUnlock(); hot != d.hot {
					d.hot = hot
					d.win.SetIgnoreMouseEvents(!hot)
					d.win.Invalidate()
				}
			}
			c.After(lyricPoll)
		default:
			// The window says nothing of a move or a resize: a look at
			// where it stands now and then keeps where it was left.
			if time.Since(d.looked) > 400*time.Millisecond {
				d.looked = time.Now()
				d.remember()
			}
		}
	}
	// Locked, it shows nothing of itself but under a pointer that stands on
	// its padlock: hovering is not a thing a window that takes no pointer hears.
	if s.LyricLocked {
		over = d.hot
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

	// The window grows until every line shows whole: set across, a line is
	// never broken off the end of the row, and set down, it stands in one
	// column where the screen has the room for it.
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
		// One ring, the middle of the top edge, while it is locked: the only
		// thing a locked window may be asked, and large enough to find again.
		// The three it may be asked while it is not stand there too.
		if s.LyricVertical {
			// Down the left edge, clear of the columns.
			ui.Column(c).Absolute().Left(8).HeightPercent(100).Justify(ui.Center).Gap(10).Children(func() { d.controls(c) })
		} else {
			// Along the top edge, the middle of it.
			ui.Row(c).Absolute().Top(8).WidthPercent(100).Justify(ui.Center).Gap(10).Children(func() { d.controls(c) })
		}
	}

	switch {
	case len(lines) == 0 && loaded:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "纯音乐，请欣赏").FontSize(size * 0.8).FontWeight(weight).TextColor(idle).PassThrough()
		})
	case len(shown) == 0:
	case s.LyricVertical:
		// Lines stand in columns, the first of them rightmost, as vertical
		// text runs right to left, and each translation beside the line it
		// belongs to. A line longer than the column scrolls up it as it is
		// sung: it never breaks off into a column of its own.
		height := d.columnHeight(c, shown, start, cur, size, weight, family, transFamily)
		// The left edge is left to the padlock, so that a locked window's one
		// button stands where the columns do not.
		area := ui.Row(c).Fill().Padding(lyricPad, lyricPad, lyricPad, lyricPad+44).Gap(size * 0.55).
			Justify(ui.End).AlignItems(ui.Start).PassThrough()
		area.Children(func() {
			for i := len(shown) - 1; i >= 0; i-- {
				l := shown[i]
				on := start+i == cur
				tint, lineSize := idle, size
				if on {
					tint, lineSize = played, size*1.04
				}
				group := ui.Row(c).Gap(size * 0.18).AlignItems(ui.Start).PassThrough()
				group.Children(func() {
					if s.LyricTrans && l.Translation != "" {
						lyricColumn(c, l.Translation, transFamily, size*0.62, 500, trans, height, 0)
					}
					lyricColumn(c, l.Text, family, lineSize, weight, tint, height,
						lyricScroll(c, l, pos, family, lineSize, weight, height))
				})
			}
		})
	default:
		// The top is left to the buttons, so that they stand above the words and
		// not on them.
		col := ui.Column(c).Fill().Padding(30, 16, 8, 16).Gap(8).Center().PassThrough()
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
	if s.LyricLocked {
		wait = lyricWatch
	}
	if pl.Playing() && cur >= 0 && cur+1 < len(lines) {
		wait = min(wait, lines[cur+1].At-pos+20*time.Millisecond)
	}
	if pl.Playing() || !s.LyricLocked {
		c.After(max(wait, 20*time.Millisecond))
	}
}

// needed is as large as the window must stand to show the lines whole: set
// across, as wide as the widest of them and as tall as they stand together;
// set down, as tall as the longest of them and as wide as their columns
// take. Never smaller than the settings ask for.
func (d *deskLyrics) needed(c *ui.Context, shown []lyrics.Line, start, cur int, size float32, weight int, family, transFamily string) (w, h float32) {
	s := &d.a.settings
	w, h = lyricWindowSize(s.LyricVertical, s.LyricSize, s.LyricLines)
	if len(shown) == 0 {
		return w, h
	}
	trans := s.LyricTrans
	if s.LyricVertical {
		columns, tallest := float32(2*lyricPad+44), float32(0)
		for i, l := range shown {
			lineSize := size
			if start+i == cur {
				lineSize = size * 1.04
			}
			tallest = max(tallest, lyricExtent(c, l.Text, family, lineSize, weight))
			columns += lineSize*1.3 + lineSize*0.55 // a column, and the room to the one beside it
			if trans && l.Translation != "" {
				columns += size*0.62*1.3 + lineSize*0.18
			}
		}
		// The columns run down the window, and a line longer than the window
		// scrolls up it, so the window stands as tall as the screen has room
		// for, and no taller.
		return max(w, columns), min(max(h, tallest+2*lyricPad), d.heightCap())
	}
	widest, tall := float32(0), float32(38)
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

// columnHeight is as tall as a column stands: the window's height less the
// room its edges take, or what the lines need when there is no window to
// ask, as in tests.
func (d *deskLyrics) columnHeight(c *ui.Context, shown []lyrics.Line, start, cur int, size float32, weight int, family, transFamily string) float32 {
	if d.win != nil {
		if h := float32(d.win.Bounds().Height) - 2*lyricPad; h > 0 {
			return h
		}
	}
	_, h := d.needed(c, shown, start, cur, size, weight, family, transFamily)
	return max(h-2*lyricPad, size*1.3)
}

// heightCap is as tall as the window may stand: the room the screen has for
// it, or a nominal height where there is no screen to ask, as in tests.
func (d *deskLyrics) heightCap() float32 {
	if d.win != nil {
		if h := float32(mygo.Screen.PrimaryDisplay().WorkArea.Height) - 96; h > 0 {
			return max(h, lyricSizeMax*3)
		}
	}
	return 900
}

// textWidth is how wide a line is set, and lyricExtent how far down a column
// it runs.
func textWidth(c *ui.Context, text, family string, size float32, weight int) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Font: family, Size: size, Weight: weight})
	return w
}

// lyricExtent is how far down a column a line runs: its characters one under
// the next, and its words as wide as they were set.
func lyricExtent(c *ui.Context, text, family string, size float32, weight int) float32 {
	total := float32(0)
	for _, run := range lyricRuns(text) {
		total += runHeight(c, run, family, size, weight)
	}
	return total
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

// runHeight is how far down a column a run takes it: a character as tall as
// it stands, and words as wide as they were set.
func runHeight(c *ui.Context, run lyricRun, family string, size float32, weight int) float32 {
	if run.upright {
		return size * 1.3
	}
	w, _ := c.MeasureText(0, ui.Span{Text: run.text, Font: family, Size: size, Weight: weight})
	if w <= 0 {
		return size * 1.3
	}
	return w
}

// lyricColumn sets a line down one column: its characters upright, one under
// another, and its words turned beside them, as Word sets a line of Chinese
// with Latin in it vertically. The column is as tall as the window and no
// taller, and a line that runs further scrolls up it as it is sung rather
// than breaking off into a column of its own.
func lyricColumn(c *ui.Context, text, family string, size float32, weight int, tint ui.Color, height, offset float32) {
	view := ui.Column(c).Height(height).ClipY().AlignItems(ui.Center).Justify(ui.Start).PassThrough()
	view.Children(func() {
		// inner is as tall as the window and its words start at its top, so that
		// two columns of different lengths stand on the same line.
		inner := ui.Column(c).Height(height).Gap(1).AlignItems(ui.Center).Justify(ui.Start).PassThrough()
		if offset > 0 {
			inner.Transform(ui.Translation(0, -offset))
		}
		inner.Children(func() {
			for _, run := range lyricRuns(text) {
				if run.upright {
					t := ui.Text(c, run.text).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(tint).PassThrough()
					if family != "" {
						t.Font(family)
					}
					continue
				}
				turned(c, run.text, family, size, weight, tint)
			}
		})
	})
}

// lyricScroll is how far up its column a line has run by now: nothing while
// it still fits, and the rest of it in time with the singing once it does
// not.
func lyricScroll(c *ui.Context, l lyrics.Line, pos time.Duration, family string, size float32, weight int, height float32) float32 {
	total := lyricExtent(c, l.Text, family, size, weight)
	if total <= height {
		return 0
	}
	sung := float32(0)
	if l.End > l.At && pos > l.At {
		sung = float32(pos-l.At) / float32(l.End-l.At)
	}
	return min(max(sung, 0), 1) * (total - height)
}

// turned sets a run of words on its side, a quarter turn clockwise, as Word
// turns the Latin in a column of vertical text: the words keep the width
// they were set at, and the room they take is that width down the column.
func turned(c *ui.Context, text, family string, size float32, weight int, tint ui.Color) {
	span := ui.Span{Text: text, Font: family, Size: size, Weight: weight, Color: tint}
	w, h := c.MeasureText(0, span)
	if w <= 0 || h <= 0 {
		return
	}
	// The cell the words turn in: as wide as a line stands tall, as tall as
	// the words are wide. They are laid out beside it and turned about its
	// top right corner, which lays them into it.
	ui.Box(c).Width(h).Height(w).PassThrough().Children(func() {
		t := ui.Text(c, text).FontSize(size).FontWeight(weight).MaxLines(1).Width(w).TextColor(tint)
		if family != "" {
			t.Font(family)
		}
		t.Absolute().Left(h).Top(0).TransformOrigin(0, 0).Transform(ui.Rotation(90)).PassThrough()
	})
}
