package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// Past lines recede faster than upcoming lines during automatic follow.
func lyricLineOpacity(relative int, browsing bool) float32 {
	if browsing {
		return 0.64
	}
	if relative < 0 {
		return 0.30 - float32(min(-relative, 4))*0.035
	}
	return 0.44 - float32(min(relative-1, 4))*0.065
}

type lyricInterlude struct {
	from, to time.Duration
	intro    bool
}

func lyricInterludeAt(lines []lyrics.Line, cur, next int) lyricInterlude {
	if next < 0 {
		return lyricInterlude{}
	}
	to := lines[next].At
	if cur < 0 {
		if to >= 4*time.Second {
			return lyricInterlude{to: to, intro: true}
		}
		return lyricInterlude{}
	}
	line := lines[cur]
	// The playback index only includes sung rows. Valid YRC word times
	// fit inside the line, so its end is the boundary for a real gap.
	if line.Timed && line.End > line.At && to-line.End > 4*time.Second {
		return lyricInterlude{from: line.End + time.Second, to: to}
	}
	return lyricInterlude{}
}

const (
	lyricIntroHeight = 42
	lyricIntroSpace  = lyricIntroHeight + timedLineGap
	lyricIntroExit   = 240 * time.Millisecond
)

type lyricIntroMotion struct {
	gap       lyricInterlude
	remaining float32
	posed     bool
	anim      ui.Animation
}

// Only wake the view at the start of the exit and at the first cue.
// The poses in between reuse layout and keep the hit targets with the text.
func (a *app) lyricIntro(c *ui.Context, scroll ui.Element, gap lyricInterlude) lyricIntroMotion {
	return prepareLyricIntro(c, scroll, gap, a.pl.Position(), a.pl.Playing())
}

func prepareLyricIntro(c *ui.Context, scroll ui.Element, gap lyricInterlude, pos time.Duration, playing bool) lyricIntroMotion {
	previous := ui.Local(scroll, "lyric-intro", func() time.Duration { return 0 })
	was := *previous
	*previous = 0
	if !gap.intro {
		return lyricIntroMotion{}
	}
	left := gap.to - pos
	if left <= 0 {
		return lyricIntroMotion{}
	}
	m := lyricIntroMotion{gap: gap, remaining: 1}
	reduceMotion := c.Preferences().ReduceMotion
	if !reduceMotion {
		m.remaining = lyricIntroRemaining(left)
	}
	if !playing {
		return m
	}
	if reduceMotion {
		c.After(left)
		return m
	}
	*previous = gap.to
	m.posed = true
	m.anim = ui.Animation{Duration: time.Nanosecond}
	if left > lyricIntroExit {
		c.After(left - lyricIntroExit)
		return m
	}
	c.After(left)
	if was != gap.to {
		// Opening or seeking into the exit still starts from its current pose.
		c.AnimationFrame()
		return m
	}
	remaining := m.remaining
	m.remaining = 0
	m.anim = ui.Animation{Duration: left, Ease: func(t float32) float32 {
		return 1 - lyricIntroRemaining(time.Duration(float64(left)*float64(1-t)))/remaining
	}}
	return m
}

func lyricIntroRemaining(left time.Duration) float32 {
	t := clamp01(float32(left) / float32(lyricIntroExit))
	return t * t * (3 - 2*t)
}

func (m lyricIntroMotion) move(e ui.Element, distance float32) {
	y := m.remaining * distance
	if m.posed {
		e.AnimatePose(ui.Pose{Y: y}, m.anim)
	} else {
		e.Translate(0, y)
	}
}

func lyricIntroDots(c *ui.Context) (slot, dots ui.Element) {
	slot = ui.Box(c.Key("intro")).Height(0).Shrink(0).Children(func() {
		dots = ui.Box(c.Key("dots")).Absolute().Left(0).Top(0).Size(70, lyricIntroSpace)
	})
	return
}

func (a *app) introDots(c *ui.Context, slot, dots ui.Element, m lyricIntroMotion) func() float32 {
	m.move(dots, -lyricIntroSpace)
	remaining := func() float32 {
		// Fade and shrink the dots and their clear band using the same
		// live pose that closes the space above the first lyric.
		x, y := dots.LocalToWindow(0, 0)
		if _, dy, ok := slot.WindowToLocal(x, y); ok {
			return clamp01(-dy / lyricIntroSpace)
		}
		return m.remaining
	}
	a.paintLyricDots(c, dots, m.gap, remaining)
	return remaining
}

func (a *app) paintLyricInterlude(c *ui.Context, box ui.Element, gap lyricInterlude) {
	a.paintLyricDots(c, box, gap, nil)
}

func (a *app) paintLyricDots(c *ui.Context, box ui.Element, gap lyricInterlude, remaining func() float32) {
	reduceMotion := c.Preferences().ReduceMotion
	box.PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		opacity := float32(1)
		if remaining != nil {
			opacity = remaining()
			r.H = lyricIntroHeight * opacity
		}
		size := min(float32(10), r.H)
		if size <= 0 {
			return
		}
		progress := float32(0)
		if !reduceMotion {
			pos := a.pl.Position()
			progress = clamp01(float32(pos-gap.from) / float32(gap.to-gap.from))
			if a.pl.Playing() && pos < gap.to && (remaining == nil || pos < gap.to-lyricIntroExit) {
				p.After(min(80*time.Millisecond, gap.to-pos))
			}
		}
		for i := range 3 {
			lit := clamp01(progress*3 - float32(i))
			p.Fill(ui.Rect{X: r.X + timedLineInsetX + float32(i*15), Y: r.Y + (r.H-size)/2, W: size, H: size}, white.Alpha(opacity*(0.32+0.58*lit)), size/2)
		}
	})
}
