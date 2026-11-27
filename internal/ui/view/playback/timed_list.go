/*
虚拟化长歌词与字幕列表，按可见区域测量行高并保留阅读锚点。
*/

package playback

import (
	"sort"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
)

// Short blocks still use their natural layout so they can center as a unit.
const TimedListThreshold = 32
const TimedLineGap = 6
const timedLineInsetX = 12
const TimedLineInsetY = 10
const timedPaneInsetRight = 8

type TimedListLayout struct {
	width             float32
	font, translation ui.Font
	themeFamily       string
	direction         ui.Direction
	leading           float32
	first             *lyrics.Line
	revision          uint64
	Measured          []bool
	manual            bool
	Offsets           []float32 // start of each row, plus the end of the last row and gap
	poseKey           uint64
}

// Reset estimates unvisited heights. Width changes invalidate actual wrapping,
// while an ordinary transcript append keeps all the measurements already made.
func (m *TimedListLayout) Reset(c *ui.Context, lines []lyrics.Line, width float32, st LinesStyle, revision uint64) {
	font, direction := LyricFont(c, st), c.Direction()
	themeFamily := c.Theme().Font
	translation := st.translation.translationFont(themeFamily)
	replaced := st.CenterShort && m.first != &lines[0]
	if replaced || m.revision != revision || m.width != width || m.font != font || m.translation != translation || m.themeFamily != themeFamily || m.direction != direction {
		m.width, m.font, m.themeFamily, m.direction, m.revision = width, font, themeFamily, direction, revision
		m.translation = translation
		m.Offsets = append(m.Offsets[:0], 0)
		m.Measured = m.Measured[:0]
	}
	if st.CenterShort {
		m.first = &lines[0]
	} else {
		m.first = nil
	}
	for i := len(m.Offsets) - 1; i < len(lines); i++ {
		rowFont := timedRowFont(font, lines[i].Kind)
		height := rowFont.Size*rowFont.LineHeight + 2*TimedLineInsetY + TimedLineGap
		if lines[i].Translation != "" && lines[i].Kind != lyrics.Instrumental {
			height += 24 + TimedLineGap
		}
		m.Offsets = append(m.Offsets, m.Offsets[i]+height)
		m.Measured = append(m.Measured, false)
	}
	m.Offsets = m.Offsets[:len(lines)+1]
	m.Measured = m.Measured[:len(lines)]
}

func timedRowFont(font ui.Font, kind lyrics.LineKind) ui.Font {
	if kind != lyrics.Sung {
		font.Size, font.Weight = lyricInfoFont(kind)
		font.LineHeight = 1.25
	}
	return font
}

func (m *TimedListLayout) rowAt(y float32) int {
	n := len(m.Measured)
	return min(n-1, sort.Search(n, func(i int) bool { return m.Offsets[i+1] > y }))
}

// Measure refines just the requested rows, applying their accumulated height
// correction to the suffix once. The caller anchors the viewport to a row.
func (m *TimedListLayout) Measure(lines []lyrics.Line, first, last int, st LinesStyle) {
	var delta float32
	for i := first; i < last; i++ {
		oldHeight := m.Offsets[i+1] - m.Offsets[i]
		m.Offsets[i] += delta
		if m.Measured[i] {
			continue
		}
		l := &lines[i]
		font := timedRowFont(m.font, l.Kind)
		var height float32
		if l.Kind == lyrics.Sung {
			_, height = ShapeLyric(l.Text, font, m.width, m.direction).Size()
		} else {
			font.Family = m.themeFamily
			_, height = ui.ShapeRichTextWith([]ui.Span{{Text: l.Text}}, font, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
		}
		if st.showsTimedWords(l) {
			if text := lyricWordsText(l.Words); text != l.Text {
				_, sung := ShapeLyric(text, m.font, m.width, m.direction).Size()
				height = max(height, sung)
			}
		}
		if l.Translation != "" && l.Kind != lyrics.Instrumental {
			_, trans := ui.ShapeRichTextWith([]ui.Span{{Text: l.Translation}}, m.translation, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
			height += TimedLineGap + trans
		}
		delta += height + 2*TimedLineInsetY + TimedLineGap - oldHeight
		m.Measured[i] = true
	}
	if delta != 0 {
		for i := last; i < len(m.Offsets); i++ {
			m.Offsets[i] += delta
		}
	}
}

func (a *View) longTimedLines(c *ui.Context, scroll ui.Element, lines []lyrics.Line, cur int, st LinesStyle, foot func()) {
	box := st.viewport(scroll.LayoutBounds())
	if box.W <= 0 || box.H <= 0 {
		scroll.Children(func() { ui.Box(c).Height(1) })
		a.LyricViewportW, a.LyricViewportH = 0, 0
		a.LyricGlide = lyricGlide{}
		a.TimedList.manual = false
		c.AnimationFrame()
		return
	}
	m, s, g := &a.TimedList, &a.LyricScroll, &a.LyricGlide
	resized := st.reflowed || box.W != a.LyricViewportW || box.H != a.LyricViewportH
	revision := a.Captions.Revision
	replaced := m.revision != revision
	if st.CenterShort {
		revision = 0
		replaced = m.first != &lines[0]
	}
	changedLine := cur != a.Reading.At
	initialFollow := st.CenterShort && a.Reading.At == lyricInitialFollow
	userMoved := g.posed && s.Y != g.offset
	if replaced || changedLine {
		m.manual = false
	} else if userMoved {
		m.manual = true
	}
	if st.Immersive {
		m.manual = st.browsing
	}
	follow := st.shouldFollow(changedLine || replaced, resized && !m.manual, false)
	anchor, inset := 0, float32(0)
	previousLead := m.leading
	hadMeasurements := len(m.Measured) > 0 && !replaced
	if hadMeasurements {
		anchor = min(m.rowAt(max(0, s.Y-previousLead)), len(lines)-1)
		inset = max(0, s.Y-previousLead-m.Offsets[anchor])
	}
	lineWidth := st.lineWidth(box.W)
	m.Reset(c, lines, lineWidth, st, revision)
	if !hadMeasurements && !follow {
		anchor = m.rowAt(s.Y)
	}
	front, back := anchor, anchor
	followRow := st.followRow(cur, st.intro.Intro)
	if follow {
		row := max(0, followRow)
		front, back = row, row
		if !replaced && Abs32(m.Offsets[row]-s.Y) < box.H*1.3 {
			front, back = min(anchor, row), max(anchor, row)
		}
	}
	// The estimate is at least one visual line high. Measure enough rows
	// around the viewport to include both ends of a normal glide.
	radius := int(box.H/max(1, m.font.Size*m.font.LineHeight+2*TimedLineInsetY+TimedLineGap)*2) + 3
	start, end := max(0, front-radius), min(len(lines), back+radius+1)
	previous := m.Offsets[anchor]
	m.Measure(lines, start, end, st)
	total := m.Offsets[len(lines)] - TimedLineGap
	var intro LyricIntroMotion
	head := float32(0)
	_, h := c.Size()
	if st.CenterShort && total <= box.H {
		head = (box.H - total) / 2
		total += head
	} else {
		if st.Immersive {
			intro = a.lyricIntro(c, scroll, st.intro)
		}
		head = lyricHeadroom(box.H, st)
		tail := h * 0.5
		if st.CenterShort {
			lastHeight := m.Offsets[len(lines)] - m.Offsets[len(lines)-1] - TimedLineGap
			tail = lyricTailPadding(h, box.H, lastHeight)
		}
		total += head + tail
	}
	lead := head
	m.leading = lead
	follow = st.shouldFollow(follow, !m.manual && lead != previousLead, !m.manual && intro.Gap.Intro)
	followRow = st.followRow(cur, intro.Gap.Intro)
	maxY := max(0, total-box.H)
	scroll.Padding(0, max(st.rightInset, timedPaneInsetRight), 0, 0)
	if !replaced {
		if m.manual && (resized || s.Y > previousLead && lead != previousLead) {
			s.Y = min(maxY, lead+m.Offsets[anchor]+min(inset, m.Offsets[anchor+1]-m.Offsets[anchor]-1))
			*g = lyricGlide{}
			m.poseKey++
		} else if s.Y > 0 && !resized {
			delta := m.Offsets[anchor] - previous
			s.Y += delta
			g.offset += delta
			if g.pending {
				g.target += delta
			}
			if delta != 0 && !userMoved && !g.pending && g.settleAt.IsZero() {
				// The anchor already compensates for the measured height.
				// Start at the corrected offset without an extra glide.
				m.poseKey++
			}
		}
	}
	target := s.Y
	if follow {
		target = 0
		if followRow >= 0 {
			lineHeight := m.Offsets[followRow+1] - m.Offsets[followRow] - TimedLineGap
			target = min(maxY, lyricFollowTarget(box.H, lineHeight, m.Offsets[followRow], lead))
		}
	}
	if !st.browsing {
		a.Reading.At = cur
	}
	a.LyricViewportW, a.LyricViewportH = box.W, box.H
	// A distant seek jumps directly to its newly measured neighborhood.
	snap := follow && (initialFollow || replaced || resized ||
		lyricShouldSnap(c, target, s.Y, box.H, false) || target > s.MaxY ||
		intro.Gap.Intro && Abs32(target-s.Y) > 0.5)
	if snap {
		s.Y, *g = target, lyricGlide{}
		m.poseKey++
	} else if follow {
		g.to(c, s.Y, target)
	}
	scroll.Children(func() {
		layer := ui.Column(c.Key(m.poseKey)).Shrink(0)
		content := layer
		if st.Immersive {
			layer.Children(func() { content = ui.Column(c.Key("rows")).Shrink(0) })
		}
		shown := s.Y
		if g.posed && content.Transitioning() {
			shown += lyricPoseLag(scroll, content)
		}
		destination := s.Y
		if g.posed && s.Y != g.offset {
			// Manual input cancels the old destination.
			destination = shown
		} else if g.pending {
			destination = g.target
		}
		// Retain both ends of the glide plus nearby lines for manual input.
		padding := box.H * 0.5
		if intro.Gap.Intro {
			padding += LyricIntroSpace
		}
		lo := max(0, min(shown, destination)-padding-lead)
		hi := max(shown, destination) + box.H + padding - lead
		first := sort.Search(len(lines), func(i int) bool { return m.Offsets[i+1] > lo })
		last := sort.Search(len(lines), func(i int) bool { return m.Offsets[i] >= hi })
		var active, hover, firstSung, slot, dots ui.Element
		var credits [2]ui.Element
		content.Children(func() {
			if head > 0 {
				ui.Box(c.Key("head")).Height(head).Shrink(0)
			}
			before := m.Offsets[first]
			if before > 0 {
				ui.Box(c.Key("before-rows")).Height(before).Shrink(0)
			}
			for i := first; i < last; i++ {
				if intro.Gap.Intro && i == st.introLine {
					slot, dots = LyricIntroDots(c)
				}
				height := m.Offsets[i+1] - m.Offsets[i] - TimedLineGap
				row, hovered := a.TimedLine(c.Key(i), &lines[i], i-cur, lineWidth, st)
				row.Height(height).Shrink(0)
				if intro.Gap.Intro && i < st.introLine {
					intro.Move(row, -LyricIntroSpace)
				}
				st.collectLyricRow(row, hovered, i, cur, lines[i].Kind, &active, &hover, &firstSung, &credits)
				if i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(TimedLineGap).Shrink(0)
				}
			}
			bottom := m.Offsets[last]
			if last == len(lines) {
				bottom -= TimedLineGap
			}
			if rest := total - bottom - lead; rest > 0 {
				ui.Box(c.Key("after-rows")).Height(rest).Shrink(0)
			}
			if foot != nil {
				foot()
			}
		})
		var remaining func() float32
		if intro.Gap.Intro {
			if slot.Valid() {
				remaining = a.IntroDots(c, slot, dots, intro)
			}
			extra := float32(0)
			if !st.browsing {
				lineHeight := m.Offsets[st.introLine+1] - m.Offsets[st.introLine] - TimedLineGap
				extra = head - lyricReadInset(box.H, lineHeight)
			}
			intro.Move(layer, LyricIntroSpace+extra)
			*g = lyricGlide{offset: s.Y, posed: true}
		} else {
			g.place(c, s, scroll, content, st.browsing)
		}
		if st.Immersive {
			if maxY > 0 {
				a.LyricEdges(scroll, active, hover, dots, firstSung, remaining, st.browsing, credits, st.rightInset)
			} else {
				a.LyricBlurPass = nil
			}
		}
	})
}

type timedLineAfter int
