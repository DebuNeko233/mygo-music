/*
验证多语言署名、纯音乐提示、来源回退和歌词展示顺序。
*/

package lyrics

import (
	"testing"
	"time"
)

// Credits from all three languages share aliases without absorbing role-labeled lyrics.
func TestDisplayMultilingualCredits(t *testing.T) {
	lrc := "[00:00.00]和声编写：A\n[00:01.00]Lead Guitar: B\n[00:02.00]作曲／編曲：C\n" +
		"[00:03.00]作詞：D\n[00:04.00]Lyricsby: D\n[00:05.00]インストゥルメンタル"
	_, view := BuildDisplay(lrc, "", "")
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[4].Kind != Instrumental {
		t.Fatalf("multilingual credits were not grouped or deduplicated: %+v", view)
	}
	for _, text := range []string{"合：一起唱", "歌手の名前：歌う", "Alice: Sing along"} {
		if lineKind(text) != Sung {
			t.Errorf("speaker label treated as metadata: %q", text)
		}
	}
}

// Cache records can give a pure-music notice a time and put credits on both sides of it.
func TestDisplayInstrumentalWithCredits(t *testing.T) {
	_, view := BuildDisplay("[00:00.00]作曲 : A\n[00:01.00]编曲 : B\n[00:05.00]纯音乐，请欣赏\n[01:47.50]企划 : C\n[01:48.50]出品 : D", "", "")
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[2].Kind != Instrumental ||
		view.Lines[2].Text != InstrumentalText || view.Lines[2].At != 5*time.Second {
		t.Fatalf("instrumental display: %+v", view.Lines)
	}
	if cur, next := view.Position(110 * time.Second); cur != -1 || next != -1 {
		t.Fatalf("static rows entered the playback timeline: %d, %d", cur, next)
	}
	for i, line := range view.Lines {
		if i == 2 {
			continue
		}
		if line.Kind != Credit || len(line.Words) != 0 {
			t.Fatalf("credit treated as singing: %+v", line)
		}
	}
}

// Cached LRC can contain only credits even when YRC supplies the sung lines.
func TestDisplayUsesYRCAndMergesCredits(t *testing.T) {
	lrc := "[00:00.00]作曲 : A\n[00:01.00]出品 Produced by : B\n[01:47.50]指挥 Conductor : C"
	yrc := `{"t":500,"c":[{"tx":"出品："},{"tx":"B"},{"or":"ignored","li":"ignored"}]}
{"t":2000,"c":[{"tx":"录音棚 Recording Studio："},{"tx":"D"}]}
[13010,4780](13010,660,0)从(13670,670,0)不`
	_, view := BuildDisplay(lrc, "[00:01.00]Produced by B\n[00:13.00]Never", yrc)
	if !view.HasSinging() || len(view.Lines) != 5 {
		t.Fatalf("merged display: %+v", view.Lines)
	}
	cur, next := view.Position(2 * time.Second)
	if cur != -1 || next != 3 || !view.Lines[next].Timed || view.Lines[next].Translation != "Never" {
		t.Fatalf("before singing: indices %d, %d, lines %+v", cur, next, view.Lines)
	}
	if cur, next = view.Position(110 * time.Second); cur != 3 || next != -1 {
		t.Fatalf("trailing credit changed the active lyric: %d, %d", cur, next)
	}
	if got := view.Lines[1].Translation; got != "Produced by B" {
		t.Fatalf("merged credit lost its translation: %q", got)
	}
}

// Playback searches by timestamp even when a caller supplies rows out of order.
func TestDisplayOrdersSungRowsWithoutChangingSource(t *testing.T) {
	lines := []Line{{At: 8 * time.Second, Text: "Second"}, {At: time.Second, Text: "First"}}
	view := NewDisplay(lines)
	current, next := view.Position(2 * time.Second)
	if current != 0 || next != 1 || view.Lines[current].Text != "First" || lines[0].Text != "Second" {
		t.Fatalf("position = %d, %d, display = %+v, source = %+v", current, next, view.Lines, lines)
	}
}

// Translation matching keeps the first row at duplicate or tied timestamps.
func TestTranslationsKeepEarlierTimestamp(t *testing.T) {
	lines := Build("[00:01.00]First\n[00:01.00]Duplicate\n[00:03.00]Second\n[00:06.00]Third",
		"[00:02.00]Tie\n[00:03.10]Near second\n[00:04.50]Outside window\n[00:06.20]Near third", "")
	want := []string{"Tie", "", "Near second", "Near third"}
	for i, line := range lines {
		if line.Translation != want[i] {
			t.Errorf("line %d translation = %q, want %q", i, line.Translation, want[i])
		}
	}
}

// A credit-only word source must not hide LRC singing or a speaker-labeled line.
func TestDisplayFallsBackToLRCBody(t *testing.T) {
	_, view := BuildDisplay("[00:08.00]合：仍然在唱\n[00:09.00]纯音乐也有故事", "",
		"[0,2000](0,2000,0)作曲：A")
	if len(view.Lines) != 3 || view.Lines[0].Kind != Credit || view.Lines[1].Kind != Sung || view.Lines[1].Timed || view.Lines[2].Kind != Sung {
		t.Fatalf("lost LRC body: %+v", view.Lines)
	}
}

// A word-timed release note beside an instrumental notice must not hide sung LRC lyrics.
func TestDisplayFallsBackWhenYRCContainsInstrumentalNotes(t *testing.T) {
	const lrc = "[00:10.00]First sung line\n[00:12.00]Second sung line"
	const translation = "[00:10.00]第一句"
	const note = "[1000,500](1000,500,0)这首歌的名字叫《示例》"
	for _, tc := range []struct{ name, notice string }{
		{"timed", "[500,500](500,500,0)纯音乐，请欣赏"},
		{"json", `{"t":500,"c":[{"tx":"纯音乐，请欣赏"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, view := BuildDisplay(lrc, translation, tc.notice+"\n"+note)
			if len(view.Lines) != 4 || view.Lines[0].Kind != Instrumental || view.Lines[1].Kind != Credit {
				t.Fatalf("display did not keep the notice and note as metadata: %+v", view.Lines)
			}
			current, next := view.Position(10 * time.Second)
			if current != 2 || next != 3 || view.Lines[current].Text != "First sung line" || view.Lines[current].Translation != "第一句" || view.Lines[current].Timed {
				t.Fatalf("fallback lost the timed LRC and its translation: current=%d next=%d lines=%+v", current, next, view.Lines)
			}
			if len(lines) != 2 || lines[0].Text != view.Lines[current].Text || lines[0].At != view.Lines[current].At || lines[0].Translation != view.Lines[current].Translation {
				t.Fatalf("singing timeline did not use the same fallback: %+v", lines)
			}
		})
	}
}

// The cache includes joined bilingual credit names and alternative instrumental notices.
func TestDisplayDictionaryAliases(t *testing.T) {
	lrc := "[00:00.00]作曲Composer：A\n[00:01.00]指挥Conductor：B\n[00:02.00]母带制作 Mastering Engineer：C\n" +
		"[00:03.00]请欣赏～纯音乐\n[00:04.00]合作联系："
	_, view := BuildDisplay(lrc, "", `{"t":1000,"c":[{"tx":"指挥: "},{"tx":"B"}]}`)
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[3].Text != InstrumentalText {
		t.Fatalf("cache aliases were not grouped as static metadata: %+v", view)
	}
}

// Accept bilingual aliases only when both languages describe the same role.
func TestDisplayCreditAliasesAreExplicit(t *testing.T) {
	_, view := BuildDisplay("[00:00]录音师 Recording Engineer：A\n[00:01]制作人 Producer：B\n[00:02]作詞 Drums：唱下去", "", "")
	if len(view.Lines) != 3 || view.Lines[0].Kind != Credit || view.Lines[1].Kind != Credit || view.Lines[2].Kind != Sung {
		t.Fatalf("field aliases misclassified the original text: %+v", view.Lines)
	}
}

// Cache excerpts preserve credits at both ends of a song and deduplicate bilingual roles.
func TestDisplayCachedCreditFields(t *testing.T) {
	for _, tc := range []struct {
		name, lrc, yrc string
		credits        int
	}{
		{
			name: "opening credits",
			lrc: "[00:01.68]编曲 : A\n[00:02.52]和音 : B\n[00:03.36]和声编唱 : A\n" +
				"[00:04.20]琵琶 : C\n[00:05.05]唱词",
			credits: 4,
		},
		{
			name: "closing credits with blank cues and narration",
			lrc: "[05:15.30]唱词\n[05:26.85]\n[05:30.23]字音指导：A\n[05:30.94]\n" +
				"[05:31.65]念白：A\n[05:32.37]\n[05:33.08]绘图：B\n[05:34.51]PV：C",
			credits: 4,
		},
		{
			name: "bilingual aliases",
			lrc: "[02:04.00]唱词\n[02:05.971]舞台监督 Orchestra Stage Manager：A\n" +
				"[02:06.304]乐团经理 Orchestra Manager：B\n[02:06.637]录音现场指导 Scoring Sessions Director：C",
			yrc:     `{"t":126304,"c":[{"tx":"Orchestra Manager: B"}]}`,
			credits: 3,
		},
		{
			name:    "compound aliases",
			lrc:     "[00:17.553]唱词\n[00:21.301]词／曲：A\n[00:23.801]cover/翻译润色 by：B",
			yrc:     `{"t":21301,"c":[{"tx":"Lyrics ＆ Music: A"}]}`,
			credits: 2,
		},
		{
			name:    "standalone song title containing a colon",
			lrc:     "[00:00]歌名：Re:dreaming Angel\n[00:01]作词：A\n[00:12]唱词",
			credits: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Use the cached cue times and role labels with sample names and lyrics.
			lines, view := BuildDisplay(tc.lrc, "", tc.yrc)
			if len(lines) != 1 || lines[0].Text != "唱词" || len(view.Lines) != tc.credits+1 {
				t.Fatalf("want one sung row and %d credits, got %+v", tc.credits, view.Lines)
			}
			for _, line := range view.Lines {
				if line.Text != "唱词" && (line.Kind != Credit || len(line.Words) != 0) {
					t.Fatalf("credit is still part of the sung timeline: %+v", line)
				}
			}
		})
	}
}

// Cached speaker labels and the title Re:dreaming Angel within a lyric stay on the playback timeline.
func TestDisplayCachedColonLyricsStayActive(t *testing.T) {
	for _, tc := range []struct {
		name, lrc, yrc, text string
		at                   time.Duration
		timed                bool
	}{
		{
			name: "word-timed rap",
			lrc:  "[01:04.296]说唱：甲乙丙丁戊",
			yrc: "[62730,1940](62730,240,0)说(62970,240,0)唱(63210,240,0)：(63450,240,0)甲" +
				"(63690,240,0)乙(63930,240,0)丙(64170,240,0)丁(64410,260,0)戊",
			text: "说唱：甲乙丙丁戊", at: 62730 * time.Millisecond, timed: true,
		},
		{
			name: "song title appears in the first verse",
			lrc:  "[00:49.324]上句\n[00:54.699]歌词（Re:dreaming Angel)\n[01:01.573]下句",
			text: "歌词（Re:dreaming Angel)", at: 54699 * time.Millisecond,
		},
		{
			name: "song title reappears later",
			lrc:  "[00:54.699]歌词（Re:dreaming Angel)\n[02:21.012]另一句（Re:dreaming Angel)",
			text: "另一句（Re:dreaming Angel)", at: 141012 * time.Millisecond,
		},
		{
			name: "spoken lyric within a verse",
			lrc:  "[00:00]作词：A\n[00:05]上句\n[00:06]念白：继续唱\n[00:07]下句",
			text: "念白：继续唱", at: 6 * time.Second,
		},
		{
			name: "word-timed narration after credits",
			yrc:  "[0,1000](0,1000,0)作词：A\n[1000,1000](1000,1000,0)念白：继续唱",
			text: "念白：继续唱", at: time.Second, timed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, view := BuildDisplay(tc.lrc, "", tc.yrc)
			current, _ := view.Position(tc.at)
			if current < 0 {
				t.Fatalf("no active lyric at %v: %+v", tc.at, view.Lines)
			}
			line := view.Lines[current]
			if line.Kind != Sung || line.At != tc.at || line.Text != tc.text || line.Timed != tc.timed {
				t.Fatalf("wrong active lyric at %v: %+v", tc.at, line)
			}
		})
	}
}

// Exact notices leave similar sung phrases and speaker labels on the timeline.
func TestDisplayDictionaryLeavesSungTextAlone(t *testing.T) {
	lrc := "[00:00.00]暂无歌词\n[00:03.00]合：仍然在唱\n[00:04.00]作曲是一场梦：我的梦\n" +
		"[00:05.00]这首歌的名字叫《远方》"
	_, view := BuildDisplay(lrc, "", "")
	if !view.HasSinging() || len(view.Lines) != 3 || view.Lines[0].At != 3*time.Second {
		t.Fatalf("a notice or a credit-like prefix changed the sung lines: %+v", view)
	}
}

// Empty data keeps the instrumental fallback, while an explicit notice stays outside the timeline.
func TestDisplayEmptyNotice(t *testing.T) {
	for _, tc := range []struct{ name, lrc, yrc, want string }{
		{"empty", "", "", InstrumentalText},
		{"blank rows", "[00:00.00]\n [00:01.00]  ", "", InstrumentalText},
		{"plain LRC", "\ufeff 暂无歌词。\n", "", UnavailableText},
		{"timed LRC", "[00:00.00]暂无歌词", "", UnavailableText},
		{"timed YRC", "", "[0,500](0,500,0)暂无歌词", UnavailableText},
		{"JSON YRC", "", `{"t":0,"c":[{"tx":"暫無歌詞"}]}`, UnavailableText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, display := BuildDisplay(tc.lrc, "", tc.yrc)
			if len(display.Lines) != 0 || display.HasSinging() || display.EmptyText() != tc.want {
				t.Fatalf("notice = %q, rows = %+v, want %q with no rows", display.EmptyText(), display.Lines, tc.want)
			}
		})
	}
}

// A release note around the instrumental notice is displayed without playback timing.
func TestDisplayInstrumentalReleaseNotes(t *testing.T) {
	lrc := "[00:03.50]这首歌的名字叫《示例》，\n[00:04.00]如果你喜欢，希望你能点击红心，收藏它，\n" +
		"[00:04.50]并能与你的朋友分享，谢谢！\n[00:05.00]纯音乐，请欣赏\n[01:37.00]合作联系："
	_, view := BuildDisplay(lrc, "", "")
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[3].Kind != Instrumental {
		t.Fatalf("instrumental release notes became sung lines: %+v", view)
	}
}
