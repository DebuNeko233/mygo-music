/*
验证纯净歌词切换后的跟随、逐句定位与无正文提示。
*/

package playback

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/player"
)

// Changing modes follows the current body line in either layout; clicking the
// cleaned line still seeks to its source timestamp and stable frames keep source identity.
func TestPureLyricsFollowAndSeek(t *testing.T) {
	for _, immersive := range []bool{false, true} {
		t.Run(fmt.Sprint(immersive), func(t *testing.T) {
			a, tt := lyricPaneView(t, nil, 480, 360)
			a.Config.LyricEdgeFade = immersive
			player.RefreshLyrics(a.Player(), 1, "[00:00]作词：甲\n[00:01]作曲：乙\n[00:20]男：唱出明天\n[00:30]女：留住星光", "", "")
			player.Hold(a.Player(), 31*time.Second)
			tt.Frame()
			a.Config.PureLyrics = true
			tt.Frame()
			if a.Reading.At != 1 || !tt.HasText("留住星光") || tt.HasText("作词：甲") {
				t.Fatal("pure presentation did not follow the current body")
			}
			first := a.Reading.First
			tt.Frame()
			if a.Reading.First != first || a.Reading.At != 1 {
				t.Fatal("a stable presentation changed reading identity or current line")
			}
			if err := tt.Click("唱出明天"); err != nil {
				t.Fatal(err)
			}
			if a.Player().Position() != 20*time.Second {
				t.Fatal("cleaned line sought to a different timestamp")
			}
			player.RefreshLyrics(a.Player(), 1, "[00:00]作曲：乙", "", "")
			tt.Frame()
			if !tt.HasText(lyrics.NoSingingText) || tt.HasText(lyrics.InstrumentalText) {
				t.Fatal("metadata-only lyrics were mistaken for an instrumental")
			}
			stage := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() { a.Stage(c, a.Player().Current(), ui.RGB(0, 0, 0)) })
			}, 480, 360)
			if !stage.HasText(lyrics.NoSingingText) {
				t.Fatal("stage did not use the pure presentation's empty notice")
			}
		})
	}
}
