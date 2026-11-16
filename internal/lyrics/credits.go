package lyrics

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type creditStrength uint8

const (
	creditNone creditStrength = iota
	creditWeak
	creditStrong
)

type creditEvidence struct {
	role, value string
	strength    creditStrength
}

func foldCreditRune(r rune) rune {
	if r >= '\uff01' && r <= '\uff5e' {
		r -= 0xfee0
	}
	return unicode.ToLower(r)
}

func compactField(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return foldCreditRune(r)
	}, text)
}

// IsCredit recognizes unambiguous standalone credits. Classifying a source
// also accounts for word timing, repeated lyrics and surrounding credits.
func IsCredit(text string) bool {
	return detectCredit(text).strength == creditStrong
}

func creditField(text string, includeWeak bool) (name, value string, ok bool) {
	e := detectCredit(text)
	return e.role, e.value, e.strength == creditStrong || includeWeak && e.strength == creditWeak
}

// Normalize a matching copy. The original text is still used for display.
func detectCredit(text string) creditEvidence {
	text = strings.Map(foldCreditRune, strings.TrimFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\ufeff' || r == '\u200b'
	}))
	if head, value, ok := strings.Cut(text, ":"); ok {
		return creditWithValue(head, value)
	}
	// Without a colon, only explicit attribution phrases can start credits.
	text = strings.Join(strings.Fields(text), " ")
	for _, separator := range []string{" by ", " at "} {
		if i := strings.Index(text, separator); i >= 0 {
			if e := creditWithValue(text[:i+len(separator)-1], text[i+len(separator):]); e.strength != creditNone {
				return e
			}
		}
	}
	return creditEvidence{}
}

func creditWithValue(head, value string) creditEvidence {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return creditEvidence{}
	}
	role, strength := creditHead(compactField(head))
	return creditEvidence{role, value, strength}
}

func lookupCreditRole(head string) (string, creditStrength) {
	if role, ok := ambiguousCreditFields[head]; ok {
		if known := creditFields[head]; known != "" {
			role = known
		}
		return role, creditWeak
	}
	if role, ok := creditFields[head]; ok {
		return role, creditStrong
	}
	return "", creditNone
}

func creditHead(head string) (string, creditStrength) {
	if head == "" || utf8.RuneCountInString(head) > 160 {
		return "", creditNone
	}
	if role, strength := lookupCreditRole(head); strength != creditNone {
		return role, strength
	}
	if strings.ContainsAny(head, "/&+、") {
		parts := strings.Split(strings.Map(func(r rune) rune {
			switch r {
			case '&', '+', '、':
				return '/'
			}
			return r
		}, head), "/")
		strength := creditStrong
		for i, part := range parts {
			role, partStrength := creditHead(part)
			if partStrength == creditNone {
				return "", creditNone
			}
			parts[i], strength = role, min(strength, partStrength)
		}
		return strings.Join(parts, "/"), strength
	}
	// An unlisted bilingual form is safe when both halves name the same role.
	var previous rune
	for i, r := range head {
		if unicode.Is(unicode.Han, previous) && latinCreditLetter(r) ||
			latinCreditLetter(previous) && unicode.Is(unicode.Han, r) {
			left, ls := lookupCreditRole(head[:i])
			right, rs := lookupCreditRole(head[i:])
			if ls != creditNone && rs != creditNone && left == right {
				return left, min(ls, rs)
			}
		}
		previous = r
	}
	return "", creditNone
}

func latinCreditLetter(r rune) bool { return r >= 'a' && r <= 'z' }

func classifyLines(lines []Line) {
	evidence := make([]creditEvidence, len(lines))
	counts := make(map[creditEvidence]int)
	for i := range lines {
		line := &lines[i]
		line.Kind, line.classified = lyricNoticeKind(line.Text), true
		if line.Kind != Sung {
			continue
		}
		e := detectCredit(line.Text)
		if e.strength != creditNone {
			evidence[i] = e
			counts[e]++
		}
	}
	for i, e := range evidence {
		if e.strength == creditStrong && !lines[i].Timed && counts[e] == 1 {
			lines[i].Kind = Credit
		}
	}
	classifyCreditEdge(lines, evidence, counts, 0, 1)
	classifyCreditEdge(lines, evidence, counts, len(lines)-1, -1)
}

// Require two distinct anchors at an edge. Weak roles normally need an
// anchor on both sides and must have no word timing or repeated text.
func classifyCreditEdge(lines []Line, evidence []creditEvidence, counts map[creditEvidence]int, start, step int) {
	first, last := -1, -1
	distinct := false
	i := start
	for ; i >= 0 && i < len(lines); i += step {
		if evidence[i].strength == creditNone && lines[i].Kind == Sung {
			break
		}
		if i != start && distance(lines[i].At, lines[i-step].At) > 10*time.Second {
			break
		}
		if evidence[i].strength == creditStrong {
			if first == -1 {
				first = i
			}
			last = i
			distinct = distinct || evidence[first] != evidence[i]
		}
	}
	if !distinct {
		return
	}
	// An opening vocal credit can also end the block when a clear pause
	// separates it from the first lyric, as in ReDreaming Angel.
	if step == 1 && i == last+2 && i < len(lines) && evidence[i-1].role == "演唱" &&
		evidence[i].strength == creditNone && lines[i].Kind == Sung && lines[i].At-lines[i-1].At >= 4*time.Second {
		last = i - 1
	}
	for i := min(first, last); i <= max(first, last); i++ {
		e := evidence[i]
		if e.strength == creditStrong || e.strength == creditWeak && !lines[i].Timed && counts[e] == 1 {
			lines[i].Kind = Credit
		}
	}
}

// Some instrumental tracks mark a production credit as a complete YRC line.
// Matching explicit roles and contributors in LRC confirms that it is metadata.
func confirmTimedCredits(original, words []Line) {
	contributor := func(value string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				return -1
			}
			return r
		}, value)
	}
	var confirmed map[creditEvidence]bool
	var openingRoles map[string]bool
	var firstLyric time.Duration
	for i := range words {
		if words[i].Kind != Sung || !words[i].Timed {
			continue
		}
		e := detectCredit(words[i].Text)
		if e.strength != creditStrong {
			continue
		}
		if confirmed == nil {
			confirmed = make(map[creditEvidence]bool)
			openingRoles = make(map[string]bool)
			beforeLyrics := true
			for _, line := range original {
				if beforeLyrics && usableSungLine(line) {
					firstLyric, beforeLyrics = line.At, false
				}
				if line.Kind == Credit {
					lrc := detectCredit(line.Text)
					lrc.value = contributor(lrc.value)
					if lrc.strength == creditStrong && lrc.value != "" {
						confirmed[lrc] = true
						if beforeLyrics {
							openingRoles[lrc.role] = true
						}
					}
				}
			}
		}
		e.value = contributor(e.value)
		if confirmed[e] {
			words[i].Kind = Credit
		}
	}
	if firstLyric > 0 && len(openingRoles) >= 2 {
		confirmOpeningTimedCredits(words, openingRoles, firstLyric)
	}
}

// A matching contributor anchors an opening block even when another role
// lists its contributors differently across the two sources.
func confirmOpeningTimedCredits(words []Line, roles map[string]bool, firstLyric time.Duration) {
	for start := 0; start < len(words) && words[start].At < firstLyric; {
		end := start
		var firstRole string
		distinct, anchored := false, false
		for end < len(words) {
			line := words[end]
			if !line.Timed || line.End > firstLyric || end > start && line.At-words[end-1].End > 10*time.Second {
				break
			}
			e := detectCredit(line.Text)
			if e.strength != creditStrong || !roles[e.role] {
				break
			}
			if end == start {
				firstRole = e.role
			}
			distinct = distinct || e.role != firstRole
			anchored = anchored || line.Kind == Credit
			end++
		}
		if distinct && anchored {
			for i := start; i < end; i++ {
				words[i].Kind = Credit
			}
		}
		start = max(start+1, end)
	}
}
