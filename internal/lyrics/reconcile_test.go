package lyrics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Cached translation timestamps follow LRC even when the closest YRC line is a different lyric.
func TestReconcileCachedTranslationTiming(t *testing.T) {
	// Timing and word boundaries from a local cache excerpt, with the words replaced.
	const lrc = "[02:09.44]甲 乙\n[02:10.22]丙丁戊 己庚辛"
	const trans = "[02:09.44]First translation\n[02:10.22]Second translation"
	const yrc = "[128640,300](128640,0,0)甲(128640,30,0) (128670,270,0)乙\n" +
		"[128940,1860](128940,270,0)丙(129210,240,0)丁(129450,360,0)戊(129810,0,0) (129810,210,0)己(130020,240,0)庚(130260,540,0)辛\n" +
		"[130800,2310](130800,0,0)壬(130800,420,0)癸(131220,420,0)子(131640,1470,0)丑(133110,0,0)寅"
	lines, display := BuildDisplay(lrc, trans, yrc)
	if len(lines) != 3 || lines[0].At != 128640*time.Millisecond || lines[1].At != 128940*time.Millisecond ||
		!lines[0].Timed || !lines[1].Timed || lines[0].Translation != "First translation" ||
		lines[1].Translation != "Second translation" || lines[2].Translation != "" {
		t.Fatalf("translation or word times changed: %+v", lines)
	}
	cur, next := display.Position(129 * time.Second)
	if cur != 1 || next != 2 || display.Lines[cur].Translation != lines[cur].Translation {
		t.Fatalf("playback and display differ at 129s: %d, %d, %+v", cur, next, display.Lines)
	}
}

// A partial YRC keeps its own timeline even when LRC has an extra line.
func TestReconcileCachedPartialYRCUsesOnlyYRC(t *testing.T) {
	// This excerpt preserves the source times, including the handoff between the two formats.
	const lrc = "[00:24.406]短句啊\n[00:24.980]甲乙丙丁戊己庚辛壬\n[00:28.758]子丑寅卯辰巳午未申"
	const yrc = "[25250,3930](25250,380,0)甲(25630,320,0)乙(25950,440,0)丙(26390,360,0)丁(26750,240,0)戊(26990,250,0)己(27240,260,0)庚(27500,390,0)辛(27890,1000,0)壬\n" +
		"[29180,3060](29180,210,0)子(29390,200,0)丑(29590,170,0)寅(29760,310,0)卯(30070,60,0)辰(30130,200,0)巳(30330,380,0)午(30710,420,0)未(31130,830,0)申"
	lines, display := BuildDisplay(lrc, "", yrc)
	if len(lines) != 2 || lines[0].At != 25250*time.Millisecond || lines[1].At != 29180*time.Millisecond ||
		!lines[0].Timed || !lines[1].Timed {
		t.Fatalf("partial YRC imported an LRC row or changed its own timing: %+v", lines)
	}
	for _, tc := range []struct {
		ms      time.Duration
		current int
		next    int
	}{
		{24405, -1, 0},
		{24406, -1, 0},
		{25249, -1, 0},
		{25250, 0, 1},
		{29180, 1, -1},
		{24406, -1, 0}, // seeking backward uses the same boundaries
	} {
		cur, next := display.Position(tc.ms * time.Millisecond)
		if cur != tc.current || next != tc.next {
			t.Errorf("position at %d ms = %d, %d, want %d, %d", tc.ms, cur, next, tc.current, tc.next)
		}
	}
}

// Explicit missing-lyrics metadata cannot suppress a usable word-timed lyric.
func TestReconcileUsesSingingOverUnavailableNotice(t *testing.T) {
	lines, display := BuildDisplay("[00:00]暂无歌词", "[00:10]Translation", "[10000,600](10000,600,0)Singing")
	if len(lines) != 1 || len(display.Lines) != 1 || !display.HasSinging() || lines[0].Translation != "Translation" {
		t.Fatalf("usable word-timed lyric lost behind the missing-lyrics notice: %+v", display.Lines)
	}
}

// Equal timestamps pair in input order so the displayed lyric keeps its translation.
func TestReconcileDuplicateTimestamps(t *testing.T) {
	lines, display := BuildDisplay("[00:10]First\n[00:10]Second", "[00:10]One\n[00:10]Two", "")
	cur, _ := display.Position(10 * time.Second)
	if len(lines) != 2 || cur != 1 || lines[0].Translation != "One" || lines[cur].Translation != "Two" {
		t.Fatalf("duplicate timestamp lost its translation: %+v", lines)
	}
}

// Invalid times cannot create a phantom first line or corrupt a valid line's word timing.
func TestReconcileMalformedTimestamps(t *testing.T) {
	const lrc = "[offset:+9223372036854775807]\n[00:10.12345678901]See [00:20.00] here\n[153722867:10]overflow"
	const yrc = "[86400001,800](86400001,800,0)past limit\n[86400000,1](86400000,1,0)end past limit\n" +
		"[15000,500](15000,250,0)kept (86400001,250,0)as text"
	lines, _ := BuildDisplay(lrc, "", yrc)
	if len(lines) != 1 || lines[0].At != 10*time.Second+123456789*time.Nanosecond ||
		lines[0].Text != "See [00:20.00] here" || lines[0].Timed {
		t.Fatalf("invalid YRC prevented the LRC fallback: %+v", lines)
	}
	lines, _ = BuildDisplay("", "", yrc)
	if len(lines) != 1 || lines[0].At != 15*time.Second || lines[0].End != 15500*time.Millisecond ||
		lines[0].Timed || lines[0].Text != "kept as text" {
		t.Fatalf("invalid word timing discarded usable YRC text: %+v", lines)
	}
	for _, word := range lines[0].Words {
		if word.Start < lines[0].At || word.End > lines[0].End {
			t.Fatalf("fallback word outside the line: %+v", word)
		}
	}
}

// An exact second translation stays fixed while the preceding near match fills the first line.
func TestReconcileTranslationBeforeExactAnchor(t *testing.T) {
	const lrc = "[00:10]First\n[00:11]Second\n[00:13]Untranslated"
	const translation = "[00:10.60]第一句\n[00:11.00]第二句"
	lines, _ := BuildDisplay(lrc, translation, "")
	if len(lines) != 3 || lines[0].Translation != "第一句" || lines[1].Translation != "第二句" || lines[2].Translation != "" {
		t.Fatalf("translations were shifted or dropped: %+v", lines)
	}
}

// A credit at the same time cannot consume the sung translation, and keeps its own labeled translation.
func TestReconcileCreditAtSungTimestamp(t *testing.T) {
	const lrc = "[00:10]作曲：甲\n[00:10]首句"
	for _, tc := range []struct {
		name, translation, credit string
	}{
		{"singing only", "[00:10]First line", ""},
		{"both", "[00:10]Composer: A\n[00:10]First line", "Composer: A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, display := BuildDisplay(lrc, tc.translation, "")
			if len(lines) != 1 || len(display.Lines) != 2 || lines[0].Translation != "First line" ||
				display.Lines[0].Kind != Credit || display.Lines[0].Translation != tc.credit {
				t.Fatalf("credit and sung translations were mixed: %+v", display.Lines)
			}
		})
	}
}

// A row present only in YRC keeps its exact translation when a nearby LRC row has no translation.
func TestReconcileExactTranslationForYRCOnlyRow(t *testing.T) {
	const lrc = "[00:10]LRC only\n[00:14]Same"
	const yrc = "[10600,400](10600,400,0)YRC only\n[14020,400](14020,400,0)Same"
	const translation = "[00:10.60]Only this line\n[00:14]Both sources"
	lines, _ := BuildDisplay(lrc, translation, yrc)
	if len(lines) != 2 || lines[0].Translation != "Only this line" ||
		lines[1].Translation != "Both sources" || !lines[0].Timed || !lines[1].Timed {
		t.Fatalf("nearby LRC row took an exact YRC translation: %+v", lines)
	}
}

// Matching the nearest repetition first used to duplicate the first verse and shift the translations.
func TestReconcileRepeatedLinesInOrder(t *testing.T) {
	const lrc = "[00:10]再见\n[00:11]再见"
	const translation = "[00:10]First\n[00:11]Second"
	const yrc = "[10700,200](10700,200,0)再见\n[11700,200](11700,200,0)再见"
	lines, display := BuildDisplay(lrc, translation, yrc)
	if len(lines) != 2 || !lines[0].Timed || !lines[1].Timed ||
		lines[0].At != 10700*time.Millisecond || lines[1].At != 11700*time.Millisecond ||
		lines[0].Translation != "First" || lines[1].Translation != "Second" {
		t.Fatalf("repeated verses were mispaired: %+v", lines)
	}
	if current, next := display.Position(10700 * time.Millisecond); current != 0 || next != 1 {
		t.Fatalf("wrong playback indices after reconciling repeated verses: %d, %d", current, next)
	}
}

// Two similar lines from the chosen source must both survive, and LRC-only text must stay out.
func TestReconcileYRCTextDoesNotMixOrDeduplicateSources(t *testing.T) {
	const lrc = "[00:08]LRC only\n[00:10]不要忘记我"
	const yrc = "[10050,500](10050,500,0)不要忘记我\n[10100,500](10100,500,0)请不要忘记我"
	lines, _ := BuildDisplay(lrc, "[00:10]Remember me", yrc)
	if len(lines) != 2 || lines[0].Text != "不要忘记我" || lines[0].Translation != "Remember me" ||
		lines[1].Text != "请不要忘记我" || lines[1].Translation != "" {
		t.Fatalf("selected lyrics were mixed or deduplicated: %+v", lines)
	}
}

// A translation for a missing YRC line must not move to a nearby different line.
func TestReconcileDoesNotShiftOmittedTranslation(t *testing.T) {
	const lrc = "[00:10]Omitted\n[00:11]Kept"
	for _, yrc := range []string{"[11000,500](11000,500,0)Kept", "[10000,500](10000,500,0)Kept"} {
		lines, _ := BuildDisplay(lrc, "[00:10]Omitted translation", yrc)
		if len(lines) != 1 || lines[0].Text != "Kept" || lines[0].Translation != "" {
			t.Fatalf("an omitted line's translation shifted to the selected source: %+v", lines)
		}
	}
}

// An unrecognized credit translation with an exact cue cannot drift to nearby singing.
func TestReconcileReservesCreditTranslationBeforeProximity(t *testing.T) {
	lines, display := BuildDisplay("[00:10]作曲：甲\n[00:11]Sung line", "[00:10]Composed by A", "")
	if len(lines) != 1 || lines[0].Translation != "" || len(display.Lines) != 2 ||
		display.Lines[0].Translation != "Composed by A" {
		t.Fatalf("credit translation shifted to singing: %+v", display.Lines)
	}
}

// Invalid word markers leave the row text usable with times bounded by its line cue.
func TestReconcileInvalidYRCWordTiming(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"negative start", "(10000,400,0)前半(-100,100,0)后半"},
		{"negative duration", "(10000,400,0)前半(10400,-100,0)后半"},
		{"backwards start", "(10400,100,0)前半(10000,100,0)后半"},
		{"past line end", "(10000,400,0)前半(11700,200,0)后半"},
		{"missing first marker", "前半(10400,400,0)后半"},
		{"no word markers", "前半后半"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, _ := BuildDisplay("", "[00:10]Translation", "[10000,1800]"+tc.body)
			if len(lines) != 1 || lines[0].Timed || lines[0].Text != "前半后半" ||
				lines[0].At != 10*time.Second || lines[0].End != 11800*time.Millisecond ||
				lines[0].Translation != "Translation" || len(lines[0].Words) == 0 {
				t.Fatalf("invalid YRC did not fall back to line timing: %+v", lines)
			}
			end := lines[0].At
			for _, word := range lines[0].Words {
				if word.Start < end || word.End <= word.Start || word.End > lines[0].End {
					t.Fatalf("invalid fallback word: %+v", word)
				}
				end = word.End
			}
		})
	}
}

// A translation on the YRC clock can still match a line whose LRC cue is farther away.
func TestReconcileTranslationOnYRCClock(t *testing.T) {
	const lrc = "[00:10]First\n[00:18]Second"
	const yrc = "[12000,400](12000,400,0)First\n[18100,400](18100,400,0)Second"
	lines, _ := BuildDisplay(lrc, "[00:12]第一句\n[00:18]第二句", yrc)
	if len(lines) != 2 || lines[0].Translation != "第一句" || lines[1].Translation != "第二句" {
		t.Fatalf("translation on the YRC clock was lost: %+v", lines)
	}
}

// An exact YRC translation survives even when LRC puts the same line before a YRC-only row.
func TestReconcileExactTranslationsWithCrossedSourceClocks(t *testing.T) {
	const lrc = "[00:10]First"
	const yrc = "[11000,500](11000,500,0)Second\n[12000,500](12000,500,0)First"
	lines, _ := BuildDisplay(lrc, "[00:11]第二句\n[00:12]第一句", yrc)
	if len(lines) != 2 || lines[0].Text != "Second" || lines[0].Translation != "第二句" ||
		lines[1].Text != "First" || lines[1].Translation != "第一句" {
		t.Fatalf("exact translation was lost or moved across source clocks: %+v", lines)
	}
}

// A large equal-time repeat group keeps source order without dropping any translation.
func TestReconcileDenseRepetitionsKeepTranslations(t *testing.T) {
	const count = 128
	lrc := strings.Repeat("[00:10]Repeated\n", count)
	yrc := strings.Repeat("[10400,500](10400,500,0)Repeated\n", count)
	translation := strings.Repeat("[00:10]Verse\n", count-1) + "[00:10]Last verse"
	lines, _ := BuildDisplay(lrc, translation, yrc)
	if len(lines) != count || lines[count-1].Translation != "Last verse" {
		t.Fatalf("repeat group lost its last translation: %d lines", len(lines))
	}
	for i, line := range lines[:count-1] {
		if !line.Timed || line.Translation != "Verse" {
			t.Fatalf("row %d lost word timing or translation: %+v", i, line)
		}
	}
}

// Only an unambiguous nearby text variant may borrow an LRC translation cue.
func TestReconcileVariantTranslationClock(t *testing.T) {
	const lrc = "[00:08]LRC only\n[00:10]不要忘记我"
	const variant = "[10050,500](10050,500,0)请不要忘记我"
	for _, tc := range []struct {
		name, lrc, yrc, translation, want string
	}{
		{"single insertion", lrc, variant, "[00:10]Remember me", "Remember me"},
		{"single omission", "[00:10]请不要忘记我", "[10050,500](10050,500,0)不要忘记我", "[00:10]Remember me", "Remember me"},
		{"ambiguous LRC", lrc + "\n[00:10.09]请不要忘记", variant, "[00:10]Remember me", ""},
		{"ambiguous YRC", lrc, variant + "\n[10060,500](10060,500,0)你不要忘记我", "[00:10]Remember me", ""},
		{"ambiguous LRC translation", lrc, variant, "[00:10]First\n[00:10]Second", ""},
		{"substitution", "[00:10]你不要忘记我", variant, "[00:10]Remember me", ""},
		{"outside timing window", "[00:09.90]不要忘记我", variant, "[00:09.90]Remember me", ""},
		{"exact YRC translation", lrc, variant, "[00:10]LRC translation\n[00:10.05]YRC translation", "YRC translation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := parseYRC(tc.yrc)
			lines, _ := BuildDisplay(tc.lrc, tc.translation, tc.yrc)
			if len(lines) != len(original) || lines[0].Translation != tc.want {
				t.Fatalf("incorrect translation or row count: %+v", lines)
			}
			for i, line := range lines {
				if line.Text != original[i].Text || line.At != original[i].At || !line.Timed {
					t.Fatalf("translation matching changed the selected source: %+v", line)
				}
			}
			if len(lines) > 1 && lines[1].Translation != "" {
				t.Fatalf("ambiguous variant got a translation: %+v", lines[1])
			}
		})
	}
}

// Timing alone cannot decide between credits that share a cue.
func TestReconcileCreditTranslationsByRole(t *testing.T) {
	const lrc = "[00:10]作曲：甲\n[00:10]作词：乙"
	for _, tc := range []struct {
		translation, composer, lyricist string
	}{
		{"[00:10]Lyrics: B\n[00:10]Composer: A", "Composer: A", "Lyrics: B"},
		{"[00:10]Lyrics: B\n[00:10]Composed by A", "Composed by A", "Lyrics: B"},
		{"[00:10]Composed by A", "", ""},
		{"[00:10]Arranger: C", "", ""},
	} {
		_, display := BuildDisplay(lrc+"\n[00:11]Sung line", tc.translation, "")
		if len(display.Lines) != 3 || display.Lines[0].Translation != tc.composer ||
			display.Lines[1].Translation != tc.lyricist || display.Lines[2].Translation != "" {
			t.Fatalf("translations %q were assigned to the wrong role: %+v", tc.translation, display.Lines)
		}
	}
}

// A timed punctuation placeholder cannot suppress sung LRC, but punctuation stays in usable YRC.
func TestReconcilePunctuationOnlyYRCFallsBackToLRC(t *testing.T) {
	const lrc = "[00:10]First line"
	const punctuation = "[9000,500](9000,500,0)…"
	lines, _ := BuildDisplay(lrc, "[00:10]Translation", punctuation)
	if len(lines) != 1 || lines[0].Text != "First line" || lines[0].At != 10*time.Second ||
		lines[0].Timed || lines[0].Translation != "Translation" {
		t.Fatalf("punctuation-only YRC displaced LRC: %+v", lines)
	}
	lines, _ = BuildDisplay(lrc, "", punctuation+"\n[11000,500](11000,500,0)第二句")
	if len(lines) != 2 || lines[0].Text != "…" || lines[1].Text != "第二句" || !lines[1].Timed {
		t.Fatalf("usable YRC did not keep its own punctuation and sung rows: %+v", lines)
	}
}

// A dense repeat group followed by a distinct lyric still pairs past the candidate limit.
func TestReconcileDenseRepetitionsWithinSong(t *testing.T) {
	const count = 128
	for _, tc := range []struct {
		name string
		step int
	}{
		{"same timestamps", 0},
		{"distinct timestamps", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lrc, yrc, translation strings.Builder
			for i := range count {
				ms := i * tc.step
				fmt.Fprintf(&lrc, "[00:10.%03d]Repeated\n", ms)
				fmt.Fprintf(&yrc, "[%d,500](%d,500,0)Repeated\n", 10200+ms, 10200+ms)
				fmt.Fprintf(&translation, "[00:10.%03d]Verse %d\n", ms, i)
			}
			lrc.WriteString("[00:16]Ending")
			yrc.WriteString("[16200,500](16200,500,0)Ending")
			translation.WriteString("[00:16]Closing")
			lines, _ := BuildDisplay(lrc.String(), translation.String(), yrc.String())
			if len(lines) != count+1 || lines[count].Translation != "Closing" {
				t.Fatalf("repeat group or ending changed: %d lines", len(lines))
			}
			for i, line := range lines[:count] {
				if line.Translation != fmt.Sprintf("Verse %d", i) {
					t.Fatalf("repeat %d lost its translation: %+v", i, line)
				}
			}
		})
	}
}

// Compare the sparse matcher with all possible ordered pairings on small grids.
func TestOrderedLyricPairsMaximizesCountThenMinimizesDistance(t *testing.T) {
	const size = 3
	for mask := range 1 << (size * size) {
		for phase := range 3 {
			cost := func(i, j int) time.Duration {
				return time.Duration((i*5+j*3+phase)%7) * time.Millisecond
			}
			allowed := func(i, j int) bool { return mask&(1<<(i*size+j)) != 0 }
			pairs := orderedLyricPairs(size, size, func(i int, add func(int, time.Duration) bool) {
				for j := range size {
					if allowed(i, j) {
						add(j, cost(i, j))
					}
				}
			})

			var wantCount int
			var wantCost time.Duration
			var visit func(i, j, count int, total time.Duration)
			visit = func(i, j, count int, total time.Duration) {
				if count > wantCount || count == wantCount && total < wantCost {
					wantCount, wantCost = count, total
				}
				for left := i; left < size; left++ {
					for right := j; right < size; right++ {
						if allowed(left, right) {
							visit(left+1, right+1, count+1, total+cost(left, right))
						}
					}
				}
			}
			visit(0, 0, 0, 0)

			previous := lyricPair{-1, -1}
			var total time.Duration
			for _, pair := range pairs {
				if pair.left <= previous.left || pair.left >= size ||
					pair.right <= previous.right || pair.right >= size || !allowed(pair.left, pair.right) {
					t.Fatalf("mask %09b phase %d: invalid order or pairing %+v", mask, phase, pairs)
				}
				total += cost(pair.left, pair.right)
				previous = pair
			}
			if len(pairs) != wantCount || total != wantCost {
				t.Fatalf("mask %09b phase %d: got %d pairs for %v, want %d for %v",
					mask, phase, len(pairs), total, wantCount, wantCost)
			}
		}
	}
}
