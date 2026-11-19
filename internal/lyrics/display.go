package lyrics

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type LineKind uint8

const (
	Sung LineKind = iota
	Credit
	Instrumental
)

const (
	InstrumentalText = "纯音乐，请欣赏"
	UnavailableText  = "暂无歌词"
)

// Display keeps the reading order and the playback timeline separate.
type Display struct {
	Lines       []Line
	sung        []int
	unavailable bool
}

func (d Display) HasSinging() bool { return len(d.sung) > 0 }

// WithUnavailableNotice distinguishes missing lyrics from instrumentals.
func (d Display) WithUnavailableNotice() Display {
	d.unavailable = true
	return d
}

// SungLines shares the parsed timing and translations with views that omit metadata.
func (d Display) SungLines() []Line {
	if len(d.sung) == len(d.Lines) {
		return d.Lines
	}
	lines := make([]Line, 0, len(d.sung))
	for _, i := range d.sung {
		lines = append(lines, d.Lines[i])
	}
	return lines
}

// EmptyText is the notice for a loaded display without sung lyrics.
func (d Display) EmptyText() string {
	if d.unavailable {
		return UnavailableText
	}
	return InstrumentalText
}

func (d Display) FirstSung() int {
	if !d.HasSinging() {
		return -1
	}
	return d.sung[0]
}

// Position returns row indices, with -1 for a missing current or next row.
func (d Display) Position(pos time.Duration) (current, next int) {
	i := sort.Search(len(d.sung), func(i int) bool { return d.Lines[d.sung[i]].At > pos })
	current, next = -1, -1
	if i > 0 {
		current = d.sung[i-1]
	}
	if i < len(d.sung) {
		next = d.sung[i]
	}
	return current, next
}

// YRC may put credits in JSON fragments outside the timed word rows.
func yrcCredits(source string) []Line {
	var lines []Line
	for _, raw := range strings.Split(source, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
			continue
		}
		var row struct {
			Time  int64 `json:"t"`
			Parts []struct {
				Text string `json:"tx"`
			} `json:"c"`
		}
		if json.Unmarshal([]byte(raw), &row) != nil || row.Time < 0 || row.Time > (24*time.Hour).Milliseconds() {
			continue
		}
		var text strings.Builder
		for _, part := range row.Parts {
			text.WriteString(part.Text)
		}
		s := strings.TrimSpace(text.String())
		if s != "" {
			if kind := lineKind(s); kind != Sung {
				lines = append(lines, Line{At: time.Duration(row.Time) * time.Millisecond, Text: s, Kind: kind})
			}
		}
	}
	return lines
}

// NewDisplay builds a playback-pane view from parsed lines and any supplemental metadata.
func NewDisplay(lines []Line, supplemental ...Line) Display {
	all := make([]Line, 0, len(lines)+len(supplemental))
	for _, source := range [][]Line{lines, supplemental} {
		start := len(all)
		for _, line := range source {
			if strings.TrimSpace(line.Text) != "" {
				all = append(all, line)
			}
		}
		classifyLines(all[start:])
	}
	classifyInstrumentalNotes(all)
	return newDisplay(all)
}

// newDisplay takes ownership of the classified lines.
func newDisplay(lines []Line) Display {
	var d Display
	var seen map[string]int
	rows := lines[:0]
	for _, line := range lines {
		if line.Kind == unavailable {
			d.unavailable = true
			continue
		}
		if line.Kind != Sung {
			key := InstrumentalText
			if line.Kind == Instrumental {
				line.Text = InstrumentalText
			} else {
				key = metadataKey(line.Text)
			}
			if index, exists := seen[key]; exists {
				if rows[index].Translation == "" {
					rows[index].Translation = line.Translation
				}
				continue
			}
			if seen == nil {
				seen = make(map[string]int)
			}
			seen[key] = len(rows)
			line.Words = nil
		}
		rows = append(rows, line)
	}
	clear(lines[len(rows):])
	d.Lines = rows
	less := func(i, j int) bool { return rows[i].At < rows[j].At }
	if !sort.SliceIsSorted(rows, less) {
		sort.SliceStable(rows, less)
	}
	for i, line := range d.Lines {
		if line.Kind == Sung {
			d.sung = append(d.sung, i)
		}
	}
	return d
}

// Some sources provide a plain notice without timestamps.
func withPlainNotice(display Display, lrc, yrc string) Display {
	if display.HasSinging() || display.unavailable {
		return display
	}
	for _, source := range []string{lrc, yrc} {
		for raw := range strings.Lines(source) {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
			if text != "" && text[0] != '[' && text[0] != '{' && lineKind(text) == unavailable {
				display.unavailable = true
				return display
			}
		}
	}
	return display
}

// BuildDisplay returns the shared singing timeline and the reading order with credits.
func BuildDisplay(lrc, translation, yrc string) ([]Line, Display) {
	display := buildDisplay(lrc, translation, yrc, yrcCredits(yrc), true)
	return display.SungLines(), display
}
