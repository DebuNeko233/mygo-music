/*
绘制离线或请求失败提示，并提供可选的重试操作。
*/

package view

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/shell"
)

func OfflineNotice(c *ui.Context, p components.Palette, offline bool, refresh func()) {
	ui.Row(c.Key("offline-status")).Absolute().Top(shell.TopBar(c) + 4).Right(24).
		Gap(2).Padding(3).Radius(999).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
		// A reachable network may still have failed resources. Keep the refresh
		// action, but never display the offline symbol for that online state.
		if offline {
			ui.Box(c.Key("offline-icon")).Size(30, 30).Center().Label("离线").Tooltip("离线，可浏览缓存").Children(func() {
				ui.Icon(c, components.IconOffline).Size(18, 18).TextColor(p.Muted)
			})
		}
		if components.IconButton(c.Key("refresh-connection"), components.IconReload, 18, p.Muted, "刷新内容").Size(30, 30).Tooltip("刷新内容").Clicked() {
			refresh()
		}
	})
}
