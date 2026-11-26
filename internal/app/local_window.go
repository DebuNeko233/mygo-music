/*
管理本地音乐目录窗口，并连接目录选择、歌曲展示与文件修复操作。
*/

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	artcache "netease-music-mygo/internal/ui/view/artwork"
)

type localViewState struct {
	window  *mygo.Window
	picking bool

	embeddedArtwork artcache.EmbeddedCache
}

var openLocalDialog = mygo.Dialog.Open

func (a *app) openLocalFolders() {
	if a.quitting {
		return
	}
	if a.localView.window != nil {
		a.localView.window.Show()
		a.localView.window.Focus()
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
	a.localView.window = w
	w.OnClosed(func() {
		if a.localView.window != w {
			return
		}
		a.localView.window = nil
		a.syncDock()
	})
	a.syncDock()
}

func (a *app) redrawLocalFolders() {
	if w := a.localView.window; w != nil {
		w.Invalidate()
	}
}

func (a *app) chooseLocalFile(id int64) {
	if a.local.Busy || a.local.Path == "" {
		return
	}
	a.localView.picking = true
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.win, Title: "重新定位音频文件",
		Filters: []mygo.FileFilter{{
			Name: "本地音频文件", Extensions: localExtensions,
			MIMETypes: []string{"audio/aac", "audio/aacp", "audio/3gpp", "audio/3gpp2", "audio/mp4", "audio/mp4a-latm", "audio/mpeg4-generic"},
		}},
	})
	a.localView.picking = false
	if err == nil && len(paths) != 0 {
		a.relinkLocalSong(id, paths[0])
	}
}

func (a *app) chooseLocalFolders() {
	if a.local.Busy || a.local.Path == "" {
		return
	}
	a.localView.picking = true
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.localView.window, Title: "选择音乐文件夹", Directory: true, Multiple: true,
	})
	a.localView.picking = false
	if err == nil {
		a.addLocalFolders(paths)
	}
}

func (a *app) localSongItems(tr *netease.Track, fromLibrary bool) func(*ui.Menu) {
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
		song := a.local.Song(track.LocalID)
		if song.Path == "" {
			return
		}
		missing := a.local.Missing[track.LocalID]
		queued := a.views.songMenu.QueueItems(m, track, !missing, fromLibrary)
		if !song.Scanned || missing {
			if queued {
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

func (a *app) localFoldersView(c *ui.Context) {
	t := a.settings.uiTheme(c.Theme())
	a.views.local.Folders(c, t, paletteFor(&t))
}
