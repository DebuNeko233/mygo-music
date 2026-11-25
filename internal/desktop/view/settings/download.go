/*
呈现下载偏好和任务列表，连接队列控制与目标目录设置。
*/

package settings

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

func (a *View) DownloadSettings(c *ui.Context, p components.Palette, row Row, block Block) {
	s := a.Config
	row("默认下载音质", "以网易云实际提供的下载音质为准", func() {
		if a.qualitySelect(c.Key("download-quality"), &s.DownloadQuality, "默认下载音质") {
			a.Save()
		}
	})
	directory := a.DownloadDirectory()
	if directory == "" {
		directory = "首次下载时选择"
	}
	row("下载目录", directory, func() {
		ui.Button(c.Key("download-directory"), "选择目录…").OnClick(func() { a.ChooseDownloadDirectory() })
	})
	row("文件名格式", "对新加入下载队列的歌曲生效", func() {
		labels := make([]string, len(nameFormats))
		chosen := nameFormats[0].Label
		for i, f := range nameFormats {
			labels[i] = f.Label
			if s.DownloadNameFormat == f.Value {
				chosen = f.Label
			}
		}
		if ui.Select(c.Key("download-name-format"), &chosen, labels).Label("下载文件名格式").Width(150).Changed() {
			for _, f := range nameFormats {
				if chosen == f.Label {
					s.DownloadNameFormat = f.Value
					break
				}
			}
			a.Save()
		}
	})

	row("另存歌词文件", "内嵌歌词之外，再保存同名 .lrc 文件", func() {
		if ui.Switch(c.Key("download-sidecar"), &s.DownloadSidecar).Label("另存歌词文件").Changed() {
			a.Save()
		}
	})
	block("下载任务", func() { a.DownloadJobs(c, p) })
}

func (a *View) DownloadJobs(c *ui.Context, p components.Palette) {
	ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
		if a.Downloads.Err != "" {
			ui.Text(c, a.Downloads.Err).FontSize(12).TextColor(p.Muted).Wrap()
		}
		if len(a.Downloads.Jobs) == 0 {
			ui.Text(c, "日常播放有本地缓存，离线也可用，通常无需额外下载。需要在其他设备或播放器中使用时，再下载歌曲即可。").FontSize(12.5).TextColor(p.Muted).Wrap()
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "下载任务").FontSize(13).Grow(1)
			ui.Button(c.Key("clear-downloads"), "清除已完成").OnClick(func() {
				a.Downloads.ClearFinished()
			})
		})
		ui.List(c.Key("downloads"), &a.State.DownloadList, len(a.Downloads.Jobs)).Height(280).ItemKey(func(i int) any { return a.Downloads.Jobs[i] }).Rows(func(row ui.ListRow) {
			j := a.Downloads.Jobs[row.Index]
			ui.Row(c).Padding(10, 0).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, downloadTitle(j.Track)).FontSize(13).MaxLines(1)
					status := j.Status
					if j.Total > 0 && !j.Finished {
						status += fmt.Sprintf(" %d%%", j.Received*100/j.Total)
					}
					if j.Result.Quality != "" {
						for _, q := range qualities {
							if q.Value == j.Result.Quality {
								label, _, _ := strings.Cut(q.Label, "（")
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
					ui.Button(c.Key("cancel"), "取消").Disabled(j.Cancelled).OnClick(func() { a.CancelDownload(j) })
				case j.Result.Path != "":
					ui.Button(c.Key("reveal"), "显示文件").OnClick(func() { a.RevealDownload(j.Result.Path) })
				default:
					ui.Button(c.Key("retry"), "重试").Disabled(!a.Downloads.CanRetry(j)).OnClick(func() { a.RetryDownload(j) })
				}
			})
		})
	})
}

func downloadTitle(tr netease.Track) string {
	if artists := tr.ArtistNames(); artists != "" {
		return tr.Name + " · " + artists
	}
	return tr.Name
}

var nameFormats = []Choice{
	{download.NameArtistTitle, "歌手 - 歌名"},
	{download.NameTitleArtist, "歌名 - 歌手"},
	{download.NameTitle, "仅歌名"},
}
