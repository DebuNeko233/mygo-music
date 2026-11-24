package settings

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/shell"
)

// cacheDirectoryDialog asks whether existing caches move to the chosen root. It shows in the settings window's own theme.
func (a *View) CacheDialog(c *ui.Context, p components.Palette) {
	d := &a.Cache.Confirm
	ui.DialogBase(c.Key("cache-directory-confirm"), &d.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(420).Padding(24).Radius(18).Gap(12).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		// The buttons and what went wrong stay in sight, beside any path as
		// long as it is.
		_, height := c.Size()
		details := height - 136 // padding, gaps, buttons and the window's margin
		if d.Error != "" {
			details -= 48
		}
		ui.Scroll(c.Key("details")).MaxHeight(details).Gap(12).Children(func() {
			ui.Text(c, "更改缓存目录").FontSize(17).FontWeight(700).TextColor(p.Text)
			ui.Text(c, "确认后立即生效，无需重启。").FontSize(13).TextColor(p.Muted)
			ui.Column(c).Padding(12).Radius(10).Gap(5).Background(p.Sidebar).Children(func() {
				ui.Text(c, "新目录").FontSize(11).FontWeight(600).TextColor(p.Muted)
				ui.Text(c, d.Target).FontSize(12.5).TextColor(p.Text).LineHeight(1.45)
			})
			ui.Text(c, "迁移并更改：搬移已有的歌曲、图片、页面与歌词缓存。\n仅更改目录：保留旧文件，之后使用新目录。").FontSize(13).TextColor(p.Text).LineHeight(1.5)
			ui.Text(c, "目标中的缓存文件夹需为空。更改期间暂时停止播放，完成后恢复进度；请勿同时运行其他应用实例。").FontSize(12).TextColor(p.Muted).LineHeight(1.45)
		})
		if d.Error != "" {
			ui.Text(c, d.Error).FontSize(12).TextColor(p.Accent).LineHeight(1.45).MaxLines(2).Tooltip(d.Error)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Margin(4, 0, 0, 0).Children(func() {
			if components.PillButton(c.Key("cancel"), nil, "取消", p.Hover.Over(p.Background), p.Text).Clicked() {
				d.Open = false
			}
			if components.PillButton(c.Key("switch"), nil, "仅更改目录", p.Hover.Over(p.Background), p.Text).Clicked() {
				a.SubmitCacheDirectory(false)
			}
			if components.PillButton(c.Key("migrate"), nil, "迁移并更改", p.Accent, p.OnAccent).Clicked() {
				a.SubmitCacheDirectory(true)
			}
		})
	})
}

// Progress uses worker notifications, not an animation or an idle polling timer.
func (a *View) CacheNotice(c *ui.Context) {
	if !a.Cache.Moving {
		return
	}
	ui.Row(c.Key("cache-move-progress")).Absolute().Top(shell.TopBar(c)+4).Left(shell.SidebarWidth+24).
		Padding(8, 12).Gap(12).Radius(12).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
		ui.Text(c, a.CacheMoveLabel()).FontSize(12).TextColor(a.Palette.Text)
		if a.Cache.Cancelable() && !a.Cache.Cancelling {
			if ui.Button(c.Key("cancel-cache-move"), "取消迁移").Clicked() {
				a.CancelCacheChange()
			}
		}
	})
}
