/*
提供主窗口内的错误占位和卡片列布局。
*/

package shell

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ui/view/components"
)

// Centered replaces a missing page with its error and optional retry action.
func Centered(c *ui.Context, p components.Palette, offline bool, err string, retry func()) {
	top, bottom := Insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
		if offline {
			ui.Icon(c, components.IconOffline).Size(24, 24).TextColor(p.Muted)
			err = "暂无缓存内容"
		}
		ui.Text(c, err).TextColor(p.Muted).FontSize(13)
		if retry != nil && components.IconButton(c.Key("retry"), components.IconReload, 18, p.Muted, "重新加载内容").Tooltip("重新加载内容").Clicked() {
			retry()
		}
	})
}

const PagePad = 40

// Columns fits cards into the content area beside the fixed-width sidebar.
func Columns(c *ui.Context, min float32) int {
	w, _ := c.Size()
	inner := w - SidebarWidth - 2*PagePad
	return max(2, int((inner+20)/(min+20)))
}
