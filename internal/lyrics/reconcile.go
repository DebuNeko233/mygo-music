package lyrics

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Treat known release-note fragments as credits only if the song has an
// explicit instrumental notice.
func classifyInstrumentalNotes(sources ...[]Line) {
	hasNotice := false
	for _, lines := range sources {
		if slices.ContainsFunc(lines, func(line Line) bool { return line.Kind == Instrumental }) {
			hasNotice = true
			break
		}
	}
	if !hasNotice {
		return
	}
	for _, lines := range sources {
		for i := range lines {
			if lines[i].Kind == Sung && isInstrumentalNote(lines[i].Text) {
				lines[i].Kind = Credit
			}
		}
	}
}

func buildDisplay(lrc, translation, yrc string, metadata []Line, includeMetadata bool) Display {
	original := slices.DeleteFunc(parseLRC(lrc), func(line Line) bool { return line.Text == "" })
	words := parseYRC(yrc)
	translations := parseLRC(translation)
	for _, source := range [][]Line{original, words, translations} {
		for i := range source {
			source[i].Kind = lineKind(source[i].Text)
		}
	}
	classifyInstrumentalNotes(original, words, metadata)

	lines := selectLyricSource(original, words, translations)
	end := maxLyricTime
	for i := len(lines) - 1; i >= 0; i-- {
		line := &lines[i]
		if i+1 < len(lines) && lines[i+1].At > line.At {
			end = lines[i+1].At
		}
		if line.Kind == Sung && !line.Timed && len(line.Words) == 0 {
			line.End = min(end, line.At+6*time.Second)
			line.Words = SpreadWords(line.Text, line.At, line.End)
		}
	}
	if includeMetadata {
		lines = append(lines, metadata...)
	}
	return withPlainNotice(newDisplay(lines), lrc, yrc)
}

// Pick one singing timeline. Keep static credits and notices from either
// source, and let display construction deduplicate them.
func selectLyricSource(original, words, translations []Line) []Line {
	sung := func(line Line) bool { return line.Kind == Sung }
	useWords := slices.ContainsFunc(words, func(line Line) bool { return sung(line) && line.Timed }) ||
		(!slices.ContainsFunc(original, sung) && slices.ContainsFunc(words, sung))
	if !useWords {
		attachTranslations(original, nil, translations)
		return appendMetadata(original, words)
	}
	attachTranslations(words, original, translations)
	return appendMetadata(words, original)
}

func appendMetadata(lines, other []Line) []Line {
	count := len(lines)
	for _, line := range other {
		if line.Kind != Sung {
			lines = append(lines, line)
		}
	}
	if len(lines) != count {
		slices.SortStableFunc(lines, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	}
	return lines
}

type translationCue struct {
	line       *Line
	sourceTime time.Duration
	anchor     int // one-based translation index for an exact match
}

// The selected source fixes the on-screen rows. Exact LRC text may supply
// an alternate clock for translations of YRC rows, without supplying text.
func attachTranslations(lines, original, translations []Line) {
	if len(translations) == 0 {
		return
	}
	cues := make([]translationCue, 0, len(lines))
	for i := range lines {
		if lines[i].Kind == Sung {
			cues = append(cues, translationCue{line: &lines[i], sourceTime: lines[i].At})
		}
	}
	exact := make(map[time.Duration][]int)
	for i, line := range translations {
		if sungTranslation(line) {
			exact[line.At] = append(exact[line.At], i)
		}
	}
	used := make([]bool, len(translations))
	matchExact := func(cue *translationCue, at time.Duration) {
		if indices := exact[at]; cue.anchor == 0 && len(indices) > 0 {
			j := indices[0]
			exact[at] = indices[1:]
			cue.line.Translation, cue.anchor, used[j] = translations[j].Text, j+1, true
		}
	}
	pairs := matchTranslationClocks(cues, original)
	for _, pair := range pairs {
		cue := &cues[pair.left]
		cue.sourceTime = original[pair.right].At
		matchExact(cue, cue.sourceTime)
	}
	// The translation endpoint supplies LRC-time cues. Reserve the exact
	// translations of omitted LRC rows before matching the YRC clock.
	last := 0
	for _, pair := range pairs {
		reserveOmittedTranslations(original[last:pair.right], exact, used)
		last = pair.right + 1
	}
	reserveOmittedTranslations(original[last:], exact, used)
	for i := range cues {
		matchExact(&cues[i], cues[i].line.At)
	}
	// Credits with unrecognized labels can have exact translations too.
	attachCreditTranslations(lines, translations, used)
	attachCreditTranslations(original, translations, used)
	attachNearbyTranslations(cues, translations, used)
}

func reserveOmittedTranslations(lines []Line, exact map[time.Duration][]int, used []bool) {
	for _, line := range lines {
		if line.Kind != Sung {
			continue
		}
		if indices := exact[line.At]; len(indices) > 0 {
			used[indices[0]] = true
			exact[line.At] = indices[1:]
		}
	}
}

func sungTranslation(line Line) bool { return line.Kind == Sung && line.Text != "" }

func attachNearbyTranslations(cues []translationCue, translations []Line, used []bool) {
	if !slices.ContainsFunc(cues, func(c translationCue) bool { return c.anchor == 0 }) {
		return
	}
	// Each exact pair bounds approximate matches on either side. Exact
	// clocks can disagree about ordering, leaving no safe gap between them.
	upper := make([]int, len(cues))
	limit := len(translations)
	for i := len(cues) - 1; i >= 0; i-- {
		upper[i] = limit
		if cues[i].anchor != 0 {
			limit = min(limit, cues[i].anchor-1)
		}
	}
	const tolerance = 1500 * time.Millisecond
	lower := 0
	pairs := orderedLyricPairs(len(cues), len(translations), func(i int, add func(int, time.Duration) bool) {
		if cues[i].anchor != 0 {
			lower = max(lower, cues[i].anchor)
			return
		}
		primary, secondary := cues[i].line.At, cues[i].sourceTime
		low, high := min(primary, secondary), max(primary, secondary)
		visit := func(after, before time.Duration) {
			start := max(lower, sort.Search(len(translations), func(j int) bool { return translations[j].At > after }))
			for j := start; j < upper[i] && translations[j].At < before; j++ {
				if tr := translations[j]; !used[j] && sungTranslation(tr) && !add(j, min(distance(tr.At, primary), distance(tr.At, secondary))) {
					break
				}
			}
		}
		if high-low < 2*tolerance {
			visit(low-tolerance, high+tolerance)
		} else {
			visit(low-tolerance, low+tolerance)
			visit(high-tolerance, high+tolerance)
		}
	})
	for _, pair := range pairs {
		cues[pair.left].line.Translation = translations[pair.right].Text
		used[pair.right] = true
	}
}

// A translated credit can have an unrecognized field name. Require an exact
// timestamp for that case and let the singing timeline claim its pairs first.
func attachCreditTranslations(lines, translations []Line, used []bool) {
	for i, line := range lines {
		if line.Kind != Credit || line.Translation != "" {
			continue
		}
		start := sort.Search(len(translations), func(j int) bool { return translations[j].At >= line.At })
		for j := start; j < len(translations) && translations[j].At == line.At; j++ {
			tr := translations[j]
			if used[j] || tr.Text == "" || tr.Kind == Instrumental || tr.Kind == unavailable {
				continue
			}
			lines[i].Translation, used[j] = tr.Text, true
			break
		}
	}
}

func distance(a, b time.Duration) time.Duration { return max(a-b, b-a) }

func lyricMatchKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}

func matchTranslationClocks(cues []translationCue, original []Line) []lyricPair {
	if len(cues) == 0 || len(original) == 0 {
		return nil
	}
	byText := make(map[string][]int)
	for i, line := range original {
		if line.Kind == Sung {
			if key := lyricMatchKey(line.Text); key != "" {
				byText[key] = append(byText[key], i)
			}
		}
	}
	const window = 5 * time.Second
	// Equal timestamps and identical text carry no distinguishing timing
	// information. Use source order for this dense case.
	if len(byText) == 1 {
		key := lyricMatchKey(cues[0].line.Text)
		indices := byText[key]
		if len(indices) != 0 &&
			distance(cues[0].line.At, original[indices[0]].At) < window &&
			original[indices[0]].At == original[indices[len(indices)-1]].At &&
			slices.IndexFunc(cues, func(c translationCue) bool {
				return c.line.At != cues[0].line.At || lyricMatchKey(c.line.Text) != key
			}) < 0 {
			pairs := make([]lyricPair, min(len(cues), len(indices)))
			for i := range pairs {
				pairs[i] = lyricPair{i, indices[i]}
			}
			return pairs
		}
	}
	return orderedLyricPairs(len(cues), len(original), func(i int, add func(int, time.Duration) bool) {
		at := cues[i].line.At
		indices := byText[lyricMatchKey(cues[i].line.Text)]
		start := sort.Search(len(indices), func(j int) bool { return original[indices[j]].At > at-window })
		for _, j := range indices[start:] {
			if original[j].At >= at+window || !add(j, distance(at, original[j].At)) {
				break
			}
		}
	})
}

type lyricPair struct{ left, right int }

// Choose the most one-to-one pairs in source order, then the smallest total
// time difference. Each candidate extends the best path ending to its left
// in a Fenwick tree. Deferring updates for one row prevents pairing it twice.
func orderedLyricPairs(left, right int, candidates func(int, func(int, time.Duration) bool)) []lyricPair {
	type path struct {
		lyricPair
		previous, count int
		cost            time.Duration
	}
	paths := []path{{}}
	tree := make([]int, right+1)
	better := func(a, b int) bool {
		x, y := paths[a], paths[b]
		if x.count != y.count {
			return x.count > y.count
		}
		if x.cost != y.cost {
			return x.cost < y.cost
		}
		return a < b
	}
	var i int
	// Limit candidates per row to keep dense input linear in row count.
	// Candidates arrive in source order so repeated rows keep their pairing.
	const candidatesPerRow = 64
	var rowStart int
	add := func(j int, cost time.Duration) bool {
		if len(paths)-rowStart == candidatesPerRow {
			return false
		}
		previous := 0
		for n := j; n > 0; n -= n & -n {
			if better(tree[n], previous) {
				previous = tree[n]
			}
		}
		p := paths[previous]
		paths = append(paths, path{lyricPair{i, j}, previous, p.count + 1, p.cost + cost})
		return true
	}
	best := 0
	for i = 0; i < left; i++ {
		rowStart = len(paths)
		candidates(i, add)
		for k := rowStart; k < len(paths); k++ {
			for n := paths[k].right + 1; n <= right; n += n & -n {
				if better(k, tree[n]) {
					tree[n] = k
				}
			}
			if better(k, best) {
				best = k
			}
		}
	}
	pairs := make([]lyricPair, paths[best].count)
	for i := len(pairs) - 1; i >= 0; i-- {
		pairs[i] = paths[best].lyricPair
		best = paths[best].previous
	}
	return pairs
}
