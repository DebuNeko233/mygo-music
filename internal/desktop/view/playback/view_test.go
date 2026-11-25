/*
验证替换共享播放器后，播放控件使用当前播放器执行命令。
*/

package playback

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/player"
)

// Transport actions use the current shared player after the owner replaces it.
func TestTransportUsesCurrentPlayer(t *testing.T) {
	first := player.New(func(func()) {})
	current := first
	v := View{Player: func() *player.Player { return current }}
	tt := ui.NewTester(func(c *ui.Context) { v.Transport(c, White, White.Alpha(.4), White, 20) }, 400, 100)
	tt.Frame()
	current = player.New(func(func()) {})
	tt.Frame()
	if err := tt.Click("循环"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if current.Repeat() == first.Repeat() {
		t.Fatal("repeat did not change on the current player")
	}
	if first.Repeat() != player.RepeatOff {
		t.Fatal("the detached player was changed")
	}
}
