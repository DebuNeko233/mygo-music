package lyrics

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

func contextualKind(text string, kind LineKind, hasNotice bool) LineKind {
	if kind == Sung && hasNotice && isInstrumentalNote(text) {
		return Credit
	}
	return kind
}

func buildDisplay(lrc, translation, yrc string, metadata []Line, includeMetadata bool) Display {
	original := slices.DeleteFunc(parseLRC(lrc), func(line Line) bool { return line.Text == "" })
	words := parseYRC(yrc)
	translations := parseLRC(translation)
	hasNotice := slices.ContainsFunc(metadata, func(line Line) bool { return lineKind(line.Text) == Instrumental })
	for _, source := range [][]Line{original, words, translations} {
		for i := range source {
			source[i].Kind = lineKind(source[i].Text)
		}
	}
	isNotice := func(line Line) bool { return line.Kind == Instrumental }
	hasNotice = hasNotice || slices.ContainsFunc(original, isNotice) || slices.ContainsFunc(words, isNotice)
	for _, source := range [][]Line{original, words} {
		for i := range source {
			source[i].Kind = contextualKind(source[i].Text, source[i].Kind, hasNotice)
		}
	}

	merged := reconcileLines(original, words, translations)
	end := maxLyricTime
	for i := len(merged) - 1; i >= 0; i-- {
		line := &merged[i]
		if i+1 < len(merged) && merged[i+1].At > line.At {
			end = merged[i+1].At
		}
		if line.Kind == Sung && !line.Timed && len(line.Words) == 0 {
			line.End = min(end, line.At+6*time.Second)
			line.Words = SpreadWords(line.Text, line.At, line.End)
		}
	}
	if !includeMetadata {
		metadata = nil
	}
	return withPlainNotice(newDisplay(merged, metadata, hasNotice), lrc, yrc)
}

type translationCue struct {
	line     *Line
	wordTime time.Duration
}

// Use the original LRC time for a merged row. Translation tracks commonly
// share that clock even when YRC supplies the visible line and word times.
func attachTranslations(original, words []Line, wordOriginal []int, translations []Line) {
	if len(translations) == 0 {
		return
	}
	cues := make([]translationCue, 0, len(original)+len(words))
	// The map is in source order. Walk both sources once to record each
	// row's LRC clock and, when available, its alternate word clock.
	nextWord := 0
	for i := range original {
		line := &original[i]
		if line.Kind != Sung {
			continue
		}
		wordTime := line.At
		for nextWord < len(wordOriginal) && wordOriginal[nextWord] == 0 {
			nextWord++
		}
		if nextWord < len(wordOriginal) && wordOriginal[nextWord] == i+1 {
			wordTime = words[nextWord].At
			nextWord++
		}
		cues = append(cues, translationCue{line, wordTime})
	}
	for i := range words {
		if words[i].Kind == Sung && (wordOriginal == nil || wordOriginal[i] == 0) {
			cues = append(cues, translationCue{&words[i], words[i].At})
		}
	}
	if len(original) != 0 && len(words) != 0 {
		slices.SortStableFunc(cues, func(a, b translationCue) int { return cmp.Compare(a.line.At, b.line.At) })
	}

	// Exact pairs divide the remaining cues into intervals so proximity
	// cannot cross an exact pair. Equal timestamps keep source order.
	used := make([]bool, len(translations))
	nextCue, nextTranslation := 0, 0
	for i, j := 0, 0; i < len(cues) && j < len(translations); {
		tr := translations[j]
		switch {
		case tr.At < cues[i].line.At || !sungTranslation(tr):
			j++
		case tr.At > cues[i].line.At:
			i++
		default:
			attachNearbyTranslations(cues[nextCue:i], translations[nextTranslation:j], used[nextTranslation:j])
			cues[i].line.Translation, used[j] = tr.Text, true
			i++
			j++
			nextCue, nextTranslation = i, j
		}
	}
	attachNearbyTranslations(cues[nextCue:], translations[nextTranslation:], used[nextTranslation:])
	attachCreditTranslations(original, translations, used)
	attachCreditTranslations(words, translations, used)
}

func sungTranslation(line Line) bool { return line.Kind == Sung && line.Text != "" }

func attachNearbyTranslations(cues []translationCue, translations []Line, used []bool) {
	if len(cues) == 0 || len(translations) == 0 {
		return
	}
	const tolerance = 1500 * time.Millisecond
	pairs := orderedLyricPairs(len(cues), len(translations), func(i int, add func(int, time.Duration)) {
		primary, secondary := cues[i].line.At, cues[i].wordTime
		low, high := min(primary, secondary), max(primary, secondary)
		visit := func(after, before time.Duration) {
			start := sort.Search(len(translations), func(j int) bool { return translations[j].At > after })
			for j := start; j < len(translations) && translations[j].At < before; j++ {
				if tr := translations[j]; sungTranslation(tr) {
					add(j, min(distance(tr.At, primary), distance(tr.At, secondary)))
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

func reconcileLines(original, words, translations []Line) []Line {
	if len(words) == 0 {
		attachTranslations(original, nil, nil, translations)
		return original
	}
	if len(original) == 0 {
		attachTranslations(nil, words, nil, translations)
		return words
	}
	originalKeys, wordKeys := make([]string, len(original)), make([]string, len(words))
	byText := make(map[string][]int)
	for i, line := range original {
		if line.Kind == Sung {
			key := lyricMatchKey(line.Text)
			originalKeys[i] = key
			if key != "" {
				byText[key] = append(byText[key], i)
			}
		}
	}
	for i, line := range words {
		if line.Kind == Sung {
			wordKeys[i] = lyricMatchKey(line.Text)
		}
	}

	const window = 5 * time.Second
	pairs := orderedLyricPairs(len(words), len(original), func(i int, add func(int, time.Duration)) {
		at := words[i].At
		indices := byText[wordKeys[i]]
		start := sort.Search(len(indices), func(j int) bool { return original[indices[j]].At > at-window })
		for _, j := range indices[start:] {
			if original[j].At >= at+window {
				break
			}
			add(j, distance(at, original[j].At))
		}
	})

	matched := make([]bool, len(original))
	wordOriginal := make([]int, len(words))
	matchCount := 0
	merge := func(i, j int) {
		matched[j] = true
		wordOriginal[i] = j + 1
		matchCount++
	}
	wordStart, originalStart := 0, 0
	for _, pair := range pairs {
		mergeSimilarLyrics(words[wordStart:pair.left], original[originalStart:pair.right],
			wordKeys[wordStart:pair.left], originalKeys[originalStart:pair.right],
			func(i, j int) { merge(wordStart+i, originalStart+j) })
		merge(pair.left, pair.right)
		wordStart, originalStart = pair.left+1, pair.right+1
	}
	mergeSimilarLyrics(words[wordStart:], original[originalStart:], wordKeys[wordStart:], originalKeys[originalStart:],
		func(i, j int) { merge(wordStart+i, originalStart+j) })

	attachTranslations(original, words, wordOriginal, translations)
	for i, source := range wordOriginal {
		if source != 0 {
			words[i].Translation = original[source-1].Translation
		}
	}
	lines := slices.Grow(words, len(original)-matchCount)
	for i, line := range original {
		if !matched[i] {
			lines = append(lines, line)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At < lines[j].At })
	return lines
}

// Try small text differences only between exact matches. When either source
// has multiple plausible partners, keep the original rows for that interval.
func mergeSimilarLyrics(words, original []Line, wordKeys, originalKeys []string, merge func(int, int)) {
	if len(words) == 0 || len(original) == 0 {
		return
	}
	const window = 100 * time.Millisecond
	originalUsed := make([]bool, len(original))
	type pair struct{ word, original int }
	var pairs []pair
	for i, line := range words {
		if wordKeys[i] == "" {
			continue
		}
		match := -1
		start := sort.Search(len(original), func(j int) bool { return original[j].At >= line.At-window })
		for j := start; j < len(original) && original[j].At <= line.At+window; j++ {
			if !similarLyricKeys(wordKeys[i], originalKeys[j]) {
				continue
			}
			if match >= 0 || originalUsed[j] {
				return
			}
			match = j
		}
		if match >= 0 {
			if len(pairs) > 0 && pairs[len(pairs)-1].original >= match {
				return
			}
			originalUsed[match] = true
			pairs = append(pairs, pair{i, match})
		}
	}
	for _, pair := range pairs {
		merge(pair.word, pair.original)
	}
}

// Short rows and one-character substitutions are easy to confuse with
// overlapping vocals. Limit this fallback to a single inserted or omitted
// character, or a single replacement in a longer line.
func similarLyricKeys(a, b string) bool {
	if a == b || b == "" {
		return false
	}
	left, right := []rune(a), []rune(b)
	if len(left) > len(right) {
		left, right = right, left
	}
	if len(left) < 5 || len(right)-len(left) > 1 {
		return false
	}
	i := 0
	for i < len(left) && left[i] == right[i] {
		i++
	}
	if len(left) != len(right) {
		return slices.Equal(left[i:], right[i+1:])
	}
	return len(left) >= 10 && slices.Equal(left[i+1:], right[i+1:])
}

type lyricPair struct{ left, right int }

// Choose the most one-to-one pairs in source order, then the smallest total
// time difference. Each candidate extends the best path ending to its left
// in a Fenwick tree. Deferring updates for one row prevents pairing it twice.
func orderedLyricPairs(left, right int, candidates func(int, func(int, time.Duration))) []lyricPair {
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
	add := func(j int, cost time.Duration) {
		previous := 0
		for n := j; n > 0; n -= n & -n {
			if better(tree[n], previous) {
				previous = tree[n]
			}
		}
		p := paths[previous]
		paths = append(paths, path{lyricPair{i, j}, previous, p.count + 1, p.cost + cost})
	}
	best := 0
	for i = 0; i < left; i++ {
		first := len(paths)
		candidates(i, add)
		for k := first; k < len(paths); k++ {
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
