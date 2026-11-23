package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/theme"
	"netease-music-mygo/internal/netease"
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

func (a *app) downloadSettings(c *ui.Context, p theme.Palette, row settingsRow, block settingsBlock) {
	s := &a.settings
	row("默认下载音质", "以网易云实际提供的下载音质为准", func() {
		if qualitySelect(c.Key("download-quality"), &s.DownloadQuality, "默认下载音质") {
			s.saveSoon()
		}
	})
	directory := a.downloadDirectory()
	if directory == "" {
		directory = "首次下载时选择"
	}
	row("下载目录", directory, func() {
		ui.Button(c.Key("download-directory"), "选择目录…").OnClick(func() { a.chooseDownloadDirectory() })
	})
	row("文件名格式", "对新加入下载队列的歌曲生效", func() {
		labels := make([]string, len(downloadNameFormats))
		chosen := downloadNameFormats[0].label
		for i, f := range downloadNameFormats {
			labels[i] = f.label
			if s.DownloadNameFormat == f.value {
				chosen = f.label
			}
		}
		if ui.Select(c.Key("download-name-format"), &chosen, labels).Label("下载文件名格式").Width(150).Changed() {
			for _, f := range downloadNameFormats {
				if chosen == f.label {
					s.DownloadNameFormat = f.value
					break
				}
			}
			s.saveSoon()
		}
	})

	row("另存歌词文件", "内嵌歌词之外，再保存同名 .lrc 文件", func() {
		if ui.Switch(c.Key("download-sidecar"), &s.DownloadSidecar).Label("另存歌词文件").Changed() {
			s.saveSoon()
		}
	})
	block("下载任务", func() { a.downloadJobs(c, p) })
}

func (a *app) downloadJobs(c *ui.Context, p theme.Palette) {
	ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
		if a.downloads.Err != "" {
			ui.Text(c, a.downloads.Err).FontSize(12).TextColor(p.Muted).Wrap()
		}
		if len(a.downloads.Jobs) == 0 {
			ui.Text(c, "日常播放有本地缓存，离线也可用，通常无需额外下载。需要在其他设备或播放器中使用时，再下载歌曲即可。").FontSize(12.5).TextColor(p.Muted).Wrap()
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "下载任务").FontSize(13).Grow(1)
			ui.Button(c.Key("clear-downloads"), "清除已完成").OnClick(func() {
				a.downloads.ClearFinished()
			})
		})
		ui.List(c.Key("downloads"), &a.downloadList, len(a.downloads.Jobs)).Height(280).ItemKey(func(i int) any { return a.downloads.Jobs[i] }).Rows(func(row ui.ListRow) {
			j := a.downloads.Jobs[row.Index]
			ui.Row(c).Padding(10, 0).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, downloadTitle(j.Track)).FontSize(13).MaxLines(1)
					status := j.Status
					if j.Total > 0 && !j.Finished {
						status += fmt.Sprintf(" %d%%", j.Received*100/j.Total)
					}
					if j.Result.Quality != "" {
						for _, q := range qualityLevels {
							if q.level == j.Result.Quality {
								label, _, _ := strings.Cut(q.label, "（")
								status += " · " + label
							}
						}
					}
					if j.Result.Format != "" {
						status += " · " + strings.ToUpper(j.Result.Format)
					}
					if j.Err != nil && !j.Cancelled {
						status += " · " + j.Err.Error()
					}
					if j.Result.Warning != "" {
						status += " · " + j.Result.Warning
					}
					ui.Text(c, status).FontSize(11.5).TextColor(p.Muted).MaxLines(2).Wrap()
				})
				switch {
				case !j.Finished:
					ui.Button(c.Key("cancel"), "取消").Disabled(j.Cancelled).OnClick(func() { a.cancelDownload(j) })
				case j.Result.Path != "":
					ui.Button(c.Key("reveal"), "显示文件").OnClick(func() { revealDownload(j.Result.Path) })
				default:
					ui.Button(c.Key("retry"), "重试").Disabled(!a.downloads.CanRetry(j)).OnClick(func() { a.retryDownload(j) })
				}
			})
		})
	})
}

func (a *app) downloadParent() *mygo.Window {
	if a.settingsWin != nil {
		return a.settingsWin
	}
	return a.win
}

func downloadTitle(tr netease.Track) string {
	if artists := tr.ArtistNames(); artists != "" {
		return tr.Name + " · " + artists
	}
	return tr.Name
}
