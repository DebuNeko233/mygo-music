package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

type downloadJob struct {
	track               netease.Track
	options             download.Options
	status              string
	received, total     int64
	result              download.Result
	err                 error
	cancel              context.CancelFunc
	finished, cancelled bool
}

// One pending transfer per source and output request.
type downloadKey struct {
	id                             int64
	cloud                          bool
	directory, quality, nameFormat string
	sidecar                        bool
}

func (j *downloadJob) key() downloadKey {
	return downloadKey{j.track.ID, j.track.Cloud, j.options.Directory, j.options.Quality, j.options.NameFormat, j.options.Sidecar}
}

type downloadState struct {
	jobs   []*downloadJob
	active *downloadJob
	list   ui.ListState
	err    string
	quit   bool
}

var downloadNameFormats = []struct{ value, label string }{
	{download.NameArtistTitle, "歌手 - 歌名"},
	{download.NameTitleArtist, "歌名 - 歌手"},
	{download.NameTitle, "仅歌名"},
}

var saveDownload = download.Save
var openDownloadDirectory = mygo.Dialog.Open
var revealDownload = mygo.Shell.ShowItemInFolder
var quitAfterDownload = mygo.App.Quit

func (a *app) downloadDirectory() string {
	if a.settings.DownloadDirectory != "" {
		return a.settings.DownloadDirectory
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Music", "Mygo Music")
	}
	return ""
}

func (a *app) chooseDownloadDirectory() bool {
	paths, err := openDownloadDirectory(mygo.OpenDialogOptions{
		Parent: a.downloadParent(), Title: "选择下载目录", Directory: true, CreateDirectories: true,
	})
	if err != nil {
		a.downloads.err = err.Error()
		return false
	}
	if len(paths) == 0 {
		return false
	}
	a.settings.DownloadDirectory = paths[0]
	a.settings.saveSoon()
	a.downloads.err = ""
	return true
}

func (a *app) enqueueDownloads(tracks []netease.Track) {
	if a.quitting || a.downloads.quit {
		return
	}
	tracks = slices.DeleteFunc(slices.Clone(tracks), func(tr netease.Track) bool { return tr.IsLocal() || tr.ProgramID != 0 || tr.ID <= 0 })
	if len(tracks) == 0 {
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

	seen := make(map[downloadKey]bool, len(a.downloads.jobs)+len(tracks))
	for _, j := range a.downloads.jobs {
		if !j.finished {
			seen[j.key()] = true
		}
	}

	count := 0
	for _, tr := range tracks {
		job := &downloadJob{track: tr, status: "等待下载", options: options}
		key := job.key()
		if seen[key] {
			continue
		}
		seen[key] = true
		a.downloads.jobs = append(a.downloads.jobs, job)
		count++
	}
	if count > 0 {
		a.toast = fmt.Sprintf("已加入 %d 首下载，可在设置的下载页面查看", count)
	}
	a.startNextDownload()
}

func (a *app) startNextDownload() {
	if a.downloads.active != nil || a.quitting || a.downloads.quit {
		return
	}
	for _, j := range a.downloads.jobs {
		if j.finished {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		j.cancel, j.status = cancel, "正在获取下载地址"
		a.downloads.active = j
		opts := j.options
		opts.Cache = a.pl.Cache
		opts.Progress = func(stage string, received, total int64) {
			a.postDownload(func() {
				if a.downloads.active == j && !j.cancelled {
					j.status, j.received, j.total = stage, received, total
				}
			})
		}
		save := saveDownload
		go func() {
			result, err := save(ctx, j.track, opts)
			cancel()
			a.postDownload(func() {
				j.cancel, j.finished, j.result, j.err = nil, true, result, err
				switch {
				case result.Path != "":
					j.status = "下载完成"
				case j.cancelled || errors.Is(err, context.Canceled):
					j.status = "已取消"
					j.cancelled = true
				case err != nil:
					j.status = "下载失败"
				default:
					j.status = "下载失败"
					j.err = errors.New("未生成下载文件")
				}
				if a.downloads.active == j {
					a.downloads.active = nil
				}
				if a.downloads.quit {
					quitAfterDownload()
					return
				}
				a.startNextDownload()
			})
		}()
		return
	}
}

func (a *app) hasPendingDownload(key downloadKey) bool {
	return slices.ContainsFunc(a.downloads.jobs, func(j *downloadJob) bool { return !j.finished && j.key() == key })
}

func (a *app) retryDownload(j *downloadJob) {
	if !j.finished || a.hasPendingDownload(j.key()) {
		return
	}
	// Keep its list identity and original request; clear every prior result.
	*j = downloadJob{track: j.track, options: j.options, status: "等待下载"}
	a.startNextDownload()
}

func (a *app) cancelDownload(j *downloadJob) {
	if j.finished {
		return
	}
	j.cancelled, j.status = true, "正在取消"
	if j.cancel != nil {
		j.cancel()
	} else {
		j.finished, j.status = true, "已取消"
	}
}

// Account transitions cannot let queued private songs run under a new session.
func (a *app) cancelDownloads() {
	for _, j := range a.downloads.jobs {
		a.cancelDownload(j)
	}
}

func (a *app) downloadJobs(c *ui.Context, p palette) {
	ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
		if a.downloads.err != "" {
			ui.Text(c, a.downloads.err).FontSize(12).TextColor(p.muted).Wrap()
		}
		if len(a.downloads.jobs) == 0 {
			ui.Text(c, "日常播放有本地缓存，离线也可用，通常无需额外下载。需要在其他设备或播放器中使用时，再下载歌曲即可。").FontSize(12.5).TextColor(p.muted).Wrap()
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "下载任务").FontSize(13).Grow(1)
			ui.Button(c.Key("clear-downloads"), "清除已完成").OnClick(func() {
				a.downloads.jobs = slices.DeleteFunc(a.downloads.jobs, func(j *downloadJob) bool { return j.finished })
			})
		})
		ui.List(c.Key("downloads"), &a.downloads.list, len(a.downloads.jobs)).Height(280).ItemKey(func(i int) any { return a.downloads.jobs[i] }).Rows(func(row ui.ListRow) {
			j := a.downloads.jobs[row.Index]
			ui.Row(c).Padding(10, 0).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, downloadTitle(j.track)).FontSize(13).MaxLines(1)
					status := j.status
					if j.total > 0 && !j.finished {
						status += fmt.Sprintf(" %d%%", j.received*100/j.total)
					}
					if j.result.Quality != "" {
						for _, q := range qualityLevels {
							if q.level == j.result.Quality {
								label, _, _ := strings.Cut(q.label, "（")
								status += " · " + label
							}
						}
					}
					if j.result.Format != "" {
						status += " · " + strings.ToUpper(j.result.Format)
					}
					if j.err != nil && !j.cancelled {
						status += " · " + j.err.Error()
					}
					if j.result.Warning != "" {
						status += " · " + j.result.Warning
					}
					ui.Text(c, status).FontSize(11.5).TextColor(p.muted).MaxLines(2).Wrap()
				})
				switch {
				case !j.finished:
					ui.Button(c.Key("cancel"), "取消").Disabled(j.cancelled).OnClick(func() { a.cancelDownload(j) })
				case j.result.Path != "":
					ui.Button(c.Key("reveal"), "显示文件").OnClick(func() { revealDownload(j.result.Path) })
				default:
					ui.Button(c.Key("retry"), "重试").Disabled(a.hasPendingDownload(j.key())).OnClick(func() { a.retryDownload(j) })
				}
			})
		})
	})
}

func (a *app) postDownload(fn func()) {
	a.dispatch(func() {
		fn()
		if a.settingsWin != nil {
			a.settingsWin.Invalidate()
		}
	}, false)
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

// Let the worker close its files and clean staging data before the process exits.
func (a *app) deferDownloadQuit() bool {
	if a.downloads.active == nil {
		return false
	}
	a.downloads.quit = true
	a.cancelDownloads()
	return true
}
