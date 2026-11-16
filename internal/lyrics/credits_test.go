package lyrics

import (
	"reflect"
	"testing"
	"time"
)

// Real cached header shapes (with fictional contributors) exercise variants
// previously missed by the prefix list, and sentences it must never absorb.
func TestCreditExpressions(t *testing.T) {
	for _, text := range []string{
		"作詞：甲", "producer: Alice", "ＰＲＯＤＵＣＥＲ：Ａｌｉｃｅ",
		"Executive Producer: Alice", "  Mixing\t Engineer ： Alice  ",
		"作词/作曲：甲", "词/曲：甲", "作词 Lyrics：甲",
		"制作人Producer：甲", "出品 Produced by: Studio A",
		"混音/母带 Mixing/Mastering Engineer：甲", "混音／母带处理：Studio A",
		"混音/母带工程师：甲", "编曲/混音：甲", "op/sp：Studio A",
		"钢琴 Piano：甲", "三味线 Shamisen：甲", "筝 Koto：甲",
		"录音现场指导 Scoring Sessions Director：甲",
		"舞台监督 Orchestra Stage Manager：甲", "乐团经理 Orchestra Manager：甲",
		"音乐监制 Music Supervisor：甲", "联合制作 Co-produced by：Studio A",
		"工程师 ProTools Operator：甲", "母带制作 Mastering Engineer：甲",
		"Producers : Alice / Bob", "Guitars: Alice", "Keyboards: Alice",
		"Tenor Saxophone: Alice", "Baritone Saxophone: Alice", "Linn Drum: Alice",
		"Additional Engineering: Alice", "Mix Engineering: Alice",
		"Synth Programming: Alice", "母带工程室: Studio A", "PV 视频：甲",
		"Produced by Alice", "Mixed by Alice", "Lyrics by Alice", "Music by Alice",
		"Composed by Alice", "Written by Alice", "Arranged by Alice",
		"Recorded at Studio A (London)", "Mixed at Studio A", "Mastered at Studio A",
		"Published by Publisher A", "人声录音：甲", "录音作品及MV版权：Studio A",
		" \ufeff作詞：甲\u200b ",
	} {
		if !IsCredit(text) {
			t.Errorf("missed credit %q", text)
		}
	}
	for _, text := range []string{
		"Music is my life: forever", "Producer of my dreams: you",
		"制作你的梦：陪你到天亮", "作词的人说：你好", "OPening: hello",
		"我说：你好", "时间：是一把刀", "曲终人散：空余叹",
		"歌手甲：唱出你的热情", "说唱：出卖你的爱", "合唱：明天会更好",
		"Music: forever", "Lead Vocals: Alice", "念白：甲",
		"Produced by", "Produced by  ", "Produced by: ", "作词：　", "作词",
		"Composer Alice", "Produced by the river: we sang", "Lyrics are written by Alice",
		"Music at midnight", "Recorded at", "She was recorded at Studio A",
		"作词//作曲：甲", "作词/：甲", "/作词：甲", "作词 & & 作曲：甲",
		"作词/未知职务：甲", "作词 Something Else：甲", "蒸気火鸡：甲",
		"心语心愿仿佛挥舞翅膀的（Re:dreaming Angel)",
	} {
		if IsCredit(text) {
			t.Errorf("mistook lyric/uncertain line for a credit: %q", text)
		}
	}
}

// A vocal label needs production anchors on both sides, not just a position
// near the start/end where the first or last sung line could appear.
func TestCreditBlockContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		texts []string
		want  []LineKind
	}{
		{"intro", []string{"作词：甲", "演唱：乙", "作曲：丙", "第一句"}, []LineKind{Credit, Credit, Credit, Sung}},
		{"outro", []string{"最后一句", "Producers: Alice", "Lead & Background Vocals: Bob", "Drums: Carol"}, []LineKind{Sung, Credit, Credit, Credit}},
		{"first sung label", []string{"作词：甲", "作曲：乙", "说唱：出卖你的爱", "下一句"}, []LineKind{Credit, Credit, Sung, Sung}},
		{"last sung label", []string{"上一句", "合唱：明天会更好", "混音：甲", "母带：乙"}, []LineKind{Sung, Sung, Credit, Credit}},
		{"unknown speaker", []string{"作词：甲", "歌手甲：正文", "作曲：乙", "下一句"}, []LineKind{Credit, Sung, Credit, Sung}},
		{"middle", []string{"上一句", "作词：甲", "念白：乙", "作曲：丙", "下一句"}, []LineKind{Sung, Credit, Sung, Credit, Sung}},
		{"refrain", []string{"作词：甲", "下一句", "作词：甲"}, []LineKind{Sung, Sung, Sung}},
		{"duplicate provider credits", []string{"作词：甲", "作曲：乙", "作曲：乙", "作词：甲", "正文"}, []LineKind{Credit, Credit, Credit, Credit, Sung}},
		{"empty value", []string{"作词：甲", "演唱：", "作曲：乙"}, []LineKind{Credit, Sung, Credit}},
		{"all credits", []string{"作词：甲", "演唱：乙", "作曲：丙"}, []LineKind{Credit, Credit, Credit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := make([]Line, len(tc.texts))
			for i, text := range tc.texts {
				lines[i] = Line{Text: text}
			}
			classifyLines(lines)
			for i := range lines {
				if lines[i].Kind != tc.want[i] {
					t.Errorf("line %d (%q): kind %v, want %v", i, lines[i].Text, lines[i].Kind, tc.want[i])
				}
			}
		})
	}
}

// Actual word timing is cautionary evidence, while SpreadWords-generated
// timing must not make ordinary LRC credits look sung.
func TestCreditWordTiming(t *testing.T) {
	yrc := "[0,1000](0,1000,0)作词：甲\n[1000,1000](1000,1000,0)演唱：乙\n[2000,1000](2000,1000,0)作曲：丙\n[20000,1000](20000,1000,0)正文"
	lines := Build("", "", yrc)
	want := []LineKind{Credit, Sung, Credit, Sung}
	for i, l := range lines {
		if l.Kind != want[i] || !l.Timed || (l.Kind == Credit && len(l.Words) != 0) {
			t.Errorf("line %d: %+v", i, l)
		}
	}
	single := Build("", "", "[0,1000](0,1000,0)Produced by Alice\n[20000,1000](20000,1000,0)正文")
	if single[0].Kind != Sung {
		t.Fatal("isolated word-timed credit-shaped lyric was excluded")
	}
	lrc := Build("[00:00]作词：甲\n[00:20]正文", "", "")
	if lrc[0].Kind != Credit || lrc[0].Timed || len(lrc[0].Words) != 0 {
		t.Fatal("synthetic word timing changed credit classification")
	}
}

// Sparse lyrics must not borrow confidence from a production credit many
// seconds away merely because no other line was transcribed between them.
func TestCreditContextRequiresNearbyTimes(t *testing.T) {
	lines := Build("[00:00]作词：甲\n[01:00]演唱：这是正文\n[02:00]作曲：乙", "", "")
	if lines[1].Kind != Sung {
		t.Fatal("distant credits swallowed a sung line")
	}
}

// In the cached ReDreaming Angel LRC, the vocal credit ends the opening block before the first lyric.
func TestCreditClosingOpeningVocalField(t *testing.T) {
	const header = "[00:03.355]录音师 : A\n[00:04.026]录音棚 : Studio\n"
	for _, tc := range []struct {
		name, role, firstCue string
		want                 LineKind
	}{
		{"cached opening", "演唱", "00:11.988", Credit},
		{"vocal label with no break", "演唱", "00:05.369", Sung},
		{"rap label with a break", "说唱", "00:11.988", Sung},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lrc := header + "[00:04.697]" + tc.role + " : A/B/C\n[00:05.369]\n[" + tc.firstCue + "]正文"
			_, display := BuildDisplay(lrc, "", "")
			if len(display.Lines) != 4 || display.Lines[2].Kind != tc.want {
				t.Fatalf("unexpected classification of the opening row: %+v", display.Lines)
			}
			current, next := display.Position(5 * time.Second)
			if tc.want == Credit && (current != -1 || next != 3) ||
				tc.want == Sung && (current != 2 || next != 3) {
				t.Fatalf("unexpected playback position after the opening row: %d, %d", current, next)
			}
		})
	}
}

// Cached instrumental tracks can encode a production credit as a long YRC cue.
func TestCreditTimedInstrumentalCreditConfirmedByLRC(t *testing.T) {
	const lrc = "[00:00.00]作曲 : A\n[00:00.00]出品 Produced by:Studio-A"
	const yrc = "[220,18960](220,18960,0)出品Produced by:StudioA"
	lines, display := BuildDisplay(lrc, "", yrc)
	if len(lines) != 0 || display.HasSinging() {
		t.Fatalf("a corroborated timing annotation entered the sung timeline: %+v", display.Lines)
	}
}

// The Mass puts timed title and credit lines before the first sung cue.
func TestCreditTimedOpeningBlockConfirmsPartialContributorMatch(t *testing.T) {
	const yrc = "[0,3444](0,3444,0)The Mass\n" +
		"[3450,3450](3450,138,0)Lyrics(3726,138,0)by(3864,3036,0)：A/B/C\n" +
		"[6900,3439](6900,3439,0)Composedby：D/E\n" +
		"[10355,1351](10355,1351,0)First sung line"
	for _, tc := range []struct {
		name, composition, firstCue string
		want                        LineKind
	}{
		{"same composer confirms the block", "D & E", "00:10.640", Credit},
		{"no matching contributor", "F", "00:10.640", Sung},
		{"role after first LRC lyric", "D & E", "00:02.000", Sung},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lrc := "[00:00.000]作词：A & B\n[00:01.000]作曲：" + tc.composition + "\n[" + tc.firstCue + "]First sung line"
			lines, display := BuildDisplay(lrc, "", yrc)
			last := lines[len(lines)-1]
			if last.At != 10355*time.Millisecond || !last.Timed {
				t.Fatalf("lost the selected YRC lyric: %+v", lines)
			}
			for _, row := range display.Lines {
				if row.At == 3450*time.Millisecond {
					if row.Kind != tc.want || tc.want == Credit && len(row.Words) != 0 {
						t.Fatalf("unexpected classification of the timed lyricist line: %+v", row)
					}
					return
				}
			}
			t.Fatal("the timed lyricist line was lost from the display")
		})
	}
}

// Classification preserves every display/timing field and leaves uncertain
// vocal lines in the sung interval used to schedule quiet-part comments.
func TestCreditBuildAndQuietIntervals(t *testing.T) {
	lrc := "[00:00]ＭＩＸＥＤ　ＢＹ：Ａｌｉｃｅ\n[00:01]作詞：甲\n[00:20]说唱：这是正文\n[00:25]第二句"
	translation := "[00:20]Rap: these are lyrics"
	lines := Build(lrc, translation, "")
	if len(lines) != 4 || lines[0].Text != "ＭＩＸＥＤ　ＢＹ：Ａｌｉｃｅ" || lines[0].Kind != Credit || lines[2].Kind != Sung || lines[2].Translation != "Rap: these are lyrics" {
		t.Fatalf("built lines %+v", lines)
	}
	want := []Quiet{{0, 20 * time.Second}, {31 * time.Second, 60 * time.Second}}
	if got := Quiets(lines, 60*time.Second, 8*time.Second); !reflect.DeepEqual(got, want) {
		t.Fatalf("quiet intervals %v, want %v", got, want)
	}
	raw := append([]Line(nil), lines...)
	for i := range raw {
		raw[i].Kind, raw[i].classified = Sung, false
	}
	before := append([]Line(nil), raw...)
	if got := Quiets(raw, 60*time.Second, 8*time.Second); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(raw, before) {
		t.Fatal("direct-line fallback changed intervals or mutated caller's lines")
	}
	if got := Quiets(Build("[00:00]作词：甲\n[00:01]作曲：乙", "", ""), 60*time.Second, time.Second); len(got) != 0 {
		t.Fatal("credit-only lyrics invented a sung interval")
	}
}

// JSON annotations remain outside the sung YRC stream; adding classification
// must not insert titles, credits or arbitrary JSON text into playback timing.
func TestCreditYRCAnnotationsStaySeparate(t *testing.T) {
	yrc := "{\"t\":0,\"c\":[{\"tx\":\"作词：甲\"}]}\n{\"t\":1000,\"c\":[{\"tx\":\"歌曲标题\"}]}\n[20000,1000](20000,500,0)正(20500,500,0)文"
	lines, _ := BuildDisplay("[00:00]作词：甲\n[00:20]正文", "", yrc)
	if len(lines) != 1 || lines[0].Kind != Sung || lines[0].At != 20*time.Second || len(lines[0].Words) != 2 {
		t.Fatalf("annotations affected playback: %+v", lines)
	}
}
