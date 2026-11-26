/*
呈现歌单命名与删除对话框，并构建将歌曲加入或移出歌单的菜单。
*/

package library

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
)

// playlistItems are the items of a song's menu that put it in the
// listener's playlists, or take it out of pid, the one it is listed in, if
// set.
func (a *View) Items(m *ui.Menu, tr *netease.Track, pid int64) {
	if a.Account.Profile == nil {
		return
	}
	m.Separator()
	m.Submenu("添加到歌单", func(m *ui.Menu) {
		if m.Item("新建歌单…").Chosen() {
			a.NewPlaylist(tr)
		}
		var to []*netease.Playlist
		for i := range a.Model.Mine {
			if pl := &a.Model.Mine[i]; pl.SpecialType != 5 && pl.ID != pid {
				to = append(to, pl)
			}
		}
		if len(to) > 0 {
			m.Separator()
		}
		for _, pl := range to {
			if m.Item(pl.Name).Chosen() {
				a.AddTo(pl.ID, tr)
			}
		}
	})
	if a.Editable(pid) != nil && m.Item("从歌单中移除").Chosen() {
		a.RemoveFrom(pid, tr)
	}
}

// playlistNameDialog asks for the name of a playlist to make, or a new
// name for one.
func (a *View) NameDialog(c *ui.Context) {
	p := *a.Palette
	d := &a.Model.Naming
	ui.DialogBase(c.Key("playlist-name"), &d.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(360).Padding(24).Radius(18).Gap(16).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, action := "新建歌单", "创建"
		if d.Rename != 0 {
			title, action = "重命名歌单", "保存"
		}
		if d.Busy {
			action = "正在创建…"
		}
		ui.Text(c, title).FontSize(17).FontWeight(700)
		in := ui.TextInput(c.Key("name"), &d.Name).Placeholder("歌单名称").Label("歌单名称").FontSize(14).AutoFocus()
		if in.Submitted() {
			a.SubmitName()
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if components.PillButton(c.Key("cancel"), nil, "取消", p.Hover.Over(p.Background), p.Text).Clicked() {
				d.Open = false
			}
			ok := components.PillButton(c.Key("ok"), nil, action, p.Accent, p.OnAccent)
			if strings.TrimSpace(d.Name) == "" {
				ok.Disabled(true).Opacity(0.5)
			}
			if ok.Clicked() {
				a.SubmitName()
			}
		})
	})
}

// deletePlaylistDialog asks whether to delete a playlist, which can't be
// undone.
func (a *View) DeleteDialog(c *ui.Context) {
	p := *a.Palette
	d := &a.Model.Deleting
	ui.DialogBase(c.Key("playlist-delete"), &d.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(360).Padding(24).Radius(18).Gap(8).
			Background(p.Background).Border(1, p.Hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "删除歌单").FontSize(17).FontWeight(700)
		ui.Text(c, "确定删除歌单“"+d.Name+"”吗？删除后无法恢复。").FontSize(13).TextColor(p.Muted).LineHeight(1.45)
		ui.Row(c).Gap(10).Justify(ui.End).Margin(8, 0, 0, 0).Children(func() {
			if components.PillButton(c.Key("cancel"), nil, "取消", p.Hover.Over(p.Background), p.Text).Clicked() {
				d.Open = false
			}
			if components.PillButton(c.Key("ok"), nil, "删除", p.Accent, p.OnAccent).Clicked() {
				a.DeleteChosen()
			}
		})
	})
}
