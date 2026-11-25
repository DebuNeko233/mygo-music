/*
协调 LRC、YRC 和翻译来源，选择可用时间线并对齐正文、译文与署名。
*/

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
		classifyLines(source)
	}
	classifyInstrumentalNotes(original, words, metadata)
	confirmTimedCredits(original, words)
	// A word-timed source consisting entirely of explicit credit shapes
	// cannot displace sung LRC. In isolation those rows may still be sung.
	if slices.ContainsFunc(original, usableSungLine) && !slices.ContainsFunc(words, func(line Line) bool {
		return line.Kind == Sung && !IsCredit(line.Text)
	}) {
		for i := range words {
			if words[i].Kind == Sung {
				words[i].Kind = Credit
			}
		}
	}

	lines, alternate := original, words
	useYRC := preferYRC(original, words)
	if useYRC {
		lines, alternate = words, original
	}
	lines = appendMetadata(lines, alternate)
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
	var translationClock []Line
	if useYRC {
		translationClock = original
	}
	attachTranslations(lines, translationClock, translations)
	return withPlainNotice(newDisplay(lines), lrc, yrc)
}

// Punctuation alone cannot displace usable LRC. Once a source is selected,
// keep all its rows, including punctuation and lines without word timings.
func preferYRC(original, words []Line) bool {
	if slices.ContainsFunc(words, func(line Line) bool { return line.Timed && usableSungLine(line) }) {
		return !shortAndDriftedYRC(original, words)
	}
	return !slices.ContainsFunc(original, usableSungLine) && slices.ContainsFunc(words, func(line Line) bool { return line.Kind == Sung })
}

func usableSungLine(line Line) bool {
	return line.Kind == Sung && strings.ContainsFunc(line.Text, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	})
}

// A shorter YRC can be intentional. Fall back only when it omits at least
// four rows and a quarter of the lyrics, and most of its rows are off by
// at least three seconds from every LRC occurrence of the same text.
func shortAndDriftedYRC(original, words []Line) bool {
	countSung := func(lines []Line) int {
		n := 0
		for _, line := range lines {
			if usableSungLine(line) {
				n++
			}
		}
		return n
	}
	nLRC, nYRC := countSung(original), countSung(words)
	needed := max(4, nYRC/2+1)
	if nYRC < needed || nLRC-nYRC < 4 || nYRC*4 > nLRC*3 {
		return false
	}

	byText := make(map[string][]time.Duration)
	for _, line := range original {
		if usableSungLine(line) {
			byText[line.Text] = append(byText[line.Text], line.At)
		}
	}
	const drift = 3 * time.Second
	for _, line := range words {
		times := byText[line.Text]
		if line.Kind != Sung || len(times) == 0 {
			continue
		}
		// Repeated choruses can match any occurrence. Only count a row
		// as drifted if no occurrence is close to its cue.
		i := sort.Search(len(times), func(i int) bool { return times[i] > line.At-drift })
		if i == len(times) || times[i] >= line.At+drift {
			needed--
			if needed == 0 {
				return true
			}
		}
	}
	return false
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

// The selected source fixes the on-screen rows. LRC can supply an
// alternate translation clock for corresponding YRC rows.
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
	variants := matchVariantClocks(cues, original, pairs, exact)
	for _, pair := range variants {
		cue := &cues[pair.left]
		cue.sourceTime = original[pair.right].At
		matchExact(cue, cue.sourceTime)
	}
	if len(variants) > 0 {
		pairs = append(pairs, variants...)
		slices.SortFunc(pairs, func(a, b lyricPair) int { return cmp.Compare(a.right, b.right) })
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
	attachCreditTranslations(lines, translations, used)
	attachNearbyTranslations(cues, translations, used)
}

func reserveOmittedTranslations(lines []Line, exact map[time.Duration][]int, used []bool) {
	for _, line := range lines {
		if line.Kind != Sung {
			continue
		}
		if indices := exact[line.At]; len(indices) > 0 {
			// At most one cue per omitted row is withheld from exact YRC
			// matches. Any surplus at this clock is unsafe for proximity.
			for _, j := range indices {
				if used[j] {
					break
				}
				used[j] = true
			}
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

// Match credit aliases before considering unlabeled translations at the
// same time. A remaining unlabeled translation needs a unique credit.
func attachCreditTranslations(lines, translations []Line, used []bool) {
	credits := make(map[time.Duration][]int)
	for i, line := range lines {
		if line.Kind == Credit {
			credits[line.At] = append(credits[line.At], i)
		}
	}
	for at, rows := range credits {
		start := sort.Search(len(translations), func(j int) bool { return translations[j].At >= at })
		roles := make(map[string][]int)
		var unlabeled []int
		for j := start; j < len(translations) && translations[j].At == at; j++ {
			tr := translations[j]
			if used[j] || tr.Text == "" || tr.Kind == Instrumental || tr.Kind == unavailable {
				continue
			}
			if role, _, ok := creditField(tr.Text, tr.Kind == Credit); ok {
				roles[role] = append(roles[role], j)
			} else {
				unlabeled = append(unlabeled, j)
			}
		}
		if len(roles) == 0 && len(unlabeled) == 0 {
			continue
		}
		seen := make(map[string]bool, len(rows))
		unmatched, remaining := -1, 0
		for _, i := range rows {
			line := &lines[i]
			key := metadataKey(line.Text)
			if seen[key] {
				continue
			}
			seen[key] = true
			role, _, _ := creditField(line.Text, true)
			indices := roles[role]
			if len(indices) == 0 {
				unmatched = i
				remaining++
				continue
			}
			j := indices[0]
			line.Translation, used[j] = translations[j].Text, true
			roles[role] = indices[1:]
		}
		if remaining == 1 && len(unlabeled) == 1 {
			lines[unmatched].Translation = translations[unlabeled[0]].Text
		}
		if remaining > 0 {
			// Keep ambiguous credit translations out of nearby sung rows.
			for _, j := range unlabeled {
				used[j] = true
			}
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
	keys := make([]string, len(cues))
	cueIndices := make(map[string][]int)
	ordinals := make([]int, len(cues))
	for i, cue := range cues {
		key := lyricMatchKey(cue.line.Text)
		keys[i] = key
		if len(byText[key]) > lyricCandidatesPerRow {
			ordinals[i] = len(cueIndices[key])
			cueIndices[key] = append(cueIndices[key], i)
		}
	}
	const window = 5 * time.Second
	return orderedLyricPairs(len(cues), len(original), func(i int, add func(int, time.Duration) bool) {
		at := cues[i].line.At
		key := keys[i]
		indices := byText[key]
		start := sort.Search(len(indices), func(j int) bool { return original[indices[j]].At > at-window })
		end := sort.Search(len(indices), func(j int) bool { return original[indices[j]].At >= at+window })
		limit := min(start+lyricCandidatesPerRow, end)
		fromStart, fromEnd := end, end
		if end > limit {
			// Keep candidates counted from both ends of the window so
			// repeated lines can reach past the first 64 cues.
			peers := cueIndices[key]
			first, last := original[indices[start]].At, original[indices[end-1]].At
			before := ordinals[i] - sort.Search(len(peers), func(j int) bool {
				return cues[peers[j]].line.At > first-window
			})
			after := sort.Search(len(peers), func(j int) bool {
				return cues[peers[j]].line.At >= last+window
			}) - ordinals[i] - 1
			a, b := min(start+before, end-1), max(start, end-1-after)
			fromStart, fromEnd = min(a, b), max(a, b)
			limit -= 2
		}
		// Visit in source order so tied paths still favor earlier rows.
		for _, j := range indices[start:limit] {
			add(j, distance(at, original[j].At))
		}
		if fromStart >= limit && fromStart < end {
			j := indices[fromStart]
			add(j, distance(at, original[j].At))
		}
		if fromEnd >= limit && fromEnd < end && fromEnd != fromStart {
			j := indices[fromEnd]
			add(j, distance(at, original[j].At))
		}
	})
}

// Only borrow a clock when the two rows are mutual, unique neighbors between
// exact text matches. The LRC row must have its own exact translation.
func matchVariantClocks(cues []translationCue, original []Line, anchors []lyricPair, exact map[time.Duration][]int) []lyricPair {
	const tolerance = 100 * time.Millisecond
	var pairs []lyricPair
	left, right := 0, 0
	for n := 0; n <= len(anchors); n++ {
		end := lyricPair{len(cues), len(original)}
		if n < len(anchors) {
			end = anchors[n]
		}
		for i := left; i < end.left && right < end.right; i++ {
			at := cues[i].line.At
			if len(exact[at]) != 0 {
				continue
			}
			j := right + sort.Search(end.right-right, func(k int) bool {
				return original[right+k].At >= at-tolerance
			})
			if j == end.right || original[j].Kind != Sung || original[j].At > at+tolerance ||
				(j+1 < end.right && original[j+1].At <= at+tolerance) {
				continue
			}
			other := original[j].At
			if len(exact[other]) != 1 ||
				(j > 0 && original[j-1].At == other) ||
				(j+1 < len(original) && original[j+1].At == other) ||
				(i > 0 && distance(cues[i-1].line.At, other) <= tolerance) ||
				(i+1 < len(cues) && distance(cues[i+1].line.At, other) <= tolerance) {
				continue
			}
			if oneLyricInsertion(lyricMatchKey(cues[i].line.Text), lyricMatchKey(original[j].Text)) {
				pairs = append(pairs, lyricPair{i, j})
			}
		}
		left, right = end.left+1, end.right+1
	}
	return pairs
}

// Accept a single inserted or omitted letter or number in otherwise identical
// text. Short interjections and one-character substitutions are too ambiguous.
func oneLyricInsertion(a, b string) bool {
	shorter, longer := []rune(a), []rune(b)
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if len(shorter) < 4 || len(longer) != len(shorter)+1 {
		return false
	}
	i := 0
	for i < len(shorter) && shorter[i] == longer[i] {
		i++
	}
	return (unicode.IsLetter(longer[i]) || unicode.IsNumber(longer[i])) && slices.Equal(shorter[i:], longer[i+1:])
}

type lyricPair struct{ left, right int }

const lyricCandidatesPerRow = 64

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
	var rowStart int
	add := func(j int, cost time.Duration) bool {
		if len(paths)-rowStart == lyricCandidatesPerRow {
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
