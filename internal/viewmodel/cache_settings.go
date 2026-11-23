package viewmodel

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"netease-music-mygo/internal/cachemove"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/player"
)

type CacheConfirmation struct {
	Open                     bool
	Directory, Target, Error string
}

// CacheSettings owns progress and transaction lifetime; cachemove still owns filesystem rollback.
type CacheSettings struct {
	Used                     int64
	Error, Target            string
	Moving, Cancelling, Quit bool
	Copied, Total            int64
	run                      uint64
	cancel                   context.CancelFunc
	Confirm                  CacheConfirmation
}
type CacheChange struct {
	Source                     cachemove.Layout
	Target, Directory, Journal string
	Migrate, Shared, Default   bool
}
type CacheServices struct {
	Post                   func(func())
	Player                 *player.Player
	Browse, Images         *diskcache.Store
	Commit                 func(string) error
	NewCache               func() *player.Cache
	Measure, Changed, Quit func()
}

func (m *CacheSettings) Change(plan CacheChange, s CacheServices) error {
	if m.Moving {
		return errors.New("正在更改缓存目录，请稍候")
	}
	source, target, directory, journal := plan.Source, plan.Target, plan.Directory, plan.Journal
	migrate, shared, isDefault := plan.Migrate, plan.Shared, plan.Default
	ctx, cancel := context.WithCancel(context.Background())
	m.Moving, m.Target, m.Error = true, target, ""
	m.cancel, m.Cancelling = cancel, false
	m.Copied, m.Total = 0, 0
	m.run++
	run := m.run
	cache := s.Player.Cache
	browse, images := s.Browse, s.Images
	resume := s.Player.SuspendCache()
	go func() {
		defer cancel()
		unblock := cache.Quiesce()
		resumeBrowse, err := browse.Suspend()
		resumeImages := func(string) {}
		if err == nil {
			resumeImages, err = images.Suspend()
		}
		var warning error
		if err == nil {
			lastPercent := -1
			options := cachemove.Options{Context: ctx, Journal: journal, Progress: func(copied, total int64) {
				percent := 0
				if total > 0 {
					percent = int(copied * 100 / total)
				}
				if percent == lastPercent {
					return
				}
				lastPercent = percent
				s.Post(func() {
					if m.Moving && m.run == run {
						m.Copied, m.Total = copied, total
					}
				})
			}}
			warning, err = cachemove.MoveLayoutWithOptions(source, target, migrate, shared, func() error {
				// Merge on the main thread so settings changed during the
				// copy are preserved. The move rolls back if saving fails.
				saved := make(chan error, 1)
				s.Post(func() {
					if err := ctx.Err(); err != nil {
						saved <- err
						return
					}
					err := s.Commit(directory)
					if err == nil {
						// Publication ends cancellation before retiring old stores.
						m.cancel = nil
					}

					saved <- err
				})
				if err := <-saved; err != nil {
					return err
				}
				// Rebind while readers can still access the originals. Only
				// after this callback returns may the move retire them.
				resumeBrowse(filepath.Join(target, "browse-v1"))
				resumeImages(filepath.Join(target, cachemove.ArtworkDir))
				return nil
			}, options)
		}
		if err != nil {
			unblock()
			resumeBrowse("")
			resumeImages("")
		}
		s.Post(func() {
			m.Moving, m.Target = false, ""
			m.cancel, m.Cancelling = nil, false
			if err == context.Canceled {
				m.Error = "已取消迁移，仍使用原目录"
			} else if err != nil {
				m.Error = "更改缓存目录失败，仍使用原目录：" + err.Error()
				if migrate && isDefault && errors.Is(err, cachemove.ErrConflict) {
					m.Error += "；可选择“仅更改目录”使用目标缓存"
				}
			} else {
				// The old audio cache stays shut: delayed downloads must
				// not recreate files at its former location.
				s.Player.Cache = s.NewCache()
				if warning != nil {
					m.Error = "新目录已启用，但迁移清理未完成：" + warning.Error()
				}
			}
			if m.Quit {
				s.Quit()
				return
			}
			resume()
			m.Used = -1
			s.Measure()
			s.Changed()
		})
	}()
	return nil
}

func (m *CacheSettings) Cancel() {
	if m.cancel != nil {
		m.Cancelling = true
		m.cancel()
	}
}
func (m *CacheSettings) DeferQuit() bool {
	if !m.Moving {
		return false
	}
	m.Quit = true
	m.Cancel()
	return true
}
func (m *CacheSettings) Label() string {
	if m.Cancelling {
		return "正在取消缓存迁移…"
	}
	if m.Target == "" {
		return "正在清理上次缓存迁移…"
	}
	if m.Total > 0 {
		return fmt.Sprintf("正在迁移缓存 %d%%", m.Copied*100/m.Total)
	}
	return "正在准备缓存迁移…"
}

func (m *CacheSettings) Cancelable() bool { return m.cancel != nil }
