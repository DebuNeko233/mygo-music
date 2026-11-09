package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Progress uses worker notifications, not an animation or an idle polling timer.
func (a *app) cacheMoveNotice(c *ui.Context) {
	if !a.cacheMoving {
		return
	}
	ui.Row(c.Key("cache-move-progress")).Absolute().Top(topBar(c)+4).Left(sidebarWidth+24).
		Padding(8, 12).Gap(12).Radius(12).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
		ui.Text(c, a.cacheMoveLabel()).FontSize(12).TextColor(a.pal.text)
		if a.cacheCancel != nil && !a.cacheCancelling {
			if ui.Button(c.Key("cancel-cache-move"), "取消迁移").Clicked() {
				a.cancelCacheChange()
			}
		}
	})
}
