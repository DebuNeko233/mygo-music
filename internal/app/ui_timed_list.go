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
		height := font.Size*font.LineHeight + 2*timedLineInsetY + timedLineGap
		if lines[i].Translation != "" {
			height += 24 + timedLineGap
		}
		m.offsets = append(m.offsets, m.offsets[i]+height)
		m.measured = append(m.measured, false)
	}
	m.offsets = m.offsets[:len(lines)+1]
	m.measured = m.measured[:len(lines)]
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
		_, height := shapeLyric(l.Text, m.font, m.width, m.direction).Size()
		if len(l.Words) > 0 {
			if text := lyricWordsText(l.Words); text != l.Text {
				_, sung := shapeLyric(text, m.font, m.width, m.direction).Size()
				height = max(height, sung)
			}
		}
		if l.Translation != "" {
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
	follow := changedLine || replaced || resized && !m.manual
	anchor, inset := 0, float32(0)
	hadMeasurements := len(m.measured) > 0 && !replaced
	if hadMeasurements {
		anchor = min(m.rowAt(s.Y), len(lines)-1)
		inset = max(0, s.Y-m.offsets[anchor])
	}
	m.reset(c, lines, timedLineWidth(box.W), st, revision)
	if !hadMeasurements && !follow {
		anchor = m.rowAt(s.Y)
	}
	front, back := anchor, anchor
	if follow {
		row := max(0, cur)
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
	center := float32(0)
	_, h := c.Size()
	if st.centerShort && total <= box.H {
		center = (box.H - total) / 2
		total = box.H - center
	} else {
		total += h * 0.5
	}
	maxY := max(0, total-box.H)
	scroll.Padding(0, timedPaneInsetRight, 0, 0)
	if !replaced {
		if resized && m.manual {
			s.Y = min(maxY, m.offsets[anchor]+min(inset, m.offsets[anchor+1]-m.offsets[anchor]-1))
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
		if cur >= 0 {
			target = min(max(0, m.offsets[cur]-box.H*0.3), maxY)
		}
	}
	a.lyricAt, a.lyricViewportW, a.lyricViewportH = cur, box.W, box.H
	// A distant seek jumps directly to its newly measured neighborhood.
	snap := follow && (resized || replaced || abs32(target-s.Y) > box.H || c.Preferences().ReduceMotion || target > s.MaxY)
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
		lo := max(0, min(shown, destination)-padding)
		hi := max(shown, destination) + box.H + padding
		first := sort.Search(len(lines), func(i int) bool { return m.offsets[i+1] > lo })
		last := sort.Search(len(lines), func(i int) bool { return m.offsets[i] >= hi })
		content.Children(func() {
			if before := center + m.offsets[first]; before > 0 {
				ui.Box(c.Key("before-rows")).Height(before).Shrink(0)
			}
			for i := first; i < last; i++ {
				height := m.offsets[i+1] - m.offsets[i] - timedLineGap
				a.timedLine(c.Key(i), &lines[i], i == cur, timedLineWidth(box.W), st).Height(height).Shrink(0)
				if i+1 < len(lines) {
					ui.Box(c.Key(timedLineAfter(i))).Height(timedLineGap).Shrink(0)
				}
			}
			bottom := m.offsets[last]
			if last == len(lines) {
				bottom -= timedLineGap
			}
			if rest := total - bottom; rest > 0 {
				ui.Box(c.Key("after-rows")).Height(rest).Shrink(0)
			}
			if foot != nil {
				foot()
			}
		})
		g.place(c, s, scroll, content)
	})
}

type timedLineAfter int
