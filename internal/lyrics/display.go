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

const InstrumentalText = "纯音乐，请欣赏"

// Display keeps the reading order and the playback timeline separate.
type Display struct {
	Lines []Line
	sung  []int
}

func (d Display) HasSinging() bool { return len(d.sung) > 0 }

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
	var d Display
	var seen map[string]struct{}
	hasNotice := false
	for _, source := range [][]Line{lines, supplemental} {
		for _, line := range source {
			if lineKind(line.Text) == Instrumental {
				hasNotice = true
				break
			}
		}
		if hasNotice {
			break
		}
	}
	for _, source := range [][]Line{lines, supplemental} {
		for _, line := range source {
			if strings.TrimSpace(line.Text) == "" {
				continue
			}
			line.Kind = lineKind(line.Text)
			if line.Kind == Sung && hasNotice && isInstrumentalNote(line.Text) {
				line.Kind = Credit
			}
			if line.Kind == unavailable {
				continue
			}
			if line.Kind != Sung {

				key := metadataKey(line.Text)
				if line.Kind == Instrumental {
					key = InstrumentalText
					line.Text = InstrumentalText
				}
				if _, exists := seen[key]; exists {
					continue
				}
				if seen == nil {
					seen = make(map[string]struct{})
				}
				seen[key] = struct{}{}
				line.Words = nil
			}
			d.Lines = append(d.Lines, line)
		}
	}
	if len(supplemental) > 0 {
		sort.SliceStable(d.Lines, func(i, j int) bool { return d.Lines[i].At < d.Lines[j].At })
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

func containsSinging(lines []Line) bool {
	for _, line := range lines {
		if strings.TrimSpace(line.Text) != "" && lineKind(line.Text) == Sung {
			return true
		}
	}
	return false
}

// BuildDisplay keeps the player's existing timed lines and prepares the
// playback-pane rows from both sources while the raw payloads are available.
func BuildDisplay(lrc, translation, yrc string) ([]Line, Display) {
	lines := Build(lrc, translation, yrc)
	if len(lines) == 0 || !lines[0].Timed {
		return lines, NewDisplay(lines, yrcCredits(yrc)...)
	}
	metadata := yrcCredits(yrc)
	if containsSinging(lines) {
		for _, line := range parseLRC(lrc) {
			if lineKind(line.Text) != Sung {
				metadata = append(metadata, line)
			}
		}
		return lines, NewDisplay(lines, metadata...)
	}
	return lines, NewDisplay(Build(lrc, translation, ""), append(metadata, lines...)...)
}
