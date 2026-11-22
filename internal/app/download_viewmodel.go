package app

import (
	"context"
	"errors"
	"slices"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

type downloadJob struct {
	track               netease.Track
	options             download.Options
	status              string
	received, total     int64
	result              download.Result
	err                 error
	finished, cancelled bool
}

type downloadKey struct {
	id                             int64
	cloud                          bool
	directory, quality, nameFormat string
	sidecar                        bool
}

func (j *downloadJob) key() downloadKey {
	return downloadKey{j.track.ID, j.track.Cloud, j.options.Directory, j.options.Quality, j.options.NameFormat, j.options.Sidecar}
}

// A row keeps its identity across retries. An execution never does: callbacks
// must match this attempt, rather than merely the row they originally targeted.
type downloadRun struct {
	job    *downloadJob
	cancel context.CancelFunc
}

type downloadViewModel struct {
	jobs   []*downloadJob
	active *downloadRun
	err    string
	quit   bool
}

type downloadServices struct {
	save  func(context.Context, netease.Track, download.Options) (download.Result, error)
	post  func(func() bool)
	cache func() *player.Cache
	quit  func()
}

func downloadableTrack(tr netease.Track) bool {
	return !tr.IsLocal() && tr.ProgramID == 0 && tr.ID > 0
}

// Enqueue snapshots each output request and deduplicates only pending jobs.
func (m *downloadViewModel) enqueue(tracks []netease.Track, options download.Options, s downloadServices) int {
	if m.quit {
		return 0
	}
	seen := make(map[downloadKey]bool, len(m.jobs)+len(tracks))
	for _, j := range m.jobs {
		if !j.finished {
			seen[j.key()] = true
		}
	}
	count := 0
	for _, tr := range tracks {
		if !downloadableTrack(tr) {
			continue
		}
		job := &downloadJob{track: tr, status: "等待下载", options: options}
		key := job.key()
		if seen[key] {
			continue
		}
		seen[key] = true
		m.jobs = append(m.jobs, job)
		count++
	}
	m.startNext(s)
	return count
}

func (m *downloadViewModel) startNext(s downloadServices) {
	if m.active != nil || m.quit {
		return
	}
	for _, j := range m.jobs {
		if j.finished {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		run := &downloadRun{job: j, cancel: cancel}
		m.active, j.status = run, "正在获取下载地址"
		track, opts := j.track, j.options
		opts.Cache = s.cache()
		opts.Progress = func(stage string, received, total int64) {
			s.post(func() bool {
				if m.active != run || j.cancelled {
					return false
				}
				j.status, j.received, j.total = stage, received, total
				return true
			})
		}
		go func() {
			result, err := s.save(ctx, track, opts)
			cancel()
			s.post(func() bool {
				if m.active != run {
					return false
				}
				j.finished, j.result, j.err = true, result, err
				switch {
				case result.Path != "":
					j.status = "下载完成"
				case j.cancelled || errors.Is(err, context.Canceled):
					j.status, j.cancelled = "已取消", true
				case err != nil:
					j.status = "下载失败"
				default:
					j.status, j.err = "下载失败", errors.New("未生成下载文件")
				}
				m.active = nil
				if m.quit {
					s.quit()
				} else {
					m.startNext(s)
				}
				return true
			})
		}()
		return
	}
}

func (m *downloadViewModel) hasPending(key downloadKey) bool {
	return slices.ContainsFunc(m.jobs, func(j *downloadJob) bool { return !j.finished && j.key() == key })
}

func (m *downloadViewModel) retry(j *downloadJob, s downloadServices) {
	if m.quit || !j.finished || m.hasPending(j.key()) {
		return
	}
	*j = downloadJob{track: j.track, options: j.options, status: "等待下载"}
	m.startNext(s)
}

func (m *downloadViewModel) cancel(j *downloadJob) {
	if j.finished {
		return
	}
	j.cancelled, j.status = true, "正在取消"
	if m.active != nil && m.active.job == j {
		m.active.cancel()
	} else {
		j.finished, j.status = true, "已取消"
	}
}

func (m *downloadViewModel) cancelAll() {
	for _, j := range m.jobs {
		m.cancel(j)
	}
}

func (m *downloadViewModel) clearFinished() {
	m.jobs = slices.DeleteFunc(m.jobs, func(j *downloadJob) bool { return j.finished })
}

func (m *downloadViewModel) deferQuit() bool {
	if m.active == nil {
		return false
	}
	m.quit = true
	m.cancelAll()
	return true
}
