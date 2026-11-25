/*
绘制当前歌曲行的播放电平，并提供原生电平控件的生命周期接入。
*/

package playback

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/player"
)

// NowPlayingBars dance in the row of the track playing.
func (a *View) NowPlayingBars(c *ui.Context, background ...ui.Color) {
	if len(background) == 1 && nativeRowBars != nil && nativeRowBars(a, c, background[0]) {
		return
	}
	color := a.palette().Accent
	ui.Box(c).Size(16, 14).Draw(func(p *ui.Painter, r ui.Rect) {
		lv, live := a.Player().Levels()
		a.RowBars.Step(p.Now(), lv, live)
		components.DrawBars(p, r, &a.RowBars, 4, color, 2.5)
		if live || a.RowBars.Group(0, player.Bands) > 0.01 {
			p.AnimationFrame()
		}
	})
}

// Native row meters are attached once during window creation and released by its lifetime.
var setupNativeRowBars func(*View)
var nativeRowBars func(*View, *ui.Context, ui.Color) bool

func (a *View) SetupNativeRowBars() {
	if setupNativeRowBars != nil {
		setupNativeRowBars(a)
	}
}
