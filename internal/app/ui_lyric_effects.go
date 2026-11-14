package app

import (
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

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

func lyricInterludeAt(lines []lyrics.Line, cur int) lyricInterlude {
	if cur+1 >= len(lines) {
		return lyricInterlude{}
	}
	to := lines[cur+1].At
	if cur < 0 {
		if to >= 4*time.Second {
			return lyricInterlude{to: to, intro: true}
		}
		return lyricInterlude{}
	}
	line := lines[cur]
	// Leave room to read the line after its last word finishes. A line
	// with no end timestamp cannot tell us where a break begins.
	if !line.Timed || lyrics.IsCredit(line.Text) {
		return lyricInterlude{}
	}
	end := line.End
	for _, word := range line.Words {
		end = max(end, word.End)
	}
	if end > line.At && to-end > 4*time.Second {
		return lyricInterlude{from: end + time.Second, to: to}
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

type lyricSweep struct {
	width float32
	parts []lyricSweepPart
}

type lyricSweepPart struct {
	rect ui.Rect
	rtl  bool
}

// Shader.Fill leaves color glyphs outside the sweep. Symbol words keep the
// opacity highlight, including emoji, flags and variation/keycap sequences.
func lyricMayHaveColorGlyph(text string) bool {
	for _, r := range text {
		if unicode.IsSymbol(r) || r == '\uFE0F' || r == '\u20E3' {
			return true
		}
	}
	return false
}

// Resolve the sweep direction from the carets at each visual line's ends.
// Disjoint bidi runs keep the opacity highlight so no unrelated text is swept.
func shapeLyricSweep(layout *ui.TextLayout, lines []ui.TextLine, clips []ui.Rect, start, end int) lyricSweep {
	var sweep lyricSweep
	for _, clip := range clips {
		if n := len(sweep.parts); n > 0 && sweep.parts[n-1].rect.Y == clip.Y {
			return lyricSweep{}
		}
		for _, line := range lines {
			if line.Rect.Y != clip.Y {
				continue
			}
			first := layout.CaretAt(ui.TextCaretPosition{Index: max(start, line.Start), Affinity: ui.TextDownstream})
			last := layout.CaretAt(ui.TextCaretPosition{Index: min(end, line.End), Affinity: ui.TextUpstream})
			// Mixed runs can share a single selection rect. Only use a
			// directional sweep if the carets cover the selected advance.
			if abs32(abs32(last.X-first.X)-clip.W) > 1 {
				return lyricSweep{}
			}
			sweep.parts = append(sweep.parts, lyricSweepPart{rect: clip, rtl: last.X < first.X})
			sweep.width += clip.W
			break
		}
	}
	if len(sweep.parts) != len(clips) {
		return lyricSweep{}
	}
	return sweep
}

const lyricSweepSource = `package main

var Lead float
var Reverse float

func Fragment(dst vec4, pos vec2) vec4 {
	x := mix(pos.x, rectSize().x-pos.x, Reverse)
	fade := min(5.0, rectSize().x*0.25)
	return vec4(1.0-smoothstep(Lead-fade, Lead+fade, x))
}
`

var lyricSweepShader = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(lyricSweepSource) })

func (s lyricSweep) paint(p *ui.Painter, r ui.Rect, layout *ui.TextLayout, progress float32) {
	// A word on two lines sweeps through the first part before the next.
	// Extend the travel at both ends so the soft front starts and ends cleanly.
	lead := progress*(s.width+10) - 5
	for _, part := range s.parts {
		clip := part.rect
		if lead <= -5 {
			break
		}
		clip.X += r.X
		clip.Y += r.Y
		p.Clip(clip, 0, func() {
			draw := func() { p.TextLayout(layout, r.X, r.Y, white) }
			if lead >= clip.W+5 {
				draw()
				return
			}
			reverse := float32(0)
			if part.rtl {
				reverse = 1
			}
			lyricSweepShader().Fill(p, clip, shader.Options{Uniforms: map[string]any{"Lead": lead, "Reverse": reverse}}, draw)
		})
		lead -= clip.W
	}
}
