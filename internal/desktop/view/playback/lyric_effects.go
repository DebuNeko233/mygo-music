/*
计算歌词透明度、前奏与间奏时序，并绘制提示点和首行进入动画。
*/

package playback

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
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

type LyricInterlude struct {
	From, To time.Duration
	Intro    bool
}

func LyricInterludeAt(lines []lyrics.Line, cur, next int) LyricInterlude {
	if next < 0 {
		return LyricInterlude{}
	}
	to := lines[next].At
	if cur < 0 {
		if to >= 4*time.Second {
			return LyricInterlude{To: to, Intro: true}
		}
		return LyricInterlude{}
	}
	line := lines[cur]
	// The playback index only includes sung rows. Valid YRC word times
	// fit inside the line, so its end is the boundary for a real gap.
	if line.Timed && line.End > line.At && to-line.End > 4*time.Second {
		return LyricInterlude{From: line.End + time.Second, To: to}
	}
	return LyricInterlude{}
}

const (
	LyricIntroHeight = 42
	LyricIntroSpace  = LyricIntroHeight + TimedLineGap
	LyricIntroExit   = 240 * time.Millisecond
)

type LyricIntroMotion struct {
	Gap       LyricInterlude
	remaining float32
	posed     bool
	anim      ui.Animation
}

// Only wake the view at the start of the exit and at the first cue.
// The poses in between reuse layout and keep the hit targets with the text.
func (a *View) lyricIntro(c *ui.Context, scroll ui.Element, gap LyricInterlude) LyricIntroMotion {
	return PrepareLyricIntro(c, scroll, gap, a.Player().Position(), a.Player().Playing())
}

func PrepareLyricIntro(c *ui.Context, scroll ui.Element, gap LyricInterlude, pos time.Duration, playing bool) LyricIntroMotion {
	previous := ui.Local(scroll, "lyric-intro", func() time.Duration { return 0 })
	was := *previous
	*previous = 0
	if !gap.Intro {
		return LyricIntroMotion{}
	}
	left := gap.To - pos
	if left <= 0 {
		return LyricIntroMotion{}
	}
	m := LyricIntroMotion{Gap: gap, remaining: 1}
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
	*previous = gap.To
	m.posed = true
	m.anim = ui.Animation{Duration: time.Nanosecond}
	if left > LyricIntroExit {
		c.After(left - LyricIntroExit)
		return m
	}
	c.After(left)
	if was != gap.To {
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
	t := components.Clamp01(float32(left) / float32(LyricIntroExit))
	return t * t * (3 - 2*t)
}

func (m LyricIntroMotion) Move(e ui.Element, distance float32) {
	y := m.remaining * distance
	if m.posed {
		e.AnimatePose(ui.Pose{Y: y}, m.anim)
	} else {
		e.Translate(0, y)
	}
}

func LyricIntroDots(c *ui.Context) (slot, dots ui.Element) {
	slot = ui.Box(c.Key("intro")).Height(0).Shrink(0).Children(func() {
		dots = ui.Box(c.Key("dots")).Absolute().Left(0).Top(0).Size(70, LyricIntroSpace)
	})
	return
}

func (a *View) IntroDots(c *ui.Context, slot, dots ui.Element, m LyricIntroMotion) func() float32 {
	m.Move(dots, -LyricIntroSpace)
	remaining := func() float32 {
		// Fade and shrink the dots and their clear band using the same
		// live pose that closes the space above the first lyric.
		x, y := dots.LocalToWindow(0, 0)
		if _, dy, ok := slot.WindowToLocal(x, y); ok {
			return components.Clamp01(-dy / LyricIntroSpace)
		}
		return m.remaining
	}
	a.paintLyricDots(c, dots, m.Gap, remaining)
	return remaining
}

func (a *View) paintLyricInterlude(c *ui.Context, box ui.Element, gap LyricInterlude) {
	a.paintLyricDots(c, box, gap, nil)
}

func (a *View) paintLyricDots(c *ui.Context, box ui.Element, gap LyricInterlude, remaining func() float32) {
	reduceMotion := c.Preferences().ReduceMotion
	box.PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		opacity := float32(1)
		if remaining != nil {
			opacity = remaining()
			r.H = LyricIntroHeight * opacity
		}
		size := min(float32(10), r.H)
		if size <= 0 {
			return
		}
		progress := float32(0)
		if !reduceMotion {
			pos := a.Player().Position()
			progress = components.Clamp01(float32(pos-gap.From) / float32(gap.To-gap.From))
			if a.Player().Playing() && pos < gap.To && (remaining == nil || pos < gap.To-LyricIntroExit) {
				p.After(min(80*time.Millisecond, gap.To-pos))
			}
		}
		for i := range 3 {
			lit := components.Clamp01(progress*3 - float32(i))
			p.Fill(ui.Rect{X: r.X + timedLineInsetX + float32(i*15), Y: r.Y + (r.H-size)/2, W: size, H: size}, White.Alpha(opacity*(0.32+0.58*lit)), size/2)
		}
	})
}
