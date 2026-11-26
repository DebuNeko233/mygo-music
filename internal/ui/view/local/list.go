/*
呈现本地曲库和目录列表，并连接播放、扫描与文件管理操作。
*/

package local

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	localvm "netease-music-mygo/internal/ui/viewmodel/local"
)

type View struct {
	LyricScroll                 ui.ScrollState
	Player                      func() *player.Player
	OpenLyricMatch              func()
	SearchLyrics, ConfirmLyrics func(*localvm.LocalLyricMatch)

	*page.Page
	Model                                             *localvm.LocalLibrary
	CurrentIndex                                      func([]netease.Track) int
	SyncViewport                                      func(*ui.Context, string, string, int, int)
	Locate                                            func(*ui.Context, int, bool)
	AddFolders                                        func([]string)
	Refresh, OpenFolders, ChooseFolders, CloseFolders func()
	RemoveFolder                                      func(string)
}

func (a *View) Build(c *ui.Context) {
	p := *a.Palette
	tracks := a.Model.Tracks
	at := a.CurrentIndex(tracks)
	a.SyncViewport(c, "本地歌曲", a.Model.Status, len(tracks), at)
	visible := false
	pane := ui.Column(c.Key("local-music")).Grow(1).MinHeight(0)
	if files := pane.DroppedFiles(); len(files) != 0 {
		a.AddFolders(files)
	}
	pane.Children(func() {
		header := func(c *ui.Context) {
			ui.Column(c).Padding(0, 12, 14, 12).Gap(8).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "本地歌曲").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
						ui.Text(c, fmt.Sprintf("%d 首歌曲", len(tracks))).FontSize(12).TextColor(p.Muted)
					})
					ui.Row(c.Key("local-actions")).Gap(12).AlignItems(ui.Stretch).Children(func() {
						if len(a.Model.Catalog.Folders) != 0 {
							ui.Button(c.Key("refresh-local"), "刷新曲库").Disabled(a.Model.Busy).OnClick(a.Refresh)
						}
						components.AnimateButtonPress(ui.PrimaryButton(c.Key("manage-local"), "管理文件夹"), 0.96).
							Disabled(a.Model.Path == "").OnClick(a.OpenFolders)
					})
				})
				if a.Model.Status != "" {
					ui.Text(c, a.Model.Status).FontSize(12).TextColor(p.Muted)
				}
			})
		}
		if len(tracks) == 0 {
			visible = a.TrackRows(c, tracks, true, header, func(c *ui.Context) {
				ui.Column(c).Center().Padding(48, 0, 48, 0).Gap(12).Children(func() {
					ui.Icon(c, components.IconNote).Size(34, 34).TextColor(p.Faint)
					ui.Text(c, "拖入音乐文件或文件夹").FontSize(14).TextColor(p.Muted)
				})
			})
			return
		}
		visible = a.TrackRows(c, tracks, true, header)
	})
	a.Locate(c, at, visible)
}

func (a *View) Folders(c *ui.Context, theme ui.Theme, p components.Palette) {
	p.ApplyAccent(&theme)
	c.SetTheme(&theme)
	c.Root().Background(p.Background)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		a.CloseFolders()
	})
	ui.Column(c.Key("local-folders")).Fill().Padding(16, 18, 16, 18).Gap(12).Children(func() {
		ui.Row(c).Justify(ui.End).Children(func() {
			components.AnimateButtonPress(ui.PrimaryButton(c.Key("add-local-folder"), "添加"), 0.96).
				Label("添加文件夹").Disabled(a.Model.Busy).OnClick(a.ChooseFolders)
		})
		folders := a.Model.Catalog.Folders
		if len(folders) == 0 {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, "还没有添加音乐文件夹").FontSize(13).TextColor(p.Muted)
			})
		} else {
			ui.Scroll(c.Key("folders-list")).Grow(1).MinHeight(0).Gap(5).Children(func() {
				for _, folder := range folders {
					ui.Row(c.Key(folder)).MinHeight(38).Radius(7).Background(p.Hover).PaddingX(9).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, folder).SingleLine().MinWidth(0).Grow(1).FontSize(12).TextColor(p.Text).Tooltip(folder)
						ui.Button(c.Key("remove-folder"), "移除").Label("移除 " + folder).Disabled(a.Model.Busy).OnClick(func() {
							a.RemoveFolder(folder)
						})
					})
				}
			})
		}
		if a.Model.Status != "" {
			ui.Text(c, a.Model.Status).FontSize(12).TextColor(p.Muted)
		}
	})
}
