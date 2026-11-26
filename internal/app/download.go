/*
为下载模型连接播放器与存储服务，协调入队、重试和目标目录选择。
*/

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	downloadsvm "netease-music-mygo/internal/viewmodel/downloads"
)

var saveDownload = download.Save

var quitAfterDownload = mygo.App.Quit

func (a *app) downloadDirectory() string {
	if a.settings.DownloadDirectory != "" {
		return a.settings.DownloadDirectory
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Music", "Nyago Music")
	}
	return ""
}

func (a *app) enqueueDownloads(tracks []netease.Track) {
	if a.quitting || a.downloads.Quitting() {
		return
	}
	if !slices.ContainsFunc(tracks, downloadsvm.CanDownload) {
		return
	}
	directory := a.downloadDirectory()
	if directory == "" {
		if !a.chooseDownloadDirectory() {
			return
		}
		directory = a.settings.DownloadDirectory
	}
	level := a.settings.DownloadQuality
	if !slices.Contains(netease.Levels, level) {
		level = "exhigh"
	}
	nameFormat := a.settings.DownloadNameFormat
	switch nameFormat {
	case download.NameTitleArtist, download.NameTitle:
	default:
		nameFormat = download.NameArtistTitle
	}

	options := download.Options{Directory: directory, Quality: level, NameFormat: nameFormat, Sidecar: a.settings.DownloadSidecar}

	count := a.downloads.Enqueue(tracks, options, a.downloadServices())
	if count > 0 {
		a.toast = fmt.Sprintf("已加入 %d 首下载，可在设置的下载页面查看", count)
	}
}

// Application adapters supply the existing model services and UI dispatcher.
func (a *app) downloadServices() downloadsvm.DownloadServices {
	return downloadsvm.DownloadServices{
		Save: saveDownload,
		Post: func(update func() bool) {
			a.dispatch(func() {
				if update() && a.settingsWin != nil {
					a.settingsWin.Invalidate()
				}
			}, false)
		},
		Cache: func() *player.Cache { return a.pl.Cache },
		Quit:  quitAfterDownload,
	}
}

func (a *app) retryDownload(j *downloadsvm.DownloadJob) {
	if !a.quitting {
		a.downloads.Retry(j, a.downloadServices())
	}
}

var openDownloadDirectory = mygo.Dialog.Open

var revealDownload = mygo.Shell.ShowItemInFolder

func (a *app) chooseDownloadDirectory() bool {
	paths, err := openDownloadDirectory(mygo.OpenDialogOptions{
		Parent: a.downloadParent(), Title: "选择下载目录", Directory: true, CreateDirectories: true,
	})
	if err != nil {
		a.downloads.Err = err.Error()
		return false
	}
	if len(paths) == 0 {
		return false
	}
	a.settings.DownloadDirectory = paths[0]
	a.settings.saveSoon()
	a.downloads.Err = ""
	return true
}

func (a *app) downloadParent() *mygo.Window {
	if a.settingsWin != nil {
		return a.settingsWin
	}
	return a.win
}
