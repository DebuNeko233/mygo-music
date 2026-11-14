package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A Line is a line of timed lyrics, with its translation if any, and
// when each of its words is sung.
type Line struct {
	At          time.Duration
	End         time.Duration
	Text        string
	Translation string
	Words       []Word
	Kind        LineKind // presentation role assigned by NewDisplay
	// Timed says the words carry their own times, as NetEase's word-timed
	// lyrics give them; else they share the line's time evenly.
	Timed bool
}

// A Word is a word of a line, or a character of Chinese or Japanese,
// with when it is sung.
type Word struct {
	Text       string
	Start, End time.Duration
}

var (
	lrcTime   = regexp.MustCompile(`\[(\d+):(\d+)(?:[.:](\d+))?\]`)
	lrcOffset = regexp.MustCompile(`\[offset:\s*([+-]?\d+)\s*\]`)
)

// lrcOffsetOf is the shift the lyrics ask for, in milliseconds: with
// [offset:+500] every line comes half a second later than it says, as for
// a file that starts before the part of the recording the times count.
func lrcOffsetOf(lrc string) time.Duration {
	m := lrcOffset.FindStringSubmatch(lrc)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return time.Duration(n) * time.Millisecond
}

// parseLRC reads lyrics in the LRC format, as NetEase serves them: a line
// may carry several times, and credits come as lines of their own.
func parseLRC(lrc string) []Line {
	var out []Line
	offset := lrcOffsetOf(lrc)
	for _, raw := range strings.Split(lrc, "\n") {
		times := lrcTime.FindAllStringSubmatchIndex(raw, -1)
		if len(times) == 0 {
			continue
		}
		text := strings.TrimSpace(raw[times[len(times)-1][1]:])
		for _, m := range times {
			min, err := strconv.Atoi(raw[m[2]:m[3]])
			if err != nil || min < 0 {
				continue // Skip malformed timestamps
			}
			sec, err := strconv.Atoi(raw[m[4]:m[5]])
			if err != nil || sec < 0 || sec > 59 {
				continue // Skip malformed timestamps
			}
			var frac time.Duration
			if m[6] >= 0 {
				f := raw[m[6]:m[7]]
				n, err := strconv.Atoi(f)
				if err != nil || n < 0 {
					continue // Skip malformed fractional seconds
				}
				frac = time.Duration(n) * time.Second
				for range len(f) {
					frac /= 10
				}
			}
			at := time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + frac + offset
			// Prevent overflow: max 24 hours
			if at > 24*time.Hour {
				continue
			}
			out = append(out, Line{At: max(at, 0), Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

var (
	yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	yrcWord = regexp.MustCompile(`\((\d+),(\d+),\d+\)`)
)

// parseYRC reads NetEase's word-timed lyrics: a line is
// "[start,length](start,length,0)word(start,length,0)word…", in
// milliseconds. Credits come as lines of JSON, which it leaves out.
func parseYRC(yrc string) []Line {
	ms := func(s string) time.Duration {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0 // Invalid timestamps become 0
		}
		d := time.Duration(n) * time.Millisecond
		// Prevent overflow: max 24 hours
		if d > 24*time.Hour {
			return 0
		}
		return d
	}
	var out []Line
	for _, raw := range strings.Split(yrc, "\n") {
		m := yrcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		l := Line{At: ms(m[1]), Timed: true}
		l.End = l.At + ms(m[2])
		// Each word runs from the mark before it to the mark of the next,
		// brackets and all: a word's own text is as it is sung.
		body := raw[len(m[0]):]
		marks := yrcWord.FindAllStringSubmatchIndex(body, -1)
		for i, w := range marks {
			text := body[w[1]:]
			if i+1 < len(marks) {
				text = body[w[1]:marks[i+1][0]]
			}
			start := ms(body[w[2]:w[3]])
			word := Word{Text: text, Start: start, End: start + ms(body[w[4]:w[5]])}
			if strings.TrimSpace(word.Text) == "" {
				// A space between words: it belongs to the word before.
				if n := len(l.Words); n > 0 {
					l.Words[n-1].Text += " "
				}
				continue
			}
			l.Words = append(l.Words, word)
		}
		for _, w := range l.Words {
			l.Text += w.Text
		}
		l.Text = strings.TrimSpace(l.Text)
		if l.Text != "" {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Build makes the lines to show from what NetEase serves: the
// word-timed lyrics where there are some, else the timed lines, with the
// translation of each.
func Build(lrc, translation, yrc string) []Line {
	return build(lrc, parseLRC(translation), yrc)
}

func build(lrc string, translations []Line, yrc string) []Line {
	var lines []Line
	if yrc != "" {
		lines = parseYRC(yrc)
	}
	if len(lines) == 0 {
		for _, l := range parseLRC(lrc) {
			if l.Text != "" {
				lines = append(lines, l)
			}
		}
		for i := range lines {
			end := lines[i].At + 6*time.Second
			if i+1 < len(lines) {
				end = min(end, lines[i+1].At)
			}
			lines[i].End = end
			lines[i].Words = SpreadWords(lines[i].Text, lines[i].At, end)
		}
	}
	attachTranslations(lines, translations)
	return lines
}

func attachTranslations(lines, translations []Line) {
	if len(lines) == 0 {
		return
	}
	// Translations go to the line nearest their time: word-timed lines
	// start a little off the timed lines the translation follows.
	// Both sources are sorted. Visit each timestamp at most once, and
	// keep the earlier row when distances or timestamps are equal.
	best := 0
	next := 1
	for _, t := range translations {
		if t.Text == "" {
			continue
		}
		for next < len(lines) {
			if lines[next].At == lines[best].At {
				next++
				continue
			}
			if t.At-lines[best].At <= lines[next].At-t.At {
				break
			}
			best, next = next, next+1
		}
		gap := lines[best].At - t.At
		if gap > -1500*time.Millisecond && gap < 1500*time.Millisecond && lines[best].Translation == "" {
			lines[best].Translation = t.Text
		}
	}
}

// mergeLyrics pairs timed lines with their translations, without words'
// own times.
func mergeLyrics(lrc, translation string) []Line {
	return Build(lrc, translation, "")
}

// SpreadWords splits text into words, each Chinese, Japanese or Korean
// character one of them, and shares the time from start to end among them
// by their lengths, for lines whose words have no times of their own.
func SpreadWords(text string, start, end time.Duration) []Word {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if cur.Len() > 0 {
				cur.WriteRune(' ')
				flush()
			}
		case isCJK(r):
			flush()
			words = append(words, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(words) == 0 {
		return nil
	}
	total := 0
	for _, w := range words {
		total += utf8.RuneCountInString(strings.TrimSpace(w))
	}
	// Singing takes most of the line, not the pause before the next.
	span := time.Duration(float64(end-start) * 0.85)
	out := make([]Word, len(words))
	at := start
	for i, w := range words {
		d := span * time.Duration(utf8.RuneCountInString(strings.TrimSpace(w))) / time.Duration(max(total, 1))
		out[i] = Word{Text: w, Start: at, End: at + d}
		at += d
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Lang answers "ja" for lyrics in Japanese, any of whose lines has kana,
// and "" for any others, which take the app's language. Their kanji are
// then drawn in a Japanese font along with their kana: in a Chinese app
// they took a Chinese one, heavier and larger than the Japanese font the
// kana fell back to.
func Lang(lines []Line) string {
	for _, l := range lines {
		for _, r := range l.Text {
			if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
				return "ja"
			}
		}
	}
	return ""
}

// Index returns the line sung at pos, or -1 before the first.
func Index(lines []Line, pos time.Duration) int {
	i := sort.Search(len(lines), func(i int) bool { return lines[i].At > pos })
	return i - 1
}

// A Quiet is a stretch of a song with nothing sung: its intro, an
// interlude, or its outro.
type Quiet struct {
	From, To time.Duration
}

// Quiets returns the stretches of a song length long, of at least least,
// in which none of lines is sung. Credits, as 作词 and 作曲, are not sung.
func Quiets(lines []Line, length, least time.Duration) []Quiet {
	var out []Quiet
	add := func(from, to time.Duration) {
		if to-from >= least {
			out = append(out, Quiet{from, to})
		}
	}
	at := time.Duration(0)
	sung := false
	for _, l := range lines {
		if l.Kind != Sung || IsCredit(l.Text) {
			continue
		}
		add(at, l.At)
		at, sung = max(at, l.End), true
	}
	if sung && length > 0 {
		add(at, length)
	}
	return out
}
