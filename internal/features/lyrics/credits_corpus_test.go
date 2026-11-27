/*
用完整喜欢歌曲样本中发现的字段形态验证署名识别，夹具仅保留合成文本。
*/

package lyrics

import (
	"fmt"
	"testing"
	"time"
)

// Exact production fields observed across the corpus are recognized without
// turning performer labels, partial role matches or empty fields into credits.
func TestCreditCorpusRoleExpressions(t *testing.T) {
	for _, role := range []string{
		"弦乐编排", "音频助理", "Synthesizer Operator", "VOCALOID调校",
		"Rap创作", "译词/中文词翻译", "低音吉他", "电贝斯", "协作制作人",
		"录音工程", "特别支持", "Writers", "配唱", "混音 / 后制", "混音录音室",
		"合声编写", "制作助理", "封面绘图", "音乐制作发行", "母带后期",
		"乐器录音棚", "录音制作", "声乐编辑", "电影原声发行", "行销策略",
		"音乐制作总监", "项目协力", "宣传", "吉他录音", "吉他录音室",
		"母带后期处理录音室", "LRC字幕",
	} {
		if !IsCredit(role + "：Studio A") {
			t.Errorf("missed explicit production field %q", role)
		}
		if IsCredit(role + "：　") {
			t.Errorf("empty field became a credit: %q", role)
		}
	}
	for _, text := range []string{
		"合声：听见你的声音", "主唱：唱出明天", "PGM：Alice", "合：一起唱",
		"说唱：唱出希望", "sing:la la la", "音乐制作总监的梦想：就在前方",
		"特别支持你的梦想：一直到天明", "编曲/未知字段：Alice", "movie:our story",
	} {
		if IsCredit(text) {
			t.Errorf("uncertain lyric or partial field became a credit: %q", text)
		}
	}
}

// Singer/backing-vocal fields can bound a confirmed edge credit block, while
// a sung label, repetition, insufficient anchors or a distant cue stays sung.
func TestCreditVocalAttributionEdges(t *testing.T) {
	for _, tc := range []struct {
		name, lrc, target string
		want              LineKind
	}{
		{"opening singer", "[00:00]演唱：甲\n[00:00]作词：乙\n[00:01]作曲：丙\n[00:20]唱出明天", "演唱：甲", Credit},
		{"closing harmony", "[00:20]唱出明天\n[00:30]录音：甲\n[00:31]键盘：乙\n[00:32]和声：丙/丁", "和声：丙/丁", Credit},
		{"cover before pause", "[00:00]混音：甲\n[00:01]母带：乙\n[00:02]翻唱：丙\n[00:20]唱出明天", "翻唱：丙", Credit},
		{"cover without pause", "[00:00]混音：甲\n[00:01]母带：乙\n[00:02]翻唱：丙\n[00:03]唱出明天", "翻唱：丙", Sung},
		{"one anchor", "[00:00]作曲：甲\n[00:01]和声：乙", "和声：乙", Sung},
		{"distant tail", "[00:00]录音：甲\n[00:01]键盘：乙\n[00:20]和声：丙", "和声：丙", Sung},
		{"repeated cue", "[00:00]演唱：甲\n[00:01]录音：乙\n[00:02]键盘：丙\n[00:03]演唱：甲\n[00:20]唱出明天", "演唱：甲", Sung},
		{"closing rap", "[00:20]唱出明天\n[00:30]录音：甲\n[00:31]键盘：乙\n[00:32]说唱：唱出希望", "说唱：唱出希望", Sung},
		{"closing chorus", "[00:20]唱出明天\n[00:30]录音：甲\n[00:31]键盘：乙\n[00:32]合唱：唱出希望", "合唱：唱出希望", Sung},
		{"opening music", "[00:00]Music: forever\n[00:01]录音：甲\n[00:02]键盘：乙\n[00:20]唱出明天", "Music: forever", Sung},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, display := BuildDisplay(tc.lrc, "", "")
			found := false
			for _, row := range display.Lines {
				if row.Text == tc.target {
					found = true
					if row.Kind != tc.want {
						t.Errorf("unexpected classification: %+v", row)
					}
				}
			}
			if !found {
				t.Fatal("lost the field instead of classifying it")
			}
		})
	}
}

// A weak timed field needs a matching, confirmed LRC credit within the clock
// window; the same words elsewhere in a verse retain their original timing.
func TestCreditWeakWordTimingConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, lrc string
		at        time.Duration
		want      LineKind
	}{
		{"nearby harmony", "[00:20]唱出明天\n[00:30]录音：甲\n[00:30.5]键盘：乙\n[00:31]和声：丙", 31 * time.Second, Credit},
		{"clock boundary", "[00:20]唱出明天\n[00:30]录音：甲\n[00:30.5]键盘：乙\n[00:31]和声：丙", 41 * time.Second, Credit},
		{"outside clock window", "[00:20]唱出明天\n[00:30]录音：甲\n[00:30.5]键盘：乙\n[00:31]和声：丙", 41*time.Second + time.Millisecond, Sung},
		{"unconfirmed LRC", "[00:31]和声：丙\n[01:20]唱出明天", 31 * time.Second, Sung},
		{"different contributor", "[00:20]唱出明天\n[00:30]录音：甲\n[00:30.5]键盘：乙\n[00:31]和声：丁", 31 * time.Second, Sung},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yrc := fmt.Sprintf("[%d,1000](%d,1000,0)和声：丙\n[80000,1000](80000,1000,0)唱出明天", tc.at.Milliseconds(), tc.at.Milliseconds())
			lines, display := BuildDisplay(tc.lrc, "", yrc)
			if tc.want == Credit && (len(lines) != 1 || lines[0].At != 80*time.Second) {
				t.Fatal("confirmed credit remained on the singing timeline")
			}
			for _, row := range display.Lines {
				if row.Text == "和声：丙" && row.Kind == tc.want {
					if row.Kind != tc.want || tc.want == Sung && (len(row.Words) != 1 || row.Words[0].Start != tc.at) {
						t.Fatalf("timing evidence not respected: %+v", row)
					}
					return
				}
			}
			t.Fatalf("field lost or misclassified: %+v", display.Lines)
		})
	}
	// PGM is ambiguous alone, but an LRC block can confirm its YRC cue.
	lrc := "[00:00]键盘：甲\n[00:01]PGM：乙\n[00:02]吉他：丙\n[00:20]唱出明天"
	yrc := "[1000,1000](1000,1000,0)PGM：乙\n[20000,1000](20000,1000,0)唱出明天"
	lines, _ := BuildDisplay(lrc, "", yrc)
	if len(lines) != 1 || lines[0].Text != "唱出明天" || lines[0].At != 20*time.Second {
		t.Fatal("corroborated programming credit stayed on the sung timeline")
	}
}

// Ratio-colon credit spelling deduplicates with the ordinary separator, while
// a ratio sign inside actual words neither removes nor retimes that verse.
func TestCreditRatioSeparator(t *testing.T) {
	lrc := "[00:00]作词：乙\n[00:01]作曲：甲\n[00:02]曲目信息\n[00:11]作曲∶甲\n[00:20]唱出（A∶B）的明天"
	lines, display := BuildDisplay(lrc, "[00:20]A new tomorrow", "")
	if len(lines) != 2 || lines[1].Text != "唱出（A∶B）的明天" || lines[1].At != 20*time.Second || lines[1].Translation != "A new tomorrow" {
		t.Fatal("ratio separator lost a lyric or shifted translation timing")
	}
	if len(display.Lines) != 4 || display.Lines[0].Kind != Credit || display.Lines[1].Kind != Credit {
		t.Fatal("equivalent composer fields were not deduplicated")
	}
	// The same rare punctuation in an unconfirmed refrain is still singing.
	lines, _ = BuildDisplay("[00:10]作曲∶甲\n[00:20]唱出明天\n[00:30]作曲∶甲", "", "")
	if len(lines) != 3 {
		t.Fatal("ratio punctuation alone overrode repeated-lyric protection")
	}
}
