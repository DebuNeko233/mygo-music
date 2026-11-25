/*
提供跨包界面测试使用的拖动、动画稳定等待与虚拟时钟推进方法。
*/

package testutil

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/player"
)

// Drag simulates a held drag over several frames and lets its motion settle.
func Drag(tt *ui.Tester, r ui.Rect, dx float32, hold time.Duration) {
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	for i := 1; i <= 6; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x+dx*float32(i)/6, y)
	}
	if hold > 0 {
		tt.Advance(hold)
	}
	tt.Release(x+dx, y)
	tt.Advance(time.Second)
	tt.Frame()
}

// Settle advances through the lyric layout and scrolling transitions.
func Settle(tt *ui.Tester) {
	tt.Frame()
	for range 2 {
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
	}
}

// FramesAt counts requested frames after playback seeks and the view settles.
func FramesAt(pl *player.Player, tt *ui.Tester, pos time.Duration) int {
	pl.Seek(pos)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	asked := 0
	for range 60 {
		tt.Advance(time.Second / 60)
		if tt.FramePending() {
			asked++
			tt.Frame()
		}
	}
	return asked
}
