package app

import (
	"sort"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// Short blocks still use their natural layout so they can center as a unit.
const timedListThreshold = 64
const timedLineGap = 6
const timedLineInsetX = 12
const timedLineInsetY = 10
const timedPaneInsetRight = 8

type timedListLayout struct {
	width     float32
	font      ui.Font
	direction ui.Direction
	leading   float32
	first     *lyrics.Line
	revision  uint64
	measured  []bool
	manual    bool
	offsets   []float32 // start of each row, plus the end of the last row and gap
	poseKey   uint64
}

// reset estimates unvisited heights. Width changes invalidate actual wrapping,
// while an ordinary transcript append keeps all the measurements already made.
func (m *timedListLayout) reset(c *ui.Context, lines []lyrics.Line, width float32, st linesStyle, revision uint64) {
	font, direction := lyricFont(c, st), c.Direction()
	replaced := st.centerShort && m.first != &lines[0]
	if replaced || m.revision != revision || m.width != width || m.font != font || m.direction != direction {
		m.width, m.font, m.direction, m.revision = width, font, direction, revision
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
func (m *timedListLayout) measure(lines []lyrics.Line, first, last int) {
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
			_, height = ui.ShapeRichTextWith([]ui.Span{{Text: l.Text}}, font, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
		}
		if len(l.Words) > 0 {
			if text := lyricWordsText(l.Words); text != l.Text {
				_, sung := shapeLyric(text, m.font, m.width, m.direction).Size()
				height = max(height, sung)
			}
		}
		if l.Translation != "" && l.Kind != lyrics.Instrumental {
			_, trans := ui.ShapeRichTextWith([]ui.Span{{Text: l.Translation}}, ui.Font{Family: m.font.Family, Size: 17, Weight: 600}, ui.TextShape{Width: m.width, Direction: m.direction}).Size()
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
	box := scroll.LayoutBounds()
	if box.W <= 0 || box.H <= 0 {
		scroll.Children(func() { ui.Box(c).Height(1) })
		c.AnimationFrame()
		return
	}
	m, s, g := &a.timedList, &a.lyricScroll, &a.lyricGlide
	resized := box.W != a.lyricViewportW || box.H != a.lyricViewportH
	revision := a.captions.revision
	replaced := m.revision != revision
	if st.centerShort {
		revision = 0
		replaced = m.first != &lines[0]
	}
	changedLine := cur != a.lyricAt
	userMoved := g.posed && s.Y != g.offset
	if replaced || changedLine {
		m.manual = false
	} else if userMoved {
		m.manual = true
	}
	if st.immersive {
		m.manual = st.browsing
	}
	follow := !st.browsing && (changedLine || replaced || resized && !m.manual)
	anchor, inset := 0, float32(0)
	previousLead := m.leading
	hadMeasurements := len(m.measured) > 0 && !replaced
	if hadMeasurements {
		anchor = min(m.rowAt(max(0, s.Y-previousLead)), len(lines)-1)
		inset = max(0, s.Y-previousLead-m.offsets[anchor])
	}
	m.reset(c, lines, timedLineWidth(box.W), st, revision)
	if !hadMeasurements && !follow {
		anchor = m.rowAt(s.Y)
	}
	front, back := anchor, anchor
	followRow := cur
	if st.intro.intro {
		followRow = st.introLine
	}
	if follow {
		row := max(0, followRow)
		front, back = row, row
		if !resized && !replaced && abs32(m.offsets[row]-s.Y) < box.H*1.3 {
			front, back = min(anchor, row), max(anchor, row)
		}
	}
	// The estimate is at least one visual line high. Measure enough rows
	// around the viewport to include both ends of a normal glide.
	radius := int(box.H/max(1, m.font.Size*m.font.LineHeight+2*timedLineInsetY+timedLineGap)*2) + 3
	start, end := max(0, front-radius), min(len(lines), back+radius+1)
	previous := m.offsets[anchor]
	m.measure(lines, start, end)
	total := m.offsets[len(lines)] - timedLineGap
	var introHeight, introOpacity float32
	head := float32(0)
	_, h := c.Size()
	if st.centerShort && total <= box.H {
		head = (box.H - total) / 2
		total += head
	} else {
		introHeight, introOpacity = a.lyricIntro(c, st.intro)
		head = lyricHeadroom(box.H, st)
		total += head + introHeight + h*0.5
	}
	lead := head
	m.leading = lead
	if st.immersive && !st.browsing && (lead != previousLead || introHeight > 0) {
		follow = true
	}
	maxY := max(0, total-box.H)
	scroll.Padding(0, timedPaneInsetRight, 0, 0)
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
			inset := lyricReadInset(box.H, lineHeight)
			if introHeight > 0 {
				inset = lerp(inset, head, introOpacity)
			}
			target = min(max(0, lead+m.offsets[followRow]-inset), maxY)
		}
	}
	if !st.browsing {
		a.lyricAt = cur
	}
	a.lyricViewportW, a.lyricViewportH = box.W, box.H
	// A distant seek jumps directly to its newly measured neighborhood.
	snap := follow && (resized || replaced || abs32(target-s.Y) > box.H || c.Preferences().ReduceMotion || target > s.MaxY || introHeight > 0 && abs32(target-s.Y) > 0.5)
	if snap {
		s.Y, *g = target, lyricGlide{}
		m.poseKey++
	} else if follow {
		g.to(c, s.Y, target)
	}
	scroll.Children(func() {
		content := ui.Column(c.Key(m.poseKey)).Shrink(0)
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
		lo := max(0, min(shown, destination)-padding-lead-introHeight)
		hi := max(shown, destination) + box.H + padding - lead
		first := sort.Search(len(lines), func(i int) bool { return m.offsets[i+1] > lo })
		last := sort.Search(len(lines), func(i int) bool { return m.offsets[i] >= hi })
		var active, hovered, intro, firstLine ui.Element
		content.Children(func() {
			if head > 0 {
				ui.Box(c.Key("head")).Height(head).Shrink(0)
			}
			before := m.offsets[first]
			if st.introLine < first {
				before += introHeight
			}
			if before > 0 {
				ui.Box(c.Key("before-rows")).Height(before).Shrink(0)
			}
			for i := first; i < last; i++ {
				if introHeight > 0 && i == st.introLine {
					intro = ui.Box(c.Key("intro")).Height(introHeight).Shrink(0)
					a.paintLyricInterlude(c, intro, st.intro, introOpacity)
				}
				height := m.offsets[i+1] - m.offsets[i] - timedLineGap
				row := a.timedLine(c.Key(i), &lines[i], i-cur, timedLineWidth(box.W), st).Height(height).Shrink(0)
				if st.immersive && row.Hovered() {
					hovered = row
				}
				if st.intro.intro && i == st.introLine {
					firstLine = row
				}
				if i == cur {
					active = row
				}
				if i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(timedLineGap).Shrink(0)
				}
			}
			bottom := m.offsets[last]
			if st.introLine < last {
				bottom += introHeight
			}
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
		g.place(c, s, scroll, content, st.browsing)
		if st.immersive && maxY > 0 {
			a.lyricEdges(scroll, active, hovered, intro, firstLine, introOpacity, st.browsing)
		}
	})
}

type timedLineAfter int
