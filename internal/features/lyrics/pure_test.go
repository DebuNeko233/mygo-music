/*
验证纯净歌词只改变展示，保护正文、原始数据、翻译及逐字时间。
*/

package lyrics

import (
	"reflect"
	"testing"
	"time"
)

// Credits and independently identified title headers disappear; performer
// labels lose only their prefixes, leaving singing and translation aligned.
func TestPureLyricsCreditsHeadersAndSpeakers(t *testing.T) {
	lrc := "[00:00]夜航 - Alice\n[00:01]作词：甲\n[00:02]作曲：乙\n[00:20]男：唱出明天\n[00:25]Alice：留住星光\n[00:30]时间：并没有停下"
	_, source := BuildDisplay(lrc, "[00:20]男：A new tomorrow\n[00:25]Alice：Keep the stars", "")
	before := append([]Line(nil), source.Lines...)
	pure := source.Pure(SongInfo{Title: "夜航", Artists: []string{"Alice"}})
	if len(pure.Lines) != 3 || pure.Lines[0].Text != "唱出明天" || pure.Lines[0].Translation != "A new tomorrow" || pure.Lines[1].Text != "留住星光" || pure.Lines[2].Text != "时间：并没有停下" {
		t.Fatalf("wrong pure presentation: %+v", pure.Lines)
	}
	if !reflect.DeepEqual(before, source.Lines) {
		t.Fatal("presentation changed the parsed source")
	}
	for i, at := range []time.Duration{20 * time.Second, 25 * time.Second, 30 * time.Second} {
		if pure.Lines[i].At != at || pure.Lines[i].Kind != Sung {
			t.Fatal("presentation retimed singing or retained metadata")
		}
		cur, _ := pure.Position(at)
		if cur != i {
			t.Fatal("filtered row index no longer follows the singing clock")
		}
	}
}

// Removing a prefix never interpolates word times or mutates the shared words,
// including a label that shares one timed word with the first lyric character.
func TestPureLyricsKeepsWordTiming(t *testing.T) {
	for _, yrc := range []string{
		"[10000,2000](10000,1000,0)男：(11000,1000,0)你好",
		"[10000,2000](10000,1000,0)男：你(11000,1000,0)好",
	} {
		_, source := BuildDisplay("", "", yrc)
		originalWords := append([]Word(nil), source.Lines[0].Words...)
		pure := source.Pure(SongInfo{})
		if len(pure.Lines) != 1 || pure.Lines[0].Text != "你好" || pure.Lines[0].At != 10*time.Second || pure.Lines[0].End != 12*time.Second || !pure.Lines[0].Timed {
			t.Fatalf("wrong timed presentation: %+v", pure.Lines)
		}
		for _, w := range pure.Lines[0].Words {
			found := false
			for _, original := range originalWords {
				found = found || w.Start == original.Start && w.End == original.End
			}
			if !found {
				t.Fatal("a surviving word acquired a different timestamp")
			}
		}
		if !reflect.DeepEqual(originalWords, source.Lines[0].Words) {
			t.Fatal("prefix removal mutated the original word array")
		}
	}
	uncertain := NewDisplay([]Line{{Text: "男：你好", Timed: true, Words: []Word{{Text: "ni hao", End: time.Second}}}})
	if got := uncertain.Pure(SongInfo{}); got.Lines[0].Text != uncertain.Lines[0].Text {
		t.Fatal("uncertain word alignment was rewritten")
	}
}

// Matching a title alone is insufficient: ordinary/repeated sung titles,
// quoted verses and unknown artist names remain intact.
func TestPureLyricsProtectsTitleRefrains(t *testing.T) {
	_, source := BuildDisplay("[00:00]作词：甲\n[00:01]作曲：乙\n[00:10]夜航\n[00:20]唱出明天\n[00:30]「夜航」\n[00:40]夜航 - Bob", "", "")
	pure := source.Pure(SongInfo{Title: "夜航", Artists: []string{"Alice"}})
	if len(pure.Lines) != 4 || pure.Lines[0].Text != "夜航" || pure.Lines[2].Text != "「夜航」" || pure.Lines[3].Text != "夜航 - Bob" {
		t.Fatalf("a sung title or unknown artist was discarded: %+v", pure.Lines)
	}
	_, intro := BuildDisplay("[00:00]夜航\n[00:01]作词：甲\n[00:02]作曲：乙\n[00:20]唱出明天", "", "")
	if got := intro.Pure(SongInfo{Title: "夜航"}); len(got.Lines) != 1 || got.Lines[0].Text != "唱出明天" {
		t.Fatal("a title before the confirmed production block stayed in the lyrics")
	}
	plain := NewDisplay([]Line{{Text: "夜航"}})
	if got := plain.Pure(SongInfo{Title: "夜航"}); len(got.Lines) != 1 || &got.Lines[0] != &plain.Lines[0] {
		t.Fatal("a bare lyric was treated as metadata or unnecessarily copied")
	}
	// A short song can sing its title just before closing credits. Real word
	// timing remains stronger evidence than the adjacent metadata block.
	timed := NewDisplay([]Line{
		{At: 20 * time.Second, Text: "夜航", Timed: true, Words: []Word{{Text: "夜航", Start: 20 * time.Second, End: 21 * time.Second}}},
		{At: 22 * time.Second, Text: "作词：甲"},
		{At: 23 * time.Second, Text: "作曲：乙"},
	})
	if got := timed.Pure(SongInfo{Title: "夜航"}); len(got.Lines) != 1 || !reflect.DeepEqual(got.Lines[0], timed.Lines[0]) {
		t.Fatal("a word-timed title verse before closing credits was removed")
	}
}

// Only an explicit instrumental notice keeps that status; hiding credits or
// receiving no lyrics does not invent an instrumental classification.
func TestPureLyricsEmptyStates(t *testing.T) {
	for _, tc := range []struct {
		name, lrc, want string
	}{
		{"credits only", "[00:00]作词：甲\n[00:01]作曲：乙", NoSingingText},
		{"empty", "", NoSingingText},
		{"instrumental", "[00:00]纯音乐，请欣赏\n[00:01]作曲：甲", InstrumentalText},
		{"unavailable", "暂无歌词", UnavailableText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, source := BuildDisplay(tc.lrc, "", "")
			pure := source.Pure(SongInfo{})
			if pure.HasSinging() || len(pure.Lines) != 0 || pure.EmptyText() != tc.want {
				t.Fatalf("wrong pure empty state: %+v / %q", pure, pure.EmptyText())
			}
		})
	}
}

// Known performers may include a location or multiple names. Ordinary colon
// sentences and artists not independently known must not lose their prefixes.
func TestPureLyricsPerformerEvidence(t *testing.T) {
	_, source := BuildDisplay("[00:00]作词：甲\n[00:01]演唱：Alice/Bob\n[00:02]作曲：乙\n[00:20]Alice（上海）：唱出明天\n[00:25]Alice&Bob：留住星光\n[00:30]Carol：这行保留\n[00:35]合：", "", "")
	pure := source.Pure(SongInfo{})
	if len(pure.Lines) != 3 || pure.Lines[0].Text != "唱出明天" || pure.Lines[1].Text != "留住星光" || pure.Lines[2].Text != "Carol：这行保留" {
		t.Fatalf("performer evidence was lost or guessed: %+v", pure.Lines)
	}
}
