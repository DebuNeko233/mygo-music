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
	lyricIntroExit   = 240 * time.Millisecond
	lyricIntroTick   = time.Second / 60
)

func (a *app) lyricIntro(c *ui.Context, gap lyricInterlude) (height, opacity float32) {
	if !gap.intro {
		return 0, 0
	}
	height, opacity, next := lyricIntroPose(gap.to-a.pl.Position(), c.Preferences().ReduceMotion)
	if next > 0 && a.pl.Playing() {
		c.After(next)
	}
	return height, opacity
}

// Fade the dots while closing their space before the first line lights up.
// Use playback time so pausing holds the pose and a seek skips the transition.
func lyricIntroPose(left time.Duration, reduceMotion bool) (height, opacity float32, next time.Duration) {
	switch {
	case left <= 0:
		return 0, 0, 0
	case reduceMotion:
		return lyricIntroHeight + timedLineGap, 1, 0
	case left > lyricIntroExit:
		return lyricIntroHeight + timedLineGap, 1, left - lyricIntroExit
	default:
		t := float32(left) / float32(lyricIntroExit)
		remaining := t * t * (3 - 2*t)
		return (lyricIntroHeight + timedLineGap) * remaining, remaining, min(left, lyricIntroTick)
	}
}

func (a *app) paintLyricInterlude(c *ui.Context, box ui.Element, gap lyricInterlude, opacity float32) {
	reduceMotion := c.Preferences().ReduceMotion
	box.PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		if gap.intro {
			r.H *= float32(lyricIntroHeight) / (lyricIntroHeight + timedLineGap)
		}
		size := min(float32(10), r.H)
		if size <= 0 {
			return
		}
		progress := float32(0)
		if !reduceMotion {
			pos := a.pl.Position()
			progress = clamp01(float32(pos-gap.from) / float32(gap.to-gap.from))
			if a.pl.Playing() && pos < gap.to {
				p.After(min(80*time.Millisecond, gap.to-pos))
			}
		}
		for i := range 3 {
			lit := clamp01(progress*3 - float32(i))
			p.Fill(ui.Rect{X: r.X + timedLineInsetX + float32(i*15), Y: r.Y + (r.H-size)/2, W: size, H: size}, white.Alpha(opacity*(0.32+0.58*lit)), size/2)
		}
	})
}
