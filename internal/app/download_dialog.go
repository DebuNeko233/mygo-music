package app

import (
	"github.com/egoist/mygo"
)

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
