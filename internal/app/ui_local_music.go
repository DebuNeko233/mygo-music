package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

var openLocalDialog = mygo.Dialog.Open

func (a *app) openLocalFolders() {
	if a.local.window != nil {
		a.local.window.Show()
		a.local.window.Focus()
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:             "管理文件夹",
		Parent:            a.win,
		Width:             520,
		Height:            350,
		MinWidth:          395,
		MinHeight:         250,
		UseContentSize:    true,
		DisableFullScreen: true,
		DisableMaximize:   true,
		DisableMinimize:   true,
		SkipTaskbar:       true,
		AutoHideMenuBar:   true,
		BackgroundColor:   "light-dark(#f4f4f6, #1c1c1f)",
		Content:           ui.View(a.localFoldersView),
	})
	a.local.window = w
	w.OnClosed(func() { a.local.window = nil })
}

func (a *app) redrawLocalFolders() {
	if w := a.local.window; w != nil {
		w.Invalidate()
	}
}

func (a *app) chooseLocalFile(id int64) {
	if a.local.busy || a.local.path == "" {
		return
	}
	a.local.picking = true
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.win, Title: "重新定位音频文件",
		Filters: []mygo.FileFilter{{Name: "本地音频文件", Extensions: localExtensions}},
	})
	a.local.picking = false
	if err == nil && len(paths) != 0 {
		a.relinkLocalSong(id, paths[0])
	}
}

func (a *app) chooseLocalFolders() {
	if a.local.busy || a.local.path == "" {
		return
	}
	a.local.picking = true
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.local.window, Title: "选择音乐文件夹", Directory: true, Multiple: true,
	})
	a.local.picking = false
	if err == nil {
		a.addLocalFolders(paths)
	}
}

func (a *app) localPage(c *ui.Context) {
	p := a.pal
	tracks := a.local.tracks
	pane := ui.Column(c.Key("local-music")).Grow(1).MinHeight(0)
	if files := pane.DroppedFiles(); len(files) != 0 {
		a.addLocalFolders(files)
	}
	pane.Children(func() {
		header := func(c *ui.Context) {
			ui.Column(c).Padding(0, 12, 14, 12).Gap(8).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "本地歌曲").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
						ui.Text(c, fmt.Sprintf("%d 首歌曲", len(tracks))).FontSize(12).TextColor(p.muted)
					})
					ui.Row(c.Key("local-actions")).Gap(12).AlignItems(ui.Stretch).Children(func() {
						if len(a.local.catalog.Folders) != 0 {
							ui.Button(c.Key("refresh-local"), "刷新曲库").Disabled(a.local.busy).OnClick(a.refreshLocalSongs)
						}
						animateButtonPress(ui.PrimaryButton(c.Key("manage-local"), "管理文件夹"), 0.96).
							Disabled(a.local.path == "").OnClick(a.openLocalFolders)
					})
				})
				if a.local.status != "" {
					ui.Text(c, a.local.status).FontSize(12).TextColor(p.muted)
				}
			})
		}
		if len(tracks) == 0 {
			a.trackRows(c, tracks, true, header, func(c *ui.Context) {
				ui.Column(c).Center().Padding(48, 0, 48, 0).Gap(12).Children(func() {
					ui.Icon(c, iconNote).Size(34, 34).TextColor(p.faint)
					ui.Text(c, "拖入包含 MP3 或 FLAC 的文件夹").FontSize(14).TextColor(p.muted)
				})
			})
			return
		}
		a.trackRows(c, tracks, true, header)
	})
}

func (a *app) localFoldersView(c *ui.Context) {
	theme := a.settings.uiTheme(c.Theme())
	p := paletteFor(&theme)
	p.applyAccent(&theme)
	c.SetTheme(&theme)
	c.Root().Background(p.bg)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		if w := a.local.window; w != nil {
			w.Close()
		}
	})
	ui.Column(c.Key("local-folders")).Fill().Padding(16, 18, 16, 18).Gap(12).Children(func() {
		ui.Row(c).Justify(ui.End).Children(func() {
			animateButtonPress(ui.PrimaryButton(c.Key("add-local-folder"), "添加"), 0.96).
				Label("添加文件夹").Disabled(a.local.busy).OnClick(a.chooseLocalFolders)
		})
		folders := a.local.catalog.Folders
		if len(folders) == 0 {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, "还没有添加音乐文件夹").FontSize(13).TextColor(p.muted)
			})
		} else {
			ui.Scroll(c.Key("folders-list")).Grow(1).MinHeight(0).Gap(5).Children(func() {
				for _, folder := range folders {
					ui.Row(c.Key(folder)).MinHeight(38).Radius(7).Background(p.hover).PaddingX(9).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, folder).SingleLine().MinWidth(0).Grow(1).FontSize(12).TextColor(p.text).Tooltip(folder)
						ui.Button(c.Key("remove-folder"), "移除").Label("移除 " + folder).Disabled(a.local.busy).OnClick(func() {
							a.removeLocalFolder(folder)
						})
					})
				}
			})
		}
		if a.local.status != "" {
			ui.Text(c, a.local.status).FontSize(12).TextColor(p.muted)
		}
	})
}

func (a *app) localSongItems(tr *netease.Track) func(*ui.Menu) {
	if tr.Temporary {
		name := tr.Name
		return func(m *ui.Menu) {
			if m.Item("复制歌名").Chosen() {
				copyText(name)
			}
		}
	}
	track := *tr
	return func(m *ui.Menu) {
		song := a.local.song(track.LocalID)
		if song.Path == "" {
			return
		}
		if !a.local.missing[track.LocalID] {
			if m.Item("下一首播放").Chosen() {
				a.pl.Enqueue(track, true)
			}
			if m.Item("加入队列").Chosen() {
				a.pl.Enqueue(track, false)
			}
		}
		if !song.Scanned || a.local.missing[track.LocalID] {
			if !a.local.missing[track.LocalID] {
				m.Separator()
			}
			if m.Item("重新定位").Chosen() {
				a.chooseLocalFile(track.LocalID)
			}
			if m.Item("从曲库移除").Chosen() {
				a.removeLocalSong(track.LocalID)
			}
		}
	}
}
