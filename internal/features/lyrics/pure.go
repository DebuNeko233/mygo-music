/*
从已解析歌词生成纯净显示，借助歌曲资料去除标题和演唱标记，保留原始时间与文本。
*/

package lyrics

import (
	"strings"
	"time"
	"unicode"
)

// SongInfo supplies independent evidence for titles and performer labels.
// It is used during preparation only; the presentation retains no song object.
type SongInfo struct {
	Title   string
	Artists []string
}

// Pure returns a read-only presentation. Unchanged rows and word slices are
// shared; removed labels copy only the affected row/words. Source stays intact.
func (d Display) Pure(info SongInfo) Display {
	people := make(map[string]bool, len(info.Artists))
	for _, name := range info.Artists {
		if key := compactField(name); key != "" {
			people[key] = true
		}
	}
	instrumental := false
	for _, line := range d.Lines {
		instrumental = instrumental || line.Kind == Instrumental
		if line.Kind == Credit {
			role, value, ok := creditField(line.Text, true)
			if ok && vocalCreditRole(role) {
				for _, name := range splitPeople(value) {
					if key := compactField(name); key != "" {
						people[key] = true
					}
				}
			}
		}
	}
	title := compactField(info.Title)
	titleCount := 0
	for _, line := range d.Lines {
		if line.Kind == Sung && title != "" && compactField(unquoteTitle(line.Text)) == title {
			titleCount++
		}
	}
	var rows []Line
	changed := false
	sungAt := 0
	for i, original := range d.Lines {
		line, keep := original, original.Kind == Sung
		if keep {
			edge := sungAt < 3 || sungAt >= len(d.sung)-3
			if edge && pureTitle(original, i, info.Title, people, d.Lines, titleCount) {
				keep = false
			} else {
				line, keep = pureSpeaker(original, people)
			}
			sungAt++
		}
		if !changed && (!keep || line.Text != original.Text || line.Translation != original.Translation) {
			rows = make([]Line, 0, len(d.sung))
			rows = append(rows, d.Lines[:i]...)
			changed = true
		}
		if changed && keep {
			rows = append(rows, line)
		}
	}
	if !changed {
		result := d
		result.noSinging = !d.HasSinging() && !instrumental
		return result
	}
	result := newDisplay(rows)
	result.unavailable = d.unavailable
	result.noSinging = !result.HasSinging() && !instrumental
	return result
}

func splitPeople(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return strings.ContainsRune("/&+、,，", r)
	})
}

func knownPeople(text string, people map[string]bool) bool {
	text = strings.TrimSpace(text)
	if people[compactField(text)] {
		return true
	}
	// A location after a credited name describes the performer, not the verse.
	if at := strings.IndexAny(text, "(（"); at > 0 && (strings.HasSuffix(text, ")") || strings.HasSuffix(text, "）")) {
		if people[compactField(text[:at])] {
			return true
		}
	}
	parts := splitPeople(text)
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !knownPeople(part, people) {
			return false
		}
	}
	return true
}

func unquoteTitle(text string) string {
	text = strings.TrimSpace(text)
	for _, pair := range [][2]string{{"《", "》"}, {"「", "」"}, {"『", "』"}, {"“", "”"}, {"\"", "\""}, {"[", "]"}} {
		if strings.HasPrefix(text, pair[0]) && strings.HasSuffix(text, pair[1]) {
			return strings.TrimSuffix(strings.TrimPrefix(text, pair[0]), pair[1])
		}
	}
	return text
}

func pureTitle(line Line, index int, title string, people map[string]bool, source []Line, titleCount int) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	text, title := strings.TrimSpace(line.Text), compactField(title)
	for _, separator := range []string{" - ", " – ", " — ", "-", "–", "—"} {
		left, right, ok := strings.Cut(text, separator)
		if ok && (compactField(left) == title && knownPeople(right, people) || compactField(right) == title && knownPeople(left, people)) {
			return true
		}
	}
	if titleCount != 1 || compactField(unquoteTitle(text)) != title {
		return false
	}
	before, after := 0, 0
	for i := index - 1; i >= 0 && source[i].Kind != Sung; i-- {
		if source[i].Kind == Credit {
			before++
		}
	}
	next := time.Duration(-1)
	for i := index + 1; i < len(source); i++ {
		if source[i].Kind == Sung {
			next = source[i].At
			break
		}
		if source[i].Kind == Credit {
			after++
		}
	}
	// Repeated titles and bare titles in a verse are never removed. A unique
	// title before two credits, or a decorated title bounding a credit block,
	// has independent metadata evidence beyond the words of the title itself.
	return after >= 2 || !line.Timed && unquoteTitle(text) != text && before >= 2 && (next < 0 || next-line.At >= 4*time.Second)
}

func speakerBody(text string, people map[string]bool) (body, prefix string, ok bool) {
	at := strings.IndexAny(text, ":：∶")
	if at < 0 {
		return "", "", false
	}
	head := compactField(text[:at])
	known := knownPeople(text[:at], people)
	if !known {
		switch head {
		case "男", "女", "合", "男声", "女声", "男聲", "女聲", "全体男声", "全体女声", "全体合唱",
			"演唱", "主唱", "合唱", "和声", "和聲", "合声", "合聲", "说唱", "說唱", "rap", "chorus", "sing", "vocal", "vocals":
			known = true
		}
	}
	if !known {
		return "", "", false
	}
	for offset, r := range text[at:] {
		if offset == 0 || unicode.IsSpace(r) {
			continue
		}
		return text[at+offset:], text[:at+offset], true
	}
	return "", text, true
}

func pureSpeaker(line Line, people map[string]bool) (Line, bool) {
	body, prefix, ok := speakerBody(line.Text, people)
	if !ok {
		return line, true
	}
	if strings.TrimSpace(body) == "" {
		return line, false
	}
	// A performer attribution can itself be an unclassified source row.
	head := compactField(strings.TrimSuffix(strings.TrimSpace(prefix), ":"))
	head = strings.TrimRight(head, ":")
	if knownPeople(body, people) && (head == "演唱" || head == "主唱" || head == "vocal" || head == "vocals") {
		return line, false
	}
	if len(line.Words) > 0 {
		words, ok := trimSpeakerWords(line.Words, prefix, body)
		if !ok {
			return line, true // preserve an uncertain alignment
		}
		line.Words = words
	}
	line.Text = body
	if translated, _, ok := speakerBody(line.Translation, people); ok {
		line.Translation = translated
	}
	return line, true
}

func trimSpeakerWords(words []Word, prefix, body string) ([]Word, bool) {
	var wanted []rune
	for _, r := range prefix {
		if !unicode.IsSpace(r) {
			wanted = append(wanted, foldCreditRune(r))
		}
	}
	matched := 0
	for i, word := range words {
		for at, r := range word.Text {
			if unicode.IsSpace(r) {
				continue
			}
			if matched < len(wanted) {
				if foldCreditRune(r) != wanted[matched] {
					return nil, false
				}
				matched++
				continue
			}
			out := append([]Word(nil), words[i:]...)
			out[0].Text = word.Text[at:]
			var text strings.Builder
			for _, w := range out {
				text.WriteString(w.Text)
			}
			return out, compactField(text.String()) == compactField(body)
		}
	}
	return nil, false
}
