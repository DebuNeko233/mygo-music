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
// window is wide and short and two lines show; stacked, it is narrow and
// tall and four do. It is dragged by its lines, unless it is locked, and it
// keeps where it was left.
type deskLyrics struct {
	a   *app
	win *mygo.Window

	looked time.Time // when the window's place was last looked at
}

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
// lines set across, narrow and tall with them stacked, and either larger
// with larger type.
func lyricWindowSize(vertical bool, size float64) (w, h float32) {
	size = min(max(size, lyricSizeMin), lyricSizeMax)
	if vertical {
		// Four lines, each with its translation under it.
		return 340, float32(60 + size*7.6)
	}
	// Two lines, each with its translation.
	return 840, float32(36 + size*5.2)
}

// lyricWindowLines are the lines the window shows, from start, with the one
// at cur among them lit: that one and the one after it set across, that one
// and the three after it stacked. Before the first line, the first ones.
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
		DisableResize: true,
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
// was left, locked in place or not, and showing or not.
func (d *deskLyrics) apply() {
	if d.win == nil {
		return // as in tests
	}
	s := &d.a.settings
	w, h := lyricWindowSize(s.LyricVertical, s.LyricSize)
	x, y := lyricWindowPlace(s.LyricX, s.LyricY, w, h, mygo.Screen.PrimaryDisplay().WorkArea)
	d.win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: int(w), Height: int(h)})
	d.win.SetMovable(!s.LyricLocked)
	d.win.SetAlwaysOnTop(true)
	if s.LyricWindow {
		d.win.ShowInactive()
	} else {
		d.win.Hide()
	}
	d.win.Invalidate()
}

// remember keeps where the window stands now, for the next time the app
// opens, and for the window to be found again on a screen that changed.
func (d *deskLyrics) remember() {
	s := &d.a.settings
	x, y := d.win.Position()
	if x != s.LyricX || y != s.LyricY {
		s.LyricX, s.LyricY = x, y
		s.save()
	}
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
	tr := pl.Current()
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
	c.Root().Background(ui.RGBA(0, 0, 0, 0))

	// The window says nothing of a move: while it may be dragged, a look at
	// where it stands now and then keeps where it is left.
	if d.win != nil && !s.LyricLocked && time.Since(d.looked) > 400*time.Millisecond {
		d.looked = time.Now()
		d.remember()
	}
	// Nothing to say before anything played.
	if tr == nil {
		return
	}

	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(18).Background(lyricGround)

	col := ui.Column(c).Fill().Padding(10, 16).Gap(8)
	if !s.LyricVertical {
		col.Center()
	} else {
		col.AlignItems(ui.Start)
	}
	// Set across, the lines are the handle the window is dragged by.
	if !s.LyricLocked {
		col.DragWindow()
	}
	col.Children(func() {
		if len(lines) == 0 {
			if loaded {
				ui.Text(c, "纯音乐，请欣赏").FontSize(size * 0.8).FontWeight(weight).TextColor(idle)
			}
			return
		}
		shown, start := lyricWindowLines(lines, cur, s.LyricVertical)
		for i, l := range shown {
			on := start+i == cur
			tint := idle
			if on {
				tint = played
			}
			line := ui.Column(c).Gap(4).AlignItems(ui.Start)
			if !s.LyricVertical {
				line.AlignItems(ui.Center)
			}
			line.Children(func() {
				text := ui.Text(c, l.Text).FontSize(size).FontWeight(weight).MaxLines(1).TextColor(tint)
				if on {
					text.FontSize(size * 1.04)
				}
				if family != "" {
					text.Font(family)
				}
				if s.LyricTrans && l.Translation != "" {
					line := ui.Text(c, l.Translation).FontSize(size * 0.62).FontWeight(500).MaxLines(1).TextColor(trans)
					if transFamily != "" {
						line.Font(transFamily)
					}
				}
			})
		}
	})

	// The controls show over the lines, and out of the way while locked.
	if !s.LyricLocked || c.Root().Hovered() {
		ui.Row(c).Absolute().Top(4).Right(6).Gap(4).Opacity(0.55).Children(func() {
			lock := ui.ButtonBase(c.Key("lock")).Padding(3, 7).Radius(7).FocusRing(false)
			lock.Children(func() {
				label := "锁定"
				if s.LyricLocked {
					label = "解锁"
				}
				ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(white)
			})
			if lock.Clicked() {
				s.LyricLocked = !s.LyricLocked
				a.changed()
			}
			open := ui.ButtonBase(c.Key("settings")).Padding(3, 7).Radius(7).FocusRing(false)
			open.Children(func() {
				ui.Text(c, "设置").FontSize(11).FontWeight(600).TextColor(white)
			})
			if open.Clicked() {
				a.openSettings()
			}
			off := ui.ButtonBase(c.Key("off")).Padding(3, 7).Radius(7).FocusRing(false)
			off.Children(func() {
				ui.Text(c, "关闭").FontSize(11).FontWeight(600).TextColor(white)
			})
			if off.Clicked() {
				a.toggleDeskLyrics()
			}
		})
	}

	// Repaint as the next line begins, and often enough that the next song
	// shows, or the end of a drag is kept.
	wait := 500 * time.Millisecond
	if pl.Playing() && cur >= 0 && cur+1 < len(lines) {
		wait = min(wait, lines[cur+1].At-pos+20*time.Millisecond)
	}
	if pl.Playing() || !s.LyricLocked {
		c.After(max(wait, 20*time.Millisecond))
	}
}
