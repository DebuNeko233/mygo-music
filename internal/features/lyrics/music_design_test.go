/*
验证音乐设计复合署名离开演唱时间轴，同时保护实际演唱的逐字内容。
*/

package lyrics

import (
	"testing"
	"time"
)

// A music-design/supervision field observed in local matched lyrics is metadata
// in both LRC and corroborated YRC, without moving the following sung line.
func TestMusicDesignCreditAcrossSources(t *testing.T) {
	lrc := "[00:00]作曲：甲\n[00:01]音乐设计/监制：Studio A\n[00:10]唱出新的明天"
	yrc := "[1000,1000](1000,1000,0)音乐设计/监制：Studio A\n[10000,2000](10000,1000,0)唱出(11000,1000,0)新的明天"
	for _, source := range []struct {
		name, words string
	}{{"LRC", ""}, {"YRC", yrc}} {
		t.Run(source.name, func(t *testing.T) {
			lines, display := BuildDisplay(lrc, "[00:10]A new tomorrow", source.words)
			if len(lines) != 1 || lines[0].Text != "唱出新的明天" || lines[0].At != 10*time.Second || lines[0].Translation != "A new tomorrow" {
				t.Fatalf("credit entered or shifted the singing timeline: %+v", lines)
			}
			found := false
			for _, line := range display.Lines {
				if line.Text == "音乐设计/监制：Studio A" {
					found = true
					if line.Kind != Credit || len(line.Words) != 0 {
						t.Fatal("music-design credit retained word highlighting")
					}
				}
			}
			if !found {
				t.Fatal("classification discarded readable credit text")
			}
			if source.words != "" && (!lines[0].Timed || len(lines[0].Words) != 2 || lines[0].Words[0].Start != 10*time.Second) {
				t.Fatal("credit classification changed the following word timings")
			}
		})
	}
}

// A matching role-shaped line actually carrying word timings remains singing
// without confirmation from the untimed source, even after adding the alias.
func TestMusicDesignWordTimedSingingIsPreserved(t *testing.T) {
	yrc := "[10000,1000](10000,500,0)音乐设计：(10500,500,0)我的梦想\n[12000,1000](12000,1000,0)唱出明天"
	lines, display := BuildDisplay("", "", yrc)
	if len(lines) != 2 || len(display.Lines) != 2 || !lines[0].Timed || lines[0].Kind != Sung || len(lines[0].Words) != 2 {
		t.Fatal("an uncorroborated word-timed lyric was removed as a credit")
	}
}
