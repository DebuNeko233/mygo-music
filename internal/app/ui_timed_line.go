package app

import (
	"strings"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

const inactiveTimedLyricScale = 0.94

// wholeLineHighlight keeps estimated LRC word times out of the lyric pane.
func (st linesStyle) wholeLineHighlight(line *lyrics.Line) bool {
	return (st.centerShort || st.immersive) && !line.Timed
}

func (st linesStyle) showsTimedWords(line *lyrics.Line) bool {
	return line.Kind == lyrics.Sung && !st.wholeLineHighlight(line) && len(line.Words) > 0
}

func (a *app) timedLine(c *ui.Context, l *lyrics.Line, relative int, width float32, st linesStyle) (ui.Element, bool) {
	if l.Kind != lyrics.Sung {
		row := lyricInfoRow(c, l, st.lang, st.translation)
		return row, st.immersive && !st.browsing && row.Hovered()
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
	scale := float32(inactiveTimedLyricScale)
	if on {
		scale = 1
	}
	line.TransformOrigin(0, 0.5).AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.1}})
	line.Children(func() {
		// The line fades as a pose, in frames that only paint, rather
		// than building the view at the display's rate as it does.
		text := ui.Column(c).AlignItems(ui.Start).Gap(6)
		fade := ui.Animation{Duration: 380 * time.Millisecond}
		if on && st.wholeLineHighlight(l) {
			fade.Duration = 180 * time.Millisecond
		} else if st.immersive && !st.browsing && !st.quiet && !hovered && relative > 1 {
			fade.Delay = time.Duration(min(relative-1, 3)) * 35 * time.Millisecond
		}
		text.AnimatePose(ui.Pose{Fade: 1 - alpha}, fade)
		text.Children(func() {
			if on && width > 0 && st.showsTimedWords(l) {
				a.paintLyricWords(c, l, width, st)
			} else {
				line := ui.Text(c, l.Text).Lang(st.lang).FontSize(st.size).FontWeight(st.weight).LineHeight(st.lineHeight).LetterSpacing(-0.4).TextColor(white)
				if st.family != "" {
					line.Font(st.family)
				}
			}
			if l.Translation != "" {
				st.translation.apply(ui.Text(c, l.Translation)).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.7))
			}
		})
	})
	if line.Clicked() && !nearClose {
		a.pl.Seek(l.At)
		if st.centerShort {
			a.lyricClickSeekRevision = a.pl.SeekRevision()
			c.AnimationFrame()
		}
	}
	return line, hovered
}

func lyricInfoFont(kind lyrics.LineKind) (size float32, weight int) {
	if kind == lyrics.Instrumental {
		return 24, 600
	}
	return 16, 500
}

func lyricStaticNotice(c *ui.Context, text, lang string) ui.Element {
	size, weight := lyricInfoFont(lyrics.Instrumental)
	return ui.Text(c, text).Lang(lang).FontSize(size).FontWeight(weight).LineHeight(1.25).TextColor(white.Alpha(0.85))
}

func lyricInfoRow(c *ui.Context, l *lyrics.Line, lang string, translatedFont lyricTypeface) ui.Element {
	row := ui.Column(c).Padding(timedLineInsetY, timedLineInsetX).Gap(timedLineGap)
	row.Children(func() {
		if l.Kind == lyrics.Instrumental {
			lyricStaticNotice(c, l.Text, lang)
			return
		}
		size, weight := lyricInfoFont(l.Kind)
		ui.Text(c, l.Text).Lang(lang).FontSize(size).FontWeight(weight).LineHeight(1.25).TextColor(white.Alpha(0.68))
		if l.Translation != "" {
			translatedFont.apply(ui.Text(c, l.Translation)).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.55))
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
	height    float32
}

func (st linesStyle) lineWidth(viewport float32) float32 {
	if viewport <= 0 {
		return 0
	}
	return max(1, viewport-max(st.rightInset, timedPaneInsetRight)-2*timedLineInsetX)
}

func lyricFont(c *ui.Context, st linesStyle) ui.Font {
	family := st.family
	if family == "" {
		family = c.Theme().Font
	}
	return ui.Font{Family: family, Size: st.size, Weight: st.weight, Lang: st.lang, LineHeight: st.lineHeight}
}

func shapeLyric(text string, font ui.Font, width float32, direction ui.Direction) *ui.TextLayout {
	return ui.ShapeRichTextWith([]ui.Span{{Text: text, LetterSpacing: -0.4}}, font, ui.TextShape{Width: width, Direction: direction})
}

// measureLyricWords shapes the line without changing its glyph positions as
// the highlight advances. Keeping the layout here also lets desktop lyrics
// outline the complete line and its highlight with the same geometry.
func measureLyricWords(line *lyrics.Line, width float32, font ui.Font, direction ui.Direction) *ui.TextLayout {
	return shapeLyric(lyricWordsText(line.Words), font, width, direction)
}

// drawOutlinedLyricWords paints the same progressive highlight as the lyric
// page, with the desktop window's colours and an outline around every layer.
func drawOutlinedLyricWords(p *ui.Painter, r ui.Rect, words []lyrics.Word, layout *ui.TextLayout, clips [][]ui.Rect, pos time.Duration, playing bool, played, idle, outline ui.Color, outlineWidth float32) {
	paint := func(color ui.Color, dx, dy float32) {
		p.TextLayout(layout, r.X+dx, r.Y+dy, color)
	}
	if outlineWidth > 0 {
		for _, off := range []struct{ dx, dy float32 }{
			{0, -outlineWidth}, {outlineWidth * .7, -outlineWidth * .7},
			{outlineWidth, 0}, {outlineWidth * .7, outlineWidth * .7},
			{0, outlineWidth}, {-outlineWidth * .7, outlineWidth * .7},
			{-outlineWidth, 0}, {-outlineWidth * .7, -outlineWidth * .7},
		} {
			paint(outline, off.dx, off.dy)
		}
	}
	paint(idle, 0, 0)
	for i, word := range words {
		lit, _ := wordPaintTime(word, pos)
		if lit == 0 {
			continue
		}
		for _, clip := range clips[i] {
			clip.X += r.X
			clip.Y += r.Y
			if lit == 1 {
				p.Clip(clip, 0, func() { paint(played, 0, 0) })
			} else {
				// KTV fill sweeps across a word instead of fading its whole text.
				clip.W *= lit
				p.Clip(clip, 0, func() { paint(played, 0, 0) })
			}
		}
	}
	if playing {
		var next time.Duration
		for _, word := range words {
			if _, delay := wordPaintTime(word, pos); delay > 0 && (next == 0 || delay < next) {
				next = delay
			}
		}
		if next > 0 {
			p.After(next)
		}
	}
}

func lyricWordsText(words []lyrics.Word) string {
	var text strings.Builder
	for _, w := range words {
		text.WriteString(w.Text)
	}
	return text.String()
}

// lyricWordClips maps timed words to the shaped text's UTF-16 selection ranges.
func lyricWordClips(words []lyrics.Word, layout *ui.TextLayout) [][]ui.Rect {
	clips := make([][]ui.Rect, len(words))
	offset := 0
	for i, word := range words {
		end := offset
		for _, r := range word.Text {
			end += utf16.RuneLen(r)
		}
		if end > offset {
			clips[i] = layout.SelectionRects(ui.TextInputRange{Start: offset, End: end})
		}
		offset = end
	}
	return clips
}

func (a *app) paintLyricWords(c *ui.Context, line *lyrics.Line, width float32, st linesStyle) {
	font := lyricFont(c, st)
	direction := c.Direction()
	cache := &a.paintedLyric
	if cache.line != line || cache.width != width || cache.font != font || cache.direction != direction {
		layout := shapeLyric(lyricWordsText(line.Words), font, width, direction)
		_, height := layout.Size()
		clips := lyricWordClips(line.Words, layout)
		*cache = paintedLyric{line: line, width: width, font: font, direction: direction, layout: layout, clips: clips, height: height}
	}
	layout, clips := cache.layout, cache.clips
	ui.Box(c).Width(width).Height(cache.height).Draw(func(p *ui.Painter, r ui.Rect) {
		drawLyricWords(p, r, line.Words, layout, clips, a.pl.Position(), a.pl.Playing())
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
func drawLyricWords(p *ui.Painter, r ui.Rect, words []lyrics.Word, layout *ui.TextLayout, clips [][]ui.Rect, pos time.Duration, playing bool) {
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
