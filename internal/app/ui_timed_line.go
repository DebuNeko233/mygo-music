package app

import (
	"strings"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

func (a *app) timedLine(c *ui.Context, l *lyrics.Line, relative int, width float32, st linesStyle) ui.Element {
	if l.Kind != lyrics.Sung {
		return lyricInfoRow(c, l, st.lang)
	}
	line := ui.ButtonBase(c).Column().AlignItems(ui.Start).Padding(timedLineInsetY, timedLineInsetX).Radius(12).Gap(6).FocusRing(false).Label(l.Text)
	// Use the painted bounds so scrolling a line near the close button
	// protects the entire row, including its translation and padding.
	nearClose := false
	if close := st.closeBounds; close.W > 0 {
		row := line.Bounds()
		const guard = 16
		nearClose = row.W > 0 && row.H > 0 && row.X < close.X+close.W+guard && row.X+row.W > close.X-guard &&
			row.Y < close.Y+close.H+guard && row.Y+row.H > close.Y-guard
		if nearClose {
			line.PassThrough().AccessDisabled(true)
		}
	}
	on := relative == 0
	// LRC supplies a line timestamp. Its estimated word times are useful
	// elsewhere, but here the whole line should light at the same time.
	lineTimed := (st.centerShort || st.immersive) && !l.Timed
	hovered := !nearClose && line.Hovered()
	alpha := float32(0.28)
	if on {
		alpha = 1
	} else if hovered {
		alpha = 0.55
	} else if st.immersive {
		alpha = lyricLineOpacity(relative, st.browsing)
	}
	if st.quiet {
		// Nearly gone under a hot comment.
		alpha *= quietFaded
	}
	if hovered {
		line.Background(white.Alpha(0.06))
	}
	scale := float32(0.94)
	if on {
		scale = 1
	}
	line.TransformOrigin(0, 0.5).AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.1}})
	line.Children(func() {
		// The line fades as a pose, in frames that only paint, rather
		// than building the view at the display's rate as it does.
		text := ui.Column(c).AlignItems(ui.Start).Gap(6)
		fade := ui.Animation{Duration: 380 * time.Millisecond}
		if lineTimed && on {
			fade.Duration = 180 * time.Millisecond
		} else if st.immersive && !st.browsing && !st.quiet && !hovered && relative > 1 {
			fade.Delay = time.Duration(min(relative-1, 3)) * 35 * time.Millisecond
		}
		text.AnimatePose(ui.Pose{Fade: 1 - alpha}, fade)
		text.Children(func() {
			if on && !lineTimed && len(l.Words) > 0 && width > 0 {
				a.paintLyricWords(c, l, width, st)
			} else {
				ui.Text(c, l.Text).Lang(st.lang).FontSize(st.size).FontWeight(st.weight).LineHeight(st.lineHeight).LetterSpacing(-0.4).TextColor(white)
			}
			if l.Translation != "" {
				ui.Text(c, l.Translation).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.7))
			}
		})
	})
	if line.Clicked() && !nearClose {
		if st.immersive {
			a.seekLyricFollow()
			c.AnimationFrame()
		}
		a.pl.Seek(l.At)
	}
	return line
}

func lyricInfoFont(kind lyrics.LineKind) (size float32, weight int) {
	if kind == lyrics.Instrumental {
		return 24, 700
	}
	return 16, 500
}

func lyricInfoRow(c *ui.Context, l *lyrics.Line, lang string) ui.Element {
	row := ui.Column(c).Padding(timedLineInsetY, timedLineInsetX).Gap(timedLineGap)
	size, weight := lyricInfoFont(l.Kind)
	color := white.Alpha(0.68)
	if l.Kind == lyrics.Instrumental {
		color = white.Alpha(0.85)
	}
	row.Children(func() {
		ui.Text(c, l.Text).Lang(lang).FontSize(size).FontWeight(weight).LineHeight(1.25).TextColor(color)
		if l.Translation != "" && l.Kind != lyrics.Instrumental {
			ui.Text(c, l.Translation).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.55))
		}
	})
	return row
}

type paintedLyric struct {
	line      *lyrics.Line
	width     float32
	font      ui.Font
	direction ui.Direction
	layout    *ui.TextLayout
	clips     [][]ui.Rect
	sweep     []lyricSweep
	immersive bool
	height    float32
}

func timedLineWidth(viewport float32) float32 {
	if viewport <= 0 {
		return 0
	}
	return max(1, viewport-timedPaneInsetRight-2*timedLineInsetX)
}

func lyricFont(c *ui.Context, st linesStyle) ui.Font {
	return ui.Font{Family: c.Theme().Font, Size: st.size, Weight: st.weight, Lang: st.lang, LineHeight: st.lineHeight}
}

func shapeLyric(text string, font ui.Font, width float32, direction ui.Direction) *ui.TextLayout {
	return ui.ShapeRichTextWith([]ui.Span{{Text: text, LetterSpacing: -0.4}}, font, ui.TextShape{Width: width, Direction: direction})
}

func lyricWordsText(words []lyrics.Word) string {
	var text strings.Builder
	for _, w := range words {
		text.WriteString(w.Text)
	}
	return text.String()
}

func (a *app) paintLyricWords(c *ui.Context, line *lyrics.Line, width float32, st linesStyle) {
	font := lyricFont(c, st)
	direction := c.Direction()
	cache := &a.paintedLyric
	sweep := st.immersive && !c.Preferences().ReduceMotion
	if cache.line != line || cache.width != width || cache.font != font || cache.direction != direction || cache.immersive != sweep {
		layout := shapeLyric(lyricWordsText(line.Words), font, width, direction)
		_, height := layout.Size()
		clips := make([][]ui.Rect, len(line.Words))
		var sweeps []lyricSweep
		var visualLines []ui.TextLine
		if sweep {
			sweeps = make([]lyricSweep, len(line.Words))
			visualLines = layout.Lines()
		}
		offset := 0
		for i, word := range line.Words {
			end := offset
			for _, r := range word.Text {
				end += utf16.RuneLen(r)
			}
			if end > offset {
				clips[i] = layout.SelectionRects(ui.TextInputRange{Start: offset, End: end})
				if sweep && !lyricMayHaveColorGlyph(word.Text) {
					sweeps[i] = shapeLyricSweep(layout, visualLines, clips[i], offset, end)
				}
			}
			offset = end
		}
		*cache = paintedLyric{line: line, width: width, font: font, direction: direction, layout: layout, clips: clips, height: height, sweep: sweeps, immersive: sweep}
	}
	layout, clips, sweeps := cache.layout, cache.clips, cache.sweep
	ui.Box(c).Width(width).Height(cache.height).Draw(func(p *ui.Painter, r ui.Rect) {
		drawLyricWords(p, r, line.Words, layout, clips, sweeps, a.pl.Position(), a.pl.Playing())
	})
}

func wordPaintTime(word lyrics.Word, pos time.Duration) (float32, time.Duration) {
	duration := max(50*time.Millisecond, word.End-word.Start)
	elapsed := pos - word.Start
	switch {
	case elapsed < 0:
		return 0, -elapsed + min(40*time.Millisecond, duration)
	case elapsed >= duration:
		return 1, 0
	default:
		return float32(elapsed) / float32(duration), min(40*time.Millisecond, duration-elapsed)
	}
}

// Only the highlight changes at playback speed. The retained text keeps
// its wrapping and glyph positions, including when a word crosses lines.
func drawLyricWords(p *ui.Painter, r ui.Rect, words []lyrics.Word, layout *ui.TextLayout, clips [][]ui.Rect, sweeps []lyricSweep, pos time.Duration, playing bool) {
	var next time.Duration
	allLit := true
	for _, word := range words {
		lit, delay := wordPaintTime(word, pos)
		allLit = allLit && lit == 1
		if delay > 0 && (next == 0 || delay < next) {
			next = delay
		}
	}
	if allLit {
		p.TextLayout(layout, r.X, r.Y, white)
		return
	}
	p.TextLayout(layout, r.X, r.Y, white.Alpha(0.38))
	var full ui.Rect
	paint := func(clip ui.Rect, lit float32) {
		clip.X += r.X
		clip.Y += r.Y
		p.Clip(clip, 0, func() { p.TextLayout(layout, r.X, r.Y, white.Alpha(lit)) })
	}
	for i, word := range words {
		lit, _ := wordPaintTime(word, pos)
		if lit == 0 {
			continue
		}
		if lit < 1 && len(sweeps) > 0 && sweeps[i].width > 0 {
			sweeps[i].paint(p, r, layout, lit)
			continue
		}
		for _, clip := range clips[i] {
			if lit == 1 {
				// Merge adjacent finished words on the same visual line.
				if full.H == clip.H && full.Y == clip.Y && clip.X <= full.X+full.W+0.1 && full.X <= clip.X+clip.W+0.1 {
					left := min(full.X, clip.X)
					full.X, full.W = left, max(full.X+full.W, clip.X+clip.W)-left
					continue
				}
				if full.W > 0 {
					paint(full, 1)
				}
				full = clip
			} else {
				paint(clip, lit)
			}
		}
	}
	if full.W > 0 {
		paint(full, 1)
	}
	if playing && next > 0 {
		p.After(next)
	}
}
