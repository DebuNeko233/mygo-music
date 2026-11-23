package viewmodel

import (
	"context"
	"errors"
	"slices"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// DownloadJob is a stable row with a captured output request and published result.
// Views borrow it read-only; retries keep the same row and replace its execution.
type DownloadJob struct {
	Track               netease.Track
	Options             download.Options
	Status              string
	Received, Total     int64
	Result              download.Result
	Err                 error
	Finished, Cancelled bool
}

type downloadKey struct {
	id                             int64
	cloud                          bool
	directory, quality, nameFormat string
	sidecar                        bool
}

func downloadRequestKey(track netease.Track, options download.Options) downloadKey {
	return downloadKey{track.ID, track.Cloud, options.Directory, options.Quality, options.NameFormat, options.Sidecar}
}

func (j *DownloadJob) key() downloadKey { return downloadRequestKey(j.Track, j.Options) }

// A row keeps its identity across retries. An execution never does: callbacks
// must match this attempt, rather than merely the row they originally targeted.
type downloadRun struct {
	job    *DownloadJob
	cancel context.CancelFunc
}

// Downloads owns the application-wide sequential queue, independently of views.
type Downloads struct {
	Jobs   []*DownloadJob
	active *downloadRun
	Err    string
	quit   bool
}

// DownloadServices connects existing export/cache services to the owning thread.
// Cache is resolved when an execution starts, not when its request is enqueued.
type DownloadServices struct {
	Save  func(context.Context, netease.Track, download.Options) (download.Result, error)
	Post  func(func() bool)
	Cache func() *player.Cache
	Quit  func()
}

func CanDownload(tr netease.Track) bool {
	return !tr.IsLocal() && tr.ProgramID == 0 && tr.ID > 0
}

// Enqueue snapshots each output request and deduplicates only pending jobs.
func (m *Downloads) Enqueue(tracks []netease.Track, options download.Options, s DownloadServices) int {
	if m.quit {
		return 0
	}
	seen := make(map[downloadKey]bool, len(m.Jobs)+len(tracks))
	for _, j := range m.Jobs {
		if !j.Finished {
			seen[j.key()] = true
		}
	}
	count := 0
	for _, tr := range tracks {
		if !CanDownload(tr) {
			continue
		}
		key := downloadRequestKey(tr, options)
		if seen[key] {
			continue
		}
		seen[key] = true
		m.Jobs = append(m.Jobs, &DownloadJob{Track: tr, Status: "等待下载", Options: options})
		count++
	}
	m.startNext(s)
	return count
}

func (m *Downloads) startNext(s DownloadServices) {
	if m.active != nil || m.quit {
		return
	}
	for _, j := range m.Jobs {
		if j.Finished {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		run := &downloadRun{job: j, cancel: cancel}
		m.active, j.Status = run, "正在获取下载地址"
		track, opts := j.Track, j.Options
		opts.Cache = s.Cache()
		opts.Progress = func(stage string, received, total int64) {
			s.Post(func() bool {
				if m.active != run || j.Cancelled {
					return false
				}
				j.Status, j.Received, j.Total = stage, received, total
				return true
			})
		}
		go func() {
			result, err := s.Save(ctx, track, opts)
			cancel()
			s.Post(func() bool {
				if m.active != run {
					return false
				}
				j.Finished, j.Result, j.Err = true, result, err
				switch {
				case result.Path != "":
					j.Status = "下载完成"
				case j.Cancelled || errors.Is(err, context.Canceled):
					j.Status, j.Cancelled = "已取消", true
				case err != nil:
					j.Status = "下载失败"
				default:
					j.Status, j.Err = "下载失败", errors.New("未生成下载文件")
				}
				m.active = nil
				if m.quit {
					s.Quit()
				} else {
					m.startNext(s)
				}
				return true
			})
		}()
		return
	}
}

func (m *Downloads) hasPending(key downloadKey) bool {
	return slices.ContainsFunc(m.Jobs, func(j *DownloadJob) bool { return !j.Finished && j.key() == key })
}

func (m *Downloads) Retry(j *DownloadJob, s DownloadServices) {
	if !m.CanRetry(j) {
		return
	}
	*j = DownloadJob{Track: j.Track, Options: j.Options, Status: "等待下载"}
	m.startNext(s)
}

func (m *Downloads) Cancel(j *DownloadJob) {
	if j.Finished {
		return
	}
	j.Cancelled, j.Status = true, "正在取消"
	if m.active != nil && m.active.job == j {
		m.active.cancel()
	} else {
		j.Finished, j.Status = true, "已取消"
	}
}

func (m *Downloads) CancelAll() {
	for _, j := range m.Jobs {
		m.Cancel(j)
	}
}

func (m *Downloads) ClearFinished() {
	m.Jobs = slices.DeleteFunc(m.Jobs, func(j *DownloadJob) bool { return j.Finished })
}

func (m *Downloads) DeferQuit() bool {
	if m.active == nil {
		return false
	}
	m.quit = true
	m.CancelAll()
	return true
}

// Busy reports a running execution, including cancellation cleanup.
func (m *Downloads) Busy() bool { return m.active != nil }

// Quitting prevents new work while the current execution releases its resources.
func (m *Downloads) Quitting() bool { return m.quit }

// CanRetry uses the same output identity as enqueue, preserving row identity
// without allowing another pending transfer for the same output request.
func (m *Downloads) CanRetry(j *DownloadJob) bool {
	return !m.quit && j.Finished && !m.hasPending(j.key())
}
