package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// deskLyrics is the song's lyrics in a window of their own, over every
// other window: the line being sung lit, the ones after it with it, and the
// translation under each if the listener asks for it. Set across, the
// window is wide and short and two lines show; set down, it is narrow and
// tall and four lines do, each written in a column of its own as Word sets
// Chinese, standing or falling with the script.
//
// Only the lines show while the pointer is elsewhere. Over them, the plate
// they stand on and their controls come back: unlocked, the plate drags the
// window and its edges resize it; locked, the window takes no pointer at
// all, and a pointer resting on it is what brings the plate and the unlock
// back. Where it was left and how large, it opens again.
type deskLyrics struct {
	a   *app
	win *mygo.Window

	looked  time.Time // when the window's place was last looked at
	watched time.Time // when the pointer's place was last looked at
	rested  time.Time // since when the pointer has been resting on it, if it is
	live    bool      // whether a locked window has taken the pointer back
}

// lyricWatch is how often a locked window looks at where the pointer is: it
// takes no pointer to hear it from.
const lyricWatch = 200 * time.Millisecond

// lyricRest is how long the pointer rests on a locked window before the
// window takes it back, so that a pointer passing over is left to pass.
const lyricRest = 400 * time.Millisecond

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

// lyricWindowSize is how large the window stands: wide and short with the
// lines set across, narrow and tall with them set down, and either larger
// with larger type.
func lyricWindowSize(vertical bool, size float64) (w, h float32) {
	size = min(max(size, lyricSizeMin), lyricSizeMax)
	if vertical {
		// Four columns, each with its translation beside it.
		return float32(60 + size*11.5), float32(60 + size*10.5)
	}
	// Two lines, each with its translation.
	return 840, float32(36 + size*5.2)
}

// lyricWindowLines are the lines the window shows, from start, with the one
// at cur among them lit: that one and the one after it set across, that one
// and the three after it set down. Before the first line, the first ones.
func lyricWindowLines(lines []lyrics.Line, cur int, vertical bool) (shown []lyrics.Line, start int) {
	if len(lines) == 0 {
		return nil, 0
	}
	n := 2
	if vertical {
		n = 4
	}
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
	w, h := lyricWindowSize(s.LyricVertical, s.LyricSize)
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         mygo.App.Name() + "桌面歌词",
		Panel:         true,
		Hidden:        true,
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

// apply makes the window as the settings say it stands: as large, where it
// was left, locked or not, and showing or not.
func (d *deskLyrics) apply() {
	if d.win == nil {
		return // as in tests
	}
	s := &d.a.settings
	needW, needH := lyricWindowSize(s.LyricVertical, s.LyricSize)
	// No smaller than the lines need, and as large as it was left.
	w, h := needW, needH
	if s.LyricWidth > int(needW) {
		w = float32(s.LyricWidth)
	}
	if s.LyricHeight > int(needH) {
		h = float32(s.LyricHeight)
	}
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win.SetMinimumSize(int(needW), int(needH))
	d.win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)})
	// Locked, it is left where it is, takes no resize, and takes no
	// pointer: the view asks for the pointer again while it rests on the
	// window, so the unlock can be clicked.
	d.win.SetMovable(!s.LyricLocked)
	d.win.SetResizable(!s.LyricLocked)
	d.win.SetIgnoreMouseEvents(s.LyricLocked)
	// Locked anew or let go, the pointer is the desktop's until it rests here.
	d.live = false
	d.rested = time.Time{}
	d.win.SetAlwaysOnTop(true)
	if s.LyricWindow {
		d.win.ShowInactive()
	} else {
		d.win.Hide()
	}
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

// pointerOver reports whether the pointer is over the window, which a locked
// window cannot be told by itself: it takes no pointer to hear it from.
func (d *deskLyrics) pointerOver() bool {
	if d.win == nil {
		return false
	}
	p := mygo.Screen.CursorScreenPoint()
	b := d.win.Bounds()
	return p.X >= b.X && p.X < b.X+b.Width && p.Y >= b.Y && p.Y < b.Y+b.Height
}

// toggleDeskLyrics shows the lyrics window or hides it, as its own button
// does and the menus.
func (a *app) toggleDeskLyrics() {
	a.settings.LyricWindow = !a.settings.LyricWindow
	a.changed()
}

// lyricGround is the plate the lines stand on: dark enough to hold white
// type over a bright wallpaper, thin enough to see it through.
var lyricGround = ui.RGBA(10, 10, 13, 0.42)

func (d *deskLyrics) view(c *ui.Context) {
	a := d.a
	s := &a.settings
	pl := a.pl
	c.Root().Background(ui.RGBA(0, 0, 0, 0))

	// Where the pointer is: over the window, the plate and the controls show,
	// and the lines stand on it. A locked window takes no pointer, so it
	// looks at where the pointer is instead, and takes the pointer again
	// once the pointer has rested on the lyrics.
	over := c.Root().Hovered()
	if d.win != nil && s.LyricWindow {
		switch {
		case s.LyricLocked:
			if time.Since(d.watched) > lyricWatch {
				d.watched = time.Now()
				switch at := d.pointerOver(); {
				case !at:
					// Passing over, or gone: the pointer is the desktop's.
					d.rested = time.Time{}
					if d.live {
						d.live = false
						d.win.SetIgnoreMouseEvents(true)
						d.win.Invalidate()
					}
				case !d.live:
					if d.rested.IsZero() {
						d.rested = time.Now()
					} else if time.Since(d.rested) >= lyricRest {
						d.live = true
						d.win.SetIgnoreMouseEvents(false)
						d.win.Invalidate()
					}
				}
			}
			over = over || d.live
			c.After(lyricWatch)
		default:
			// The window says nothing of a move or a resize: a look at
			// where it stands now and then keeps where it was left.
			if time.Since(d.looked) > 400*time.Millisecond {
				d.looked = time.Now()
				d.remember()
			}
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

	if over {
		// The root is the plate: absolute children are painted over the lines,
		// so a plate of their own would bury them, and the drag goes to
		// whatever takes the pointer, which the lines must not.
		c.Root().Background(lyricGround).Radius(18)
		// Unlocked, the plate is what the window is dragged by.
		if !s.LyricLocked {
			c.Root().DragWindow()
		}
		// Below the words, on the side the columns leave free.
		ui.Row(c).Absolute().Bottom(6).Left(8).Gap(4).Opacity(0.6).Children(func() {
			button := func(key, label string, on func()) {
				b := ui.ButtonBase(c.Key(key)).Padding(3, 8).Radius(7).FocusRing(false)
				b.Children(func() {
					ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(white)
				})
				if b.Clicked() {
					on()
				}
			}
			if s.LyricLocked {
				button("unlock", "解锁", func() {
					s.LyricLocked = false
					a.changed()
				})
				return
			}
			button("lock", "锁定", func() {
				s.LyricLocked = true
				a.changed()
			})
			button("settings", "设置", func() { a.openSettings() })
			button("off", "关闭", func() { a.toggleDeskLyrics() })
		})
	}

	shown, start := lyricWindowLines(lines, cur, s.LyricVertical)
	switch {
	case len(lines) == 0 && loaded:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "纯音乐，请欣赏").FontSize(size * 0.8).FontWeight(weight).TextColor(idle).PassThrough()
		})
	case len(shown) == 0:
	case s.LyricVertical:
		// Lines stand in columns, the first of them rightmost, as vertical
		// text runs right to left, and each translation beside its line.
		area := ui.Row(c).Fill().Padding(8).Gap(10).Justify(ui.End).AlignItems(ui.Start).PassThrough()
		area.Children(func() {
			for i := len(shown) - 1; i >= 0; i-- {
				l := shown[i]
				on := start+i == cur
				tint := idle
				lineSize := size
				if on {
					tint = played
					lineSize = size * 1.04
				}
				if s.LyricTrans && l.Translation != "" {
					lyricColumns(c, l.Translation, transFamily, size*0.62, 500, trans)
				}
				lyricColumns(c, l.Text, family, lineSize, weight, tint)
			}
		})
	default:
		col := ui.Column(c).Fill().Padding(10, 16).Gap(8).Center().PassThrough()
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

// lyricColumns sets one line down the columns it needs: its characters
// upright, one under another, and its words turned beside them, as Word sets
// a line of Chinese with Latin in it vertically. What a column cannot hold
// runs on into the next column, which stands to its left.
func lyricColumns(c *ui.Context, text string, family string, size float32, weight int, tint ui.Color) {
	chunks := lyricChunks(c, text, family, size, weight)
	for i := len(chunks) - 1; i >= 0; i-- {
		lyricColumn(c, chunks[i], family, size, weight, tint)
	}
}

// lyricChunks splits a line into what each column holds, in reading order.
func lyricChunks(c *ui.Context, text string, family string, size float32, weight int) [][]lyricRun {
	// As much as a column holds, in the room the window gives it.
	room := lyricColumnRoom(size)
	var chunks [][]lyricRun
	var col []lyricRun
	used := float32(0)
	set := func() {
		if len(col) == 0 {
			return
		}
		chunks = append(chunks, col)
		col, used = nil, 0
	}
	for _, run := range lyricRuns(text) {
		h := runHeight(c, run, family, size, weight)
		if used+h > room && len(col) > 0 {
			set()
		}
		col = append(col, run)
		used += h
	}
	set()
	return chunks
}

// lyricColumnRoom is how tall the room a column stands in is.
func lyricColumnRoom(size float32) float32 {
	_, h := lyricWindowSize(true, float64(size))
	return h - 16
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

// lyricColumn sets a stretch of a line down one column.
func lyricColumn(c *ui.Context, runs []lyricRun, family string, size float32, weight int, tint ui.Color) {
	col := ui.Column(c).Gap(1).AlignItems(ui.Center).PassThrough()
	col.Children(func() {
		for _, run := range runs {
			if run.upright {
				ui.Text(c, run.text).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(tint).PassThrough()
				continue
			}
			turned(c, run.text, family, size, weight, tint)
		}
	})
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
