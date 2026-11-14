package lyrics

import (
	"testing"
	"time"
)

// Cache records can give a pure-music notice a time and put credits on both sides of it.
func TestDisplayInstrumentalWithCredits(t *testing.T) {
	_, view := BuildDisplay("[00:00.00]作曲 : A\n[00:01.00]编曲 : B\n[00:05.00]纯音乐，请欣赏\n[01:47.50]企划 : C\n[01:48.50]出品 : D", "", "")
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[0].Text != InstrumentalText || view.Lines[0].At != 5*time.Second {
		t.Fatalf("instrumental display: %+v", view.Lines)
	}
	if cur, next := view.Position(110 * time.Second); cur != -1 || next != -1 {
		t.Fatalf("static rows entered the playback timeline: %d, %d", cur, next)
	}
	for _, line := range view.Lines[1:] {
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
	_, view := BuildDisplay(lrc, "[00:13.00]Never", yrc)
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
}

// A credit-only word source must not hide LRC singing or a speaker-labeled line.
func TestDisplayFallsBackToLRCBody(t *testing.T) {
	_, view := BuildDisplay("[00:08.00]合：仍然在唱\n[00:09.00]纯音乐也有故事", "",
		"[0,2000](0,2000,0)作曲：A")
	if len(view.Lines) != 3 || view.Lines[0].Kind != Credit || view.Lines[1].Kind != Sung || view.Lines[1].Timed || view.Lines[2].Kind != Sung {
		t.Fatalf("lost LRC body: %+v", view.Lines)
	}
}

// The cache includes joined bilingual credit names and alternative instrumental notices.
func TestDisplayDictionaryAliases(t *testing.T) {
	lrc := "[00:00.00]作曲Composer：A\n[00:01.00]指挥Conductor：B\n[00:02.00]母带制作 Mastering Engineer：C\n" +
		"[00:03.00]请欣赏～纯音乐\n[00:04.00]合作联系："
	_, view := BuildDisplay(lrc, "", `{"t":1000,"c":[{"tx":"指挥: "},{"tx":"B"}]}`)
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[0].Text != InstrumentalText {
		t.Fatalf("cache aliases were not grouped as static metadata: %+v", view)
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
	_, empty := BuildDisplay("[00:00.00]暂无歌词", "", "")
	if len(empty.Lines) != 0 {
		t.Fatalf("missing lyrics became a timed line: %+v", empty)
	}
}

// A release note around the instrumental notice is displayed without playback timing.
func TestDisplayInstrumentalReleaseNotes(t *testing.T) {
	lrc := "[00:03.50]这首歌的名字叫《示例》，\n[00:04.00]如果你喜欢，希望你能点击红心，收藏它，\n" +
		"[00:04.50]并能与你的朋友分享，谢谢！\n[00:05.00]纯音乐，请欣赏\n[01:37.00]合作联系："
	_, view := BuildDisplay(lrc, "", "")
	if view.HasSinging() || len(view.Lines) != 5 || view.Lines[0].Kind != Instrumental {
		t.Fatalf("instrumental release notes became sung lines: %+v", view)
	}
}
