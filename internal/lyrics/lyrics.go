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
	lrcTime   = regexp.MustCompile(`^\[(\d+):(\d+)(?:[.:](\d+))?\]`)
	lrcOffset = regexp.MustCompile(`^\[offset:\s*([+-]?\d+)\s*\]$`)
)

const maxLyricTime = 24 * time.Hour

// lrcOffsetOf is the shift the lyrics ask for, in milliseconds: with
// [offset:+500] every line comes half a second later than it says, as for
// a file that starts before the part of the recording the times count.
func lrcOffsetOf(lrc string) time.Duration {
	for line := range strings.Lines(lrc) {
		tag := lrcOffset.FindStringSubmatch(strings.TrimSpace(strings.TrimPrefix(line, "\ufeff")))
		if tag == nil {
			continue
		}
		n, err := strconv.ParseInt(tag[1], 10, 64)
		if err == nil && n >= -maxLyricTime.Milliseconds() && n <= maxLyricTime.Milliseconds() {
			return time.Duration(n) * time.Millisecond
		}
	}
	return 0
}

// parseLRC consumes only leading timestamps, leaving bracketed text in the lyric intact.
func parseLRC(lrc string) []Line {
	var out []Line
	offset := lrcOffsetOf(lrc)
	for raw := range strings.Lines(lrc) {
		text := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		first := len(out)
		for {
			tag := lrcTime.FindStringSubmatch(text)
			if tag == nil {
				break
			}
			text = strings.TrimSpace(text[len(tag[0]):])
			minutes, minErr := strconv.ParseUint(tag[1], 10, 64)
			seconds, secErr := strconv.ParseUint(tag[2], 10, 64)
			if minErr != nil || secErr != nil || minutes > 1440 || seconds >= 60 {
				continue
			}
			fraction := tag[3]
			fraction = fraction[:min(len(fraction), 9)]
			nanos, _ := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 32)
			at := time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(nanos)
			if at <= maxLyricTime && at+offset <= maxLyricTime {
				out = append(out, Line{At: max(0, at+offset)})
			}
		}
		for i := first; i < len(out); i++ {
			out[i].Text = text
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

var (
	yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	// Recognize signed fields so invalid timings never leak into the lyric text.
	yrcWord = regexp.MustCompile(`\(([+-]?\d+),([+-]?\d+),[+-]?\d+\)`)
)

// parseYRC reads NetEase's word-timed lyrics: a line is
// "[start,length](start,length,0)word(start,length,0)word…", in
// milliseconds. Credits come as lines of JSON, which it leaves out.
func parseYRC(yrc string) []Line {
	ms := func(s string) (time.Duration, bool) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 || n > maxLyricTime.Milliseconds() {
			return 0, false
		}
		return time.Duration(n) * time.Millisecond, true
	}
	var out []Line
	for raw := range strings.Lines(yrc) {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		m := yrcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		at, validStart := ms(m[1])
		length, validLength := ms(m[2])
		if !validStart || !validLength || at+length > maxLyricTime {
			continue
		}
		l := Line{At: at, End: at + length, Timed: true}
		var previous time.Duration
		// Each word runs from the mark before it to the mark of the next,
		// brackets and all: a word's own text is as it is sung.
		body := raw[len(m[0]):]
		marks := yrcWord.FindAllStringSubmatchIndex(body, -1)
		prefix := body
		if len(marks) != 0 {
			prefix = body[:marks[0][0]]
		}
		var text strings.Builder
		text.Grow(len(body))
		text.WriteString(prefix)
		if len(marks) == 0 || strings.TrimSpace(prefix) != "" || l.End == l.At {
			l.Timed = false
		}
		for i, w := range marks {
			part := body[w[1]:]
			if i+1 < len(marks) {
				part = body[w[1]:marks[i+1][0]]
			}
			start, validStart := ms(body[w[2]:w[3]])
			duration, validDuration := ms(body[w[4]:w[5]])
			if !validStart || !validDuration || start < l.At || start < previous || start+duration > l.End {
				l.Timed = false
			}
			previous = start
			word := Word{Text: part, Start: start, End: start + duration}
			if strings.TrimSpace(word.Text) == "" {
				// A space between words: it belongs to the word before.
				if n := len(l.Words); n > 0 {
					l.Words[n-1].Text += " "
					text.WriteByte(' ')
				}
				continue
			}
			text.WriteString(part)
			l.Words = append(l.Words, word)
		}
		l.Text = strings.TrimSpace(text.String())
		if !l.Timed {
			l.Words = nil
			if l.End > l.At {
				l.Words = SpreadWords(l.Text, l.At, l.End)
			}
		}
		if l.Text != "" {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Build returns lyrics from the selected source. Supplemental JSON credits can inform
// classification but are only included as rows by BuildDisplay.
func Build(lrc, translation, yrc string) []Line {
	return buildDisplay(lrc, translation, yrc, yrcCredits(yrc), false).Lines
}

// mergeLyrics pairs timed lines with their translations, without words' own times.
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
