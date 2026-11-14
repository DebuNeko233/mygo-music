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

// EmptyText is the notice for a loaded display with no rows.
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
		if s := strings.TrimSpace(text.String()); s != "" && lineKind(s) != Sung {
			lines = append(lines, Line{At: time.Duration(row.Time) * time.Millisecond, Text: s})
		}
	}
	return lines
}

// NewDisplay builds a playback-pane view from parsed lines and any supplemental metadata.
func NewDisplay(lines []Line, supplemental ...Line) Display {
	d := Display{Lines: make([]Line, 0, len(lines)+len(supplemental))}
	hasNotice := false
	for _, source := range [][]Line{lines, supplemental} {
		for _, line := range source {
			if strings.TrimSpace(line.Text) == "" {
				continue
			}
			line.Kind = lineKind(line.Text)
			if line.Kind == unavailable {
				d.unavailable = true
				continue
			}
			hasNotice = hasNotice || line.Kind == Instrumental
			d.Lines = append(d.Lines, line)
		}
	}
	var seen map[string]int
	rows := d.Lines[:0]
	for _, line := range d.Lines {
		if line.Kind == Sung && hasNotice && isInstrumentalNote(line.Text) {
			line.Kind = Credit
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
	clear(d.Lines[len(rows):])
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
	if !d.HasSinging() {
		// For static content the instrumental notice heads the credits.
		for i, line := range d.Lines {
			if line.Kind == Instrumental {
				copy(d.Lines[1:i+1], d.Lines[:i])
				d.Lines[0] = line
				break
			}
		}
	}
	return d
}

// Some sources provide a plain notice without timestamps.
func withPlainNotice(display Display, lrc, yrc string) Display {
	if len(display.Lines) != 0 || display.unavailable {
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

// BuildDisplay keeps the player's existing timed lines and prepares the
// playback-pane rows from both sources while the raw payloads are available.
func BuildDisplay(lrc, translation, yrc string) ([]Line, Display) {
	translations := parseLRC(translation)
	lines := build(lrc, translations, yrc)
	metadata := yrcCredits(yrc)
	if len(lines) == 0 || !lines[0].Timed {
		return lines, withPlainNotice(NewDisplay(lines, metadata...), lrc, yrc)
	}
	original := parseLRC(lrc)
	attachTranslations(original, translations)
	credits := metadata
	for _, line := range original {
		if lineKind(line.Text) != Sung {
			credits = append(credits, line)
		}
	}
	// Use the display classification, which also recognizes prose alongside an instrumental notice.
	display := NewDisplay(lines, credits...)
	if display.HasSinging() {
		return lines, display
	}
	// The fallback already includes every LRC row, including its credits.
	return lines, withPlainNotice(NewDisplay(build(lrc, translations, ""), append(metadata, lines...)...), lrc, yrc)
}
