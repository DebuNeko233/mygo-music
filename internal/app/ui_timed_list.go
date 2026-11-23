package app

import (
	"sort"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// Short blocks still use their natural layout so they can center as a unit.
const timedListThreshold = 32
const timedLineGap = 6
const timedLineInsetX = 12
const timedLineInsetY = 10
const timedPaneInsetRight = 8

type timedListLayout struct {
	width             float32
	font, translation ui.Font
	themeFamily       string
	direction         ui.Direction
	leading           float32
	first             *lyrics.Line
	revision          uint64
	measured          []bool
	manual            bool
	offsets           []float32 // start of each row, plus the end of the last row and gap
	poseKey           uint64
}

// reset estimates unvisited heights. Width changes invalidate actual wrapping,
// while an ordinary transcript append keeps all the measurements already made.
func (m *timedListLayout) reset(c *ui.Context, lines []lyrics.Line, width float32, st linesStyle, revision uint64) {
	font, direction := lyricFont(c, st), c.Direction()
	themeFamily := c.Theme().Font
	translation := st.translation.translationFont(themeFamily)
	replaced := st.centerShort && m.first != &lines[0]
	if replaced || m.revision != revision || m.width != width || m.font != font || m.translation != translation || m.themeFamily != themeFamily || m.direction != direction {
		m.width, m.font, m.themeFamily, m.direction, m.revision = width, font, themeFamily, direction, revision
		m.translation = translation
		m.offsets = append(m.offsets[:0], 0)
		m.measured = m.measured[:0]
	}
	if st.centerShort {
		m.first = &lines[0]
	} else {
		m.first = nil
	}
	for i := len(m.offsets) - 1; i < len(lines); i++ {
		rowFont := timedRowFont(font, lines[i].Kind)
		height := rowFont.Size*rowFont.LineHeight + 2*timedLineInsetY + timedLineGap
		if lines[i].Translation != "" && lines[i].Kind != lyrics.Instrumental {
			height += 24 + timedLineGap
		}
		m.offsets = append(m.offsets, m.offsets[i]+height)
		m.measured = append(m.measured, false)
	}
	m.offsets = m.offsets[:len(lines)+1]
	m.measured = m.measured[:len(lines)]
}

func timedRowFont(font ui.Font, kind lyrics.LineKind) ui.Font {
	if kind != lyrics.Sung {
		font.Size, font.Weight = lyricInfoFont(kind)
		font.LineHeight = 1.25
	}
	return font
}

func (m *timedListLayout) rowAt(y float32) int {
	n := len(m.measured)
	return min(n-1, sort.Search(n, func(i int) bool { return m.offsets[i+1] > y }))
}

// measure refines just the requested rows, applying their accumulated height
// correction to the suffix once. The caller anchors the viewport to a row.
func (m *timedListLayout) measure(lines []lyrics.Line, first, last int, st linesStyle) {
	var delta float32
	for i := first; i < last; i++ {
		oldHeight := m.offsets[i+1] - m.offsets[i]
		m.offsets[i] += delta
		if m.measured[i] {
			continue
		}
		l := &lines[i]
		font := timedRowFont(m.font, l.Kind)
		var height float32
		if l.Kind == lyrics.Sung {
			_, height = shapeLyric(l.Text, font, m.width, m.direction).Size()
		} else {
			font.Family = m.themeFamily
			_, height = ui.ShapeRichTextWith([]ui.Span{{Text: l.Text}}, font, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
		}
		if st.showsTimedWords(l) {
			if text := lyricWordsText(l.Words); text != l.Text {
				_, sung := shapeLyric(text, m.font, m.width, m.direction).Size()
				height = max(height, sung)
			}
		}
		if l.Translation != "" && l.Kind != lyrics.Instrumental {
			_, trans := ui.ShapeRichTextWith([]ui.Span{{Text: l.Translation}}, m.translation, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
			height += timedLineGap + trans
		}
		delta += height + 2*timedLineInsetY + timedLineGap - oldHeight
		m.measured[i] = true
	}
	if delta != 0 {
		for i := last; i < len(m.offsets); i++ {
			m.offsets[i] += delta
		}
	}
}

func (a *app) longTimedLines(c *ui.Context, scroll ui.Element, lines []lyrics.Line, cur int, st linesStyle, foot func()) {
	box := st.viewport(scroll.LayoutBounds())
	if box.W <= 0 || box.H <= 0 {
		scroll.Children(func() { ui.Box(c).Height(1) })
		a.lyricViewportW, a.lyricViewportH = 0, 0
		a.lyricGlide = lyricGlide{}
		a.timedList.manual = false
		c.AnimationFrame()
		return
	}
	m, s, g := &a.timedList, &a.lyricScroll, &a.lyricGlide
	resized := box.W != a.lyricViewportW || box.H != a.lyricViewportH
	revision := a.captions.Revision
	replaced := m.revision != revision
	if st.centerShort {
		revision = 0
		replaced = m.first != &lines[0]
	}
	changedLine := cur != a.lyricReading.At
	initialFollow := st.centerShort && a.lyricReading.At == lyricInitialFollow
	userMoved := g.posed && s.Y != g.offset
	if replaced || changedLine {
		m.manual = false
	} else if userMoved {
		m.manual = true
	}
	if st.immersive {
		m.manual = st.browsing
	}
	follow := st.shouldFollow(changedLine || replaced, resized && !m.manual, false)
	anchor, inset := 0, float32(0)
	previousLead := m.leading
	hadMeasurements := len(m.measured) > 0 && !replaced
	if hadMeasurements {
		anchor = min(m.rowAt(max(0, s.Y-previousLead)), len(lines)-1)
		inset = max(0, s.Y-previousLead-m.offsets[anchor])
	}
	lineWidth := st.lineWidth(box.W)
	m.reset(c, lines, lineWidth, st, revision)
	if !hadMeasurements && !follow {
		anchor = m.rowAt(s.Y)
	}
	front, back := anchor, anchor
	followRow := st.followRow(cur, st.intro.intro)
	if follow {
		row := max(0, followRow)
		front, back = row, row
		if !replaced && abs32(m.offsets[row]-s.Y) < box.H*1.3 {
			front, back = min(anchor, row), max(anchor, row)
		}
	}
	// The estimate is at least one visual line high. Measure enough rows
	// around the viewport to include both ends of a normal glide.
	radius := int(box.H/max(1, m.font.Size*m.font.LineHeight+2*timedLineInsetY+timedLineGap)*2) + 3
	start, end := max(0, front-radius), min(len(lines), back+radius+1)
	previous := m.offsets[anchor]
	m.measure(lines, start, end, st)
	total := m.offsets[len(lines)] - timedLineGap
	var intro lyricIntroMotion
	head := float32(0)
	_, h := c.Size()
	if st.centerShort && total <= box.H {
		head = (box.H - total) / 2
		total += head
	} else {
		if st.immersive {
			intro = a.lyricIntro(c, scroll, st.intro)
		}
		head = lyricHeadroom(box.H, st)
		tail := h * 0.5
		if st.centerShort {
			lastHeight := m.offsets[len(lines)] - m.offsets[len(lines)-1] - timedLineGap
			tail = lyricTailPadding(h, box.H, lastHeight)
		}
		total += head + tail
	}
	lead := head
	m.leading = lead
	follow = st.shouldFollow(follow, !m.manual && lead != previousLead, !m.manual && intro.gap.intro)
	followRow = st.followRow(cur, intro.gap.intro)
	maxY := max(0, total-box.H)
	scroll.Padding(0, max(st.rightInset, timedPaneInsetRight), 0, 0)
	if !replaced {
		if m.manual && (resized || s.Y > previousLead && lead != previousLead) {
			s.Y = min(maxY, lead+m.offsets[anchor]+min(inset, m.offsets[anchor+1]-m.offsets[anchor]-1))
			*g = lyricGlide{}
			m.poseKey++
		} else if s.Y > 0 && !resized {
			delta := m.offsets[anchor] - previous
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
			lineHeight := m.offsets[followRow+1] - m.offsets[followRow] - timedLineGap
			target = min(maxY, lyricFollowTarget(box.H, lineHeight, m.offsets[followRow], lead))
		}
	}
	if !st.browsing {
		a.lyricReading.At = cur
	}
	heightChanged := st.centerShort && box.H != a.lyricViewportH
	a.lyricViewportW, a.lyricViewportH = box.W, box.H
	// A distant seek jumps directly to its newly measured neighborhood.
	settledResize := heightChanged && !changedLine && !m.manual && !g.pending && g.settleAt.IsZero()
	snap := follow && (initialFollow || replaced || resized && (!st.immersive || !g.posed) ||
		settledResize || lyricShouldSnap(c, target, s.Y, box.H, false) || target > s.MaxY ||
		intro.gap.intro && abs32(target-s.Y) > 0.5)
	if snap {
		s.Y, *g = target, lyricGlide{}
		m.poseKey++
	} else if follow {
		g.to(c, s.Y, target)
	}
	scroll.Children(func() {
		layer := ui.Column(c.Key(m.poseKey)).Shrink(0)
		content := layer
		if st.immersive {
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
		if intro.gap.intro {
			padding += lyricIntroSpace
		}
		lo := max(0, min(shown, destination)-padding-lead)
		hi := max(shown, destination) + box.H + padding - lead
		first := sort.Search(len(lines), func(i int) bool { return m.offsets[i+1] > lo })
		last := sort.Search(len(lines), func(i int) bool { return m.offsets[i] >= hi })
		var active, hover, firstSung, slot, dots ui.Element
		var credits [2]ui.Element
		content.Children(func() {
			if head > 0 {
				ui.Box(c.Key("head")).Height(head).Shrink(0)
			}
			before := m.offsets[first]
			if before > 0 {
				ui.Box(c.Key("before-rows")).Height(before).Shrink(0)
			}
			for i := first; i < last; i++ {
				if intro.gap.intro && i == st.introLine {
					slot, dots = lyricIntroDots(c)
				}
				height := m.offsets[i+1] - m.offsets[i] - timedLineGap
				row, hovered := a.timedLine(c.Key(i), &lines[i], i-cur, lineWidth, st)
				row.Height(height).Shrink(0)
				if intro.gap.intro && i < st.introLine {
					intro.move(row, -lyricIntroSpace)
				}
				st.collectLyricRow(row, hovered, i, cur, lines[i].Kind, &active, &hover, &firstSung, &credits)
				if i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(timedLineGap).Shrink(0)
				}
			}
			bottom := m.offsets[last]
			if last == len(lines) {
				bottom -= timedLineGap
			}
			if rest := total - bottom - lead; rest > 0 {
				ui.Box(c.Key("after-rows")).Height(rest).Shrink(0)
			}
			if foot != nil {
				foot()
			}
		})
		var remaining func() float32
		if intro.gap.intro {
			if slot.Valid() {
				remaining = a.introDots(c, slot, dots, intro)
			}
			extra := float32(0)
			if !st.browsing {
				lineHeight := m.offsets[st.introLine+1] - m.offsets[st.introLine] - timedLineGap
				extra = head - lyricReadInset(box.H, lineHeight)
			}
			intro.move(layer, lyricIntroSpace+extra)
			*g = lyricGlide{offset: s.Y, posed: true}
		} else {
			g.place(c, s, scroll, content, st.browsing)
		}
		if st.immersive {
			if maxY > 0 {
				a.lyricEdges(scroll, active, hover, dots, firstSung, remaining, st.browsing, credits, st.rightInset)
			} else {
				a.lyricBlurPass = nil
			}
		}
	})
}

type timedLineAfter int
