package playback

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Notch is the quick player around the camera housing at the top of the
// screen: black like the housing, it grows out of it as music plays (the
// cover on the left, the music's levels on the right) and opens into the
// player under the pointer. On a display without a notch it hangs from the
// top of the screen all the same.
type Notch struct {
	Lifetime                            components.NativeLifetime
	PollTimer, greetingTimer, hideTimer *time.Timer
	A                                   *View
	Win                                 *mygo.Window

	W0, H0     float32 // the housing's size
	Real       bool    // the display has one: else the notch hides until wanted
	WinW, WinH float32
	ScreenX    float32 // the window's left on the screen
	ScreenY    float32

	Open       bool // the pointer is over it: it shows the player
	Hello      bool // it shows itself, turned off, with a way to keep it
	EnteredAt  time.Time
	LastInside time.Time
	passing    bool   // clicks pass through to what is under the window
	Watched    bool   // the pointer's moves poll
	Unwatch    func() // stops them polling, nil while they don't
	active     bool   // the app is active, when they go unseen over the menu bar
	Polling    bool   // a poll is due

	bars  components.BarState
	scrub scrubState
}

const (
	NotchOpenW  = 468
	notchIdleW  = 330
	notchHelloW = 340
)

func NewNotch(a *View) *Notch {
	n := &Notch{A: a}
	n.measure()
	n.WinW, n.WinH = NotchOpenW+60, 240
	n.place()
	n.Win = mygo.NewWindow(mygo.WindowOptions{
		Title:         mygo.App.Name() + "迷你播放器",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		DisableShadow: true,
		DisableResize: true,
		X:             int(n.ScreenX),
		Y:             int(n.ScreenY),
		Width:         int(n.WinW),
		Height:        int(n.WinH),
		Content:       ui.View(n.View),
	})
	// Escape and the like close a panel: this one stays, until the app quits.
	n.Win.OnClose(func(e *mygo.CloseEvent) {
		if !a.Quitting() {
			e.PreventDefault()
		}
	})
	raiseAboveMenuBar(n.Win.NativeHandle())
	n.Win.SetIgnoreMouseEvents(true)
	n.passing = true
	if a.Config.Notch {
		n.Win.ShowInactive()
		pinToTop(n.Win.NativeHandle(), float64(n.WinW), float64(n.WinH))
	}
	n.Lifetime.Listen(n.Win.OnClosed(n.Close))
	n.Lifetime.Listen(mygo.Screen.OnDisplaysChanged(func() {
		if n.Lifetime.Closed {
			return
		}
		n.measure()
		n.place()
		pinToTop(n.Win.NativeHandle(), float64(n.WinW), float64(n.WinH))
	}))
	// The pointer's moves wake the notch: it polls only while one may
	// soon open or close it, and costs nothing at rest.
	n.active = true
	n.Lifetime.Listen(mygo.App.OnDidBecomeActive(func() {
		if !n.Lifetime.Closed {
			n.active = true
			n.Poll()
		}
	}))
	n.Lifetime.Listen(mygo.App.OnDidResignActive(func() { n.active = false }))
	n.watch()
	n.Poll()
	return n
}

func (n *Notch) Close() {
	if n.Lifetime.Closed {
		return
	}
	n.Lifetime.Close()
	for _, timer := range []*time.Timer{n.PollTimer, n.greetingTimer, n.hideTimer} {
		if timer != nil {
			timer.Stop()
		}
	}
	if n.Unwatch != nil {
		n.Unwatch()
		n.Unwatch = nil
	}
	n.Polling, n.Watched = false, false
	n.Win = nil
}

// watch follows the pointer's moves while the notch shows, turned on or
// greeting. Otherwise it lets them be: each, anywhere on the screen, would
// wake the app for nothing.
func (n *Notch) watch() {
	if n.Lifetime.Closed {
		return
	}
	on := n.A.Config.Notch || n.Hello
	switch {
	case on && n.Unwatch == nil:
		n.Unwatch, n.Watched = watchPointer(n.Poll)
	case !on && n.Unwatch != nil:
		n.Unwatch()
		n.Unwatch, n.Watched = nil, false
	}
}

// measure finds the housing of the display with the menu bar.
func (n *Notch) measure() {
	n.W0, n.H0, n.Real = 200, 32, false
	if w, h, ok := notchSize(); ok {
		n.W0, n.H0, n.Real = float32(w), float32(h), true
	} else if h := menuBarHeight(); h > 0 {
		// A notch of its own, as high as the menu bar it hangs over.
		n.H0 = float32(h)
	}
	if n.Win != nil {
		n.Win.Invalidate()
	}
}

// place centers the window at the top of the display with the menu bar,
// where the pointer finds it.
func (n *Notch) place() {
	d := mygo.Screen.PrimaryDisplay()
	n.ScreenX = float32(d.Bounds.X) + (float32(d.Bounds.Width)-n.WinW)/2
	n.ScreenY = float32(d.Bounds.Y)
}

// SetEnabled shows the notch player, or hides it for good, as the
// settings say.
func (n *Notch) SetEnabled(on bool) {
	if n.Win == nil {
		return // as in tests
	}
	if on == n.Win.IsVisible() {
		n.Win.Invalidate()
		n.watch()
		return
	}
	if on {
		n.Win.ShowInactive()
		pinToTop(n.Win.NativeHandle(), float64(n.WinW), float64(n.WinH))
		n.watch()
		n.Poll()
		return
	}
	n.Open, n.Hello = false, false
	n.Win.Hide()
	n.watch()
}

// Greet shows the notch once, as the first song plays, to a listener who
// never turned it on: it grows out of the housing, or the top of a screen
// without one, with the song and a way to keep it, and goes back in. The
// notch is a window of the Mac's alone, so this is only reached there.
func (n *Notch) Greet() {
	s := n.A.Config
	if s.Notch || s.NotchGreeted {
		return
	}
	s.NotchGreeted = true
	s.SaveSoon()
	n.Hello = true
	if n.Win == nil {
		return // as in tests
	}
	n.Win.ShowInactive()
	pinToTop(n.Win.NativeHandle(), float64(n.WinW), float64(n.WinH))
	n.watch()
	n.Poll()
	n.byeSoon(7 * time.Second)
}

// byeSoon ends the greeting after d, unless the pointer is on it then.
func (n *Notch) byeSoon(d time.Duration) {
	if n.greetingTimer != nil {
		n.greetingTimer.Stop()
	}
	n.greetingTimer = time.AfterFunc(d, func() {
		n.A.Dispatch(func() {
			if n.Lifetime.Closed {
				return
			}
			switch {
			case !n.Hello:
			case n.Open || !n.passing:
				n.byeSoon(2 * time.Second)
			default:
				n.bye()
			}
		}, false)
	})
}

// bye ends the greeting: the notch goes back into the housing and, still
// turned off, hides there.
func (n *Notch) bye() {
	n.Hello = false
	n.Win.Invalidate()
	if n.A.Config.Notch {
		return
	}
	n.Open = false
	n.passing = true
	n.Win.SetIgnoreMouseEvents(true)
	if n.hideTimer != nil {
		n.hideTimer.Stop()
	}
	n.hideTimer = time.AfterFunc(time.Second, func() {
		n.A.Dispatch(func() {
			if n.Lifetime.Closed {
				return
			}
			if !n.Hello && !n.A.Config.Notch {
				n.Win.Hide()
				n.watch()
			}
		}, false)
	})
}

// keep turns the notch player on, from its greeting.
func (n *Notch) keep() {
	n.Hello = false
	n.A.Config.Notch = true
	n.A.Config.SaveSoon()
	n.SetEnabled(true)
}

// target returns the size the shape goes to.
func (n *Notch) target() (w, h float32) {
	tr := n.A.Player().Current()
	s := n.A.Config
	switch {
	case n.Open && tr == nil:
		return notchIdleW, n.H0 + 72
	case n.Open:
		return NotchOpenW, n.H0 + 178
	case n.Hello && tr != nil:
		return notchHelloW, n.H0 + 58
	case !n.Real && !s.NotchVirtual:
		// Over a menu bar without a notch, it would hide the menus and
		// icons there: nothing shows until the pointer asks for it.
		return 0, 0
	case tr != nil && s.NotchActivity && s.Notch:
		// Wings just wide enough for the cover and the levels, so they
		// hide no more of the menu bar than they must.
		return n.W0 + 2*max(0, n.H0-12) + 20, n.H0
	default:
		return n.W0, n.H0
	}
}

// Poll follows the pointer, which the window cannot always see: it lets
// clicks through where it draws nothing, and opens while over it.
func (n *Notch) Poll() {
	if n.Lifetime.Closed {
		return
	}
	if !n.A.Config.Notch && !n.Hello {
		return
	}
	pt := n.A.CursorPoint()
	w, h := n.target()
	x := n.ScreenX + (n.WinW-w)/2
	px, py := float32(pt.X), float32(pt.Y)
	slack := float32(6)
	inside := px >= x-slack && px <= x+w+slack && py >= n.ScreenY-slack && py <= n.ScreenY+h+slack
	if !n.Real && !n.A.Config.NotchVirtual && !n.Open && !n.Hello {
		// Without a notch, the pointer pressed against the top edge of
		// the screen, about its middle, opens it.
		cx := n.ScreenX + n.WinW/2
		inside = px >= cx-n.W0/2 && px <= cx+n.W0/2 && py <= n.ScreenY+1
	}
	now := time.Now()
	changed := false
	if inside {
		if n.LastInside.IsZero() || now.Sub(n.LastInside) > 100*time.Millisecond {
			n.EnteredAt = now
		}
		n.LastInside = now
		// A pointer passing by to the menu bar does not open it: one that
		// rests there does. While it greets it stays as it is, the pointer
		// on its button.
		if !n.Open && !n.Hello && now.Sub(n.EnteredAt) >= 300*time.Millisecond {
			n.Open, changed = true, true
		}
	} else if n.Open && now.Sub(n.LastInside) > 300*time.Millisecond {
		n.Open, changed = false, true
	}
	pass := !inside
	if n.Win == nil {
		n.passing = pass
		return // as in tests
	}
	if pass != n.passing {
		n.passing = pass
		n.Win.SetIgnoreMouseEvents(pass)
	}
	if changed {
		n.Win.Invalidate()
	}
	n.pollAgain(inside)
}

// pollAgain polls again soon while the pointer is over the notch or it is
// open, for the times it rests and leaves; and, more slowly, while the app
// is active, when the pointer's moves outside its windows go unseen, or
// when they are never seen. Otherwise the next move polls.
func (n *Notch) pollAgain(inside bool) {
	var d time.Duration
	switch {
	case inside || n.Open:
		d = 33 * time.Millisecond
	case n.active || !n.Watched:
		d = 100 * time.Millisecond
	}
	if d == 0 || n.Polling {
		return
	}
	n.Polling = true
	n.PollTimer = time.AfterFunc(d, func() {
		n.A.Dispatch(func() {
			if n.Lifetime.Closed {
				return
			}
			n.Polling = false
			n.Poll()
		}, false)
	})
}

// shape returns the outline of the housing grown to w×h: straight sides
// that flare into the top edge, and a rounded foot.
func shape(cx, w, h, flare, foot float32) *ui.Path {
	const k = 0.5523 // a quarter circle's control points
	l, r := cx-w/2, cx+w/2
	p := &ui.Path{}
	p.MoveTo(l-flare, 0)
	p.CubeTo(l-flare+flare*k, 0, l, flare-flare*k, l, flare)
	p.LineTo(l, h-foot)
	p.CubeTo(l, h-foot+foot*k, l+foot-foot*k, h, l+foot, h)
	p.LineTo(r-foot, h)
	p.CubeTo(r-foot+foot*k, h, r, h-foot+foot*k, r, h-foot)
	p.LineTo(r, flare)
	p.CubeTo(r, flare-flare*k, r+flare-flare*k, 0, r+flare, 0)
	p.Close()
	return p
}

func (n *Notch) View(c *ui.Context) {
	a := n.A
	if a.Config.UIFont != "" {
		t := a.Theme(c.Theme())
		c.SetTheme(&t)
	}
	pl := a.Player()
	tr := pl.Current()
	c.Root().Background(ui.Transparent)

	tw, th := n.target()
	spring := ui.Animation{Spring: ui.SpringOptions{Duration: 520 * time.Millisecond, Bounce: 0.28}}
	if !n.Open {
		spring.Spring.Bounce = 0.12
	}
	stage := ui.Box(c).Fill()
	// A spring overshoots: never below nothing.
	w := max(0, stage.AnimateTo("w", tw, spring))
	h := max(0, stage.AnimateTo("h", th, spring))
	opened := float32(0)
	if n.Open {
		opened = 1
	}
	open := stage.AnimateTo("open", opened, spring)
	hello := stage.AnimateTo("hello", components.BoolFloat(n.Hello), spring)
	foot := 10 + 18*open
	flare := 6 + 8*open
	cx := n.WinW / 2

	stage.Draw(func(p *ui.Painter, r ui.Rect) {
		if w < 1 || h < 1 {
			return
		}
		path := shape(r.X+cx, w, h, flare, foot)
		if open > 0.05 {
			p.Shadow(ui.Rect{X: r.X + cx - w/2, Y: r.Y + h - 40, W: w, H: 40}, foot, 0, 14, 30, -6, ui.RGBA(0, 0, 0, 0.45*open))
		}
		p.FillPath(path, ui.RGB(0, 0, 0))
	})
	stage.Children(func() {
		body := ui.Box(c).Absolute().Top(0).Left(cx-w/2).Size(w, h).Radius(0, 0, foot, foot).Clip().TextColor(White)
		body.Children(func() {
			if tr == nil {
				n.idle(c, open, (w-notchIdleW)/2)
				return
			}
			if a.Config.NotchActivity || !a.Config.Notch {
				n.compact(c, tr, 1-open)
			}
			n.helloRow(c, hello*(1-open), (w-notchHelloW)/2)
			if open > 0.02 {
				n.expanded(c, tr, open, (w-NotchOpenW)/2)
			}
		})
	})
	a.SyncWindows()
}

// compact shows the cover on the housing's left and the levels on its right.
func (n *Notch) compact(c *ui.Context, tr *netease.Track, shown float32) {
	if shown < 0.02 {
		return
	}
	art := n.A.TrackArtwork(tr, 96, false)
	s := n.H0 - 12
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(n.H0).PaddingX(7).AlignItems(ui.Center).Opacity(shown).Children(func() {
		cov := ui.Box(c).Size(s, s).Radius(s * 0.28)
		in := components.ArtIn(cov, components.Bitmap(art))
		cov.Children(func() {
			components.CoverPlaceholder(c, s, s*0.28, 1-in, White.Alpha(0.12), White.Alpha(0.35))
			components.ArtImage(c, components.Bitmap(art), s*0.28, in)
		})
		ui.Spacer(c)
		n.levels(c, tr, 16, s*0.7, 4)
	})
}

// levels draws the music's levels in the cover's color.
func (n *Notch) levels(c *ui.Context, tr *netease.Track, w, h float32, count int) {
	a := n.A
	color := ui.RGB(255, 255, 255)
	if art := a.TrackArtwork(tr, 96, false); art != nil && !art.ImageFailed() {
		color = art.ImageTint().Mix(White, 0.35)
	}
	ui.Box(c).Size(w, h).Draw(func(p *ui.Painter, r ui.Rect) {
		lv, live := a.Player().Levels()
		n.bars.Step(p.Now(), lv, live)
		components.DrawBars(p, r, &n.bars, count, color, 2.5)
		if live || n.bars.Group(0, player.Bands) > 0.01 {
			p.AnimationFrame()
		}
	})
}

func (n *Notch) expanded(c *ui.Context, tr *netease.Track, open, left float32) {
	a := n.A
	pl := a.Player()
	art := a.TrackArtwork(tr, 200, false)
	a.tickTimes(c)
	col := ui.Column(c).Absolute().Top(n.H0+6).Left(left).Width(NotchOpenW).Padding(8, 22, 16, 22).Gap(12).Opacity(max(0, open*1.4-0.4))
	col.Translate(0, (1-open)*-10)
	col.Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			cov := ui.ButtonBase(c.Key("cover")).Size(60, 60).Radius(12).FocusRing(false).Label("打开正在播放")
			lift := ui.Pose{}
			if cov.Hovered() {
				lift.Scale = 1.05
			}
			cov.AnimatePose(lift, ui.Snappy)
			in := components.ArtIn(cov, components.Bitmap(art))
			cov.Children(func() {
				components.CoverPlaceholder(c, 60, 12, 1-in, White.Alpha(0.12), White.Alpha(0.35))
				components.ArtImage(c, components.Bitmap(art), 12, in)
			})
			if cov.Clicked() {
				a.Playback.Open = true
				a.ShowMain()
				a.Window().Invalidate()
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, tr.Name).FontSize(15).FontWeight(700).SingleLine()
				if pl.Buffering() {
					ui.Text(c, "正在缓冲…").FontSize(12.5).TextColor(White.Alpha(0.55)).SingleLine()
				} else {
					a.ArtistLinks(c, tr, White.Alpha(0.55), White, false).FontSize(12.5).SingleLine()
				}
			})
			a.LikeButton(c.Key("like"), tr, 17, White.Alpha(0.6))
			n.levels(c, tr, 26, 22, 5)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, components.Clock(pl.Position())).FontSize(10.5).FontFeatures("tnum").TextColor(White.Alpha(0.5)).Width(30)
			a.scrubber(c.Key("notch-scrub"), &n.scrub, White.Alpha(0.18), White.Alpha(0.9), 14, ui.Color{}).Grow(1)
			ui.Text(c, "-"+components.Clock(pl.Duration()-pl.Position())).FontSize(10.5).FontFeatures("tnum").TextColor(White.Alpha(0.5)).Width(34).TextAlign(ui.End)
		})
		ui.Box(c).Margin(-6, 0, 0, 0).Children(func() {
			a.Transport(c, White, White.Alpha(0.4), White, 20)
		})
	})
}

// helloRow says, under the housing, what the notch is, while it greets.
func (n *Notch) helloRow(c *ui.Context, shown, left float32) {
	if shown < 0.02 {
		return
	}
	a := n.A
	row := ui.Row(c).Absolute().Top(n.H0+2).Left(left).Width(notchHelloW).Padding(6, 14, 14, 20).Gap(12).AlignItems(ui.Center).
		Opacity(max(0, shown*1.4-0.4))
	row.Translate(0, (1-shown)*-8)
	row.Children(func() {
		title, hint := "刘海里也能控制播放", "指针停在这里，即可暂停、切歌"
		if !n.Real {
			title, hint = "屏幕顶部也能控制播放", "指针移到顶部中央，即可暂停、切歌"
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13.5).FontWeight(700).SingleLine()
			ui.Text(c, hint).FontSize(11.5).TextColor(White.Alpha(0.55)).SingleLine()
		})
		b := ui.ButtonBase(c.Key("keep")).Padding(6, 14).Radius(999).Background(a.palette().Accent).FocusRing(false).Label("开启刘海播放器")
		b.Children(func() { ui.Text(c, "开启").FontSize(12.5).FontWeight(600) })
		if b.Clicked() {
			n.keep()
		}
	})
}

// idle is the notch with nothing to play: it opens on a way to the app.
func (n *Notch) idle(c *ui.Context, open, left float32) {
	if open < 0.02 {
		return
	}
	a := n.A
	ui.Row(c).Absolute().Top(n.H0+4).Left(left).Width(notchIdleW).Padding(10, 20, 14, 20).Gap(12).AlignItems(ui.Center).
		Opacity(max(0, open*1.4-0.4)).Children(func() {
		ui.Box(c).Size(36, 36).Radius(10).Background(a.palette().Accent).Center().Children(func() {
			ui.Icon(c, components.IconNote).Size(18, 18).TextColor(White)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, mygo.App.Name()).FontSize(14).FontWeight(700)
			ui.Text(c, "未在播放").FontSize(12).TextColor(White.Alpha(0.5))
		})
		b := ui.ButtonBase(c.Key("open")).Padding(6, 14).Radius(999).Background(White.Alpha(0.14)).FocusRing(false)
		b.Children(func() { ui.Text(c, "打开").FontSize(12.5).FontWeight(600) })
		if b.Clicked() {
			a.ShowMain()
		}
	})
}

type PlayerSig struct {
	Track      int64
	Playing    bool
	Loading    bool
	Buffering  bool
	Shuffle    bool
	Repeat     player.RepeatMode
	Volume     float64
	NowPlaying bool
	Err        string
}
