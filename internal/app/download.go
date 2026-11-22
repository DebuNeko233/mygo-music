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
)

var saveDownload = download.Save
var quitAfterDownload = mygo.App.Quit

var downloadNameFormats = []struct{ value, label string }{
	{download.NameArtistTitle, "歌手 - 歌名"},
	{download.NameTitleArtist, "歌名 - 歌手"},
	{download.NameTitle, "仅歌名"},
}

func (a *app) downloadDirectory() string {
	if a.settings.DownloadDirectory != "" {
		return a.settings.DownloadDirectory
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Music", "Mygo Music")
	}
	return ""
}

func (a *app) enqueueDownloads(tracks []netease.Track) {
	if a.quitting || a.downloads.quit {
		return
	}
	if !slices.ContainsFunc(tracks, downloadableTrack) {
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
	nameFormat := download.NameArtistTitle
	for _, f := range downloadNameFormats {
		if f.value == a.settings.DownloadNameFormat {
			nameFormat = f.value
			break
		}
	}

	options := download.Options{Directory: directory, Quality: level, NameFormat: nameFormat, Sidecar: a.settings.DownloadSidecar}

	count := a.downloads.enqueue(tracks, options, a.downloadServices())
	if count > 0 {
		a.toast = fmt.Sprintf("已加入 %d 首下载，可在设置的下载页面查看", count)
	}
}

// Application adapters supply the existing model services and UI dispatcher.
func (a *app) downloadServices() downloadServices {
	return downloadServices{
		save: saveDownload,
		post: func(update func() bool) {
			a.dispatch(func() {
				if update() && a.settingsWin != nil {
					a.settingsWin.Invalidate()
				}
			}, false)
		},
		cache: func() *player.Cache { return a.pl.Cache },
		quit:  quitAfterDownload,
	}
}

func (a *app) hasPendingDownload(key downloadKey) bool { return a.downloads.hasPending(key) }
func (a *app) retryDownload(j *downloadJob) {
	if !a.quitting {
		a.downloads.retry(j, a.downloadServices())
	}
}
func (a *app) cancelDownload(j *downloadJob) { a.downloads.cancel(j) }
func (a *app) cancelDownloads()              { a.downloads.cancelAll() }
func (a *app) deferDownloadQuit() bool       { return a.downloads.deferQuit() }
