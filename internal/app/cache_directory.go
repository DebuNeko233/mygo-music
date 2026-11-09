package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/cachemove"
)

// openCacheDirectory asks for a folder, as the system's own picker does.
var openCacheDirectory = mygo.Dialog.Open
var quitAfterCacheMove = mygo.App.Quit

// changeCacheDirectory suspends all stores before moving them off the UI
// thread. Settings commit only after a successful copy; failure resumes the
// original stores. Playback resumes with any controls changed during the move.
func (a *app) changeCacheDirectory(directory string, migrate bool) error {
	if a.cacheMoving {
		return errors.New("正在更改缓存目录，请稍候")
	}
	source, err := cachePaths(a.settings)
	if err != nil {
		return err
	}
	target, err := cacheDirectory(directory)
	if err != nil {
		return err
	}
	same, err := cachemove.SameDirectory(source.Root, target)
	if err != nil {
		return err
	}
	upgrade := same && a.settings.CacheLayout == 0
	system, err := cacheDirectory("")
	if err != nil {
		return err
	}
	isDefault, err := cachemove.SameDirectory(target, system)
	if err != nil {
		return err
	}
	if isDefault {
		directory = ""
	}
	if same && !upgrade {
		// Older settings may name the system root explicitly. Restoring
		// defaults changes that preference without moving the same files.
		if isDefault && a.settings.CacheDir != "" {
			next := a.settings
			next.CacheDir = ""
			if err := next.save(); err != nil {
				return err
			}
			a.settings = next
		}
		return nil
	}
	shared := upgrade || isDefault
	if !shared {
		if err := cachemove.Validate(source.Root, target); err != nil {
			return err
		}
	}
	journal, err := cacheMoveJournal()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cacheMoving, a.cacheTarget, a.cacheError = true, target, ""
	a.cacheCancel, a.cacheCancelling = cancel, false
	a.cacheCopied, a.cacheTotal = 0, 0
	a.cacheRun++
	run := a.cacheRun
	cache := a.pl.Cache
	browse, images := a.browseCache, a.imageCache
	resume := a.pl.SuspendCache()
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
				a.post(func() {
					if a.cacheMoving && a.cacheRun == run {
						a.cacheCopied, a.cacheTotal = copied, total
					}
				})
			}}
			warning, err = cachemove.MoveLayoutWithOptions(source, target, migrate, shared, func() error {
				// Merge on the main thread so settings changed during the
				// copy are preserved. MoveLayout rolls back if saving fails.
				saved := make(chan error, 1)
				a.post(func() {
					if err := ctx.Err(); err != nil {
						saved <- err
						return
					}
					next := a.settings
					next.CacheDir = directory
					next.CacheLayout = 1
					err := next.save()
					if err == nil {
						a.settings = next
						a.cacheCancel = nil // the published transaction is now committed
					}
					saved <- err
				})
				if err := <-saved; err != nil {
					return err
				}
				// Rebind while readers can still access the originals. Only
				// after this callback returns may MoveLayout retire them.
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
		a.post(func() {
			a.cacheMoving, a.cacheTarget = false, ""
			a.cacheCancel, a.cacheCancelling = nil, false
			if errors.Is(err, context.Canceled) {
				a.cacheError = "已取消迁移，仍使用原目录"
			} else if err != nil {
				a.cacheError = "更改缓存目录失败，仍使用原目录：" + err.Error()
				if migrate && isDefault && errors.Is(err, cachemove.ErrConflict) {
					a.cacheError += "；可选择“仅更改目录”使用目标缓存"
				}
			} else {
				// The old audio cache stays shut: delayed downloads must
				// not recreate files at its former location.
				a.pl.Cache = newSongCache(a.settings)
				if warning != nil {
					a.cacheError = "新目录已启用，但部分旧缓存未能删除：" + warning.Error()
				}
			}
			if a.cacheQuit {
				quitAfterCacheMove()
				return
			}
			resume()
			a.cacheUsed = -1
			a.measureCache()
			a.measureOfflineCache()
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
			a.redraw()
		})
	}()
	return nil
}

// chooseCacheDirectory asks for the shared cache root.
func (a *app) chooseCacheDirectory() {
	if a.cacheMoving {
		return
	}
	paths, err := openCacheDirectory(mygo.OpenDialogOptions{
		Parent: a.settingsWin, Title: "选择空文件夹作为缓存目录", Directory: true, CreateDirectories: true,
	})
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	if len(paths) == 0 {
		// The picker was closed: nothing asked for.
		return
	}
	a.confirmCacheDirectory(filepath.Clean(paths[0]))
}

// confirmCacheDirectory asks whether to move existing caches to directory.
func (a *app) confirmCacheDirectory(directory string) {
	if a.cacheMoving {
		return
	}
	target, err := cacheDirectory(directory)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	source, err := cacheDirectory(a.settings.CacheDir)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	same, err := cachemove.SameDirectory(source, target)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	if same && a.settings.CacheLayout != 0 {
		if err := a.changeCacheDirectory(directory, false); err != nil {
			a.cacheError = err.Error()
		}
		return
	}
	a.cacheConfirm = cacheDirectoryConfirmation{open: true, directory: directory, target: target}
}

// submitCacheDirectory moves all caches when requested; otherwise old files stay.
func (a *app) submitCacheDirectory(migrate bool) {
	d := &a.cacheConfirm
	if err := a.changeCacheDirectory(d.directory, migrate); err != nil {
		d.err = err.Error()
		return
	}
	d.open = false
}

// cacheDirectoryDescription shows the root and any conversion/move failure.
func (a *app) cacheDirectoryDescription() string {
	dir, err := cacheDirectory(a.settings.CacheDir)
	if err != nil {
		return err.Error()
	}
	desc := dir
	if a.cacheMoving {
		desc += "\n" + a.cacheMoveLabel()
	}
	if a.cacheError != "" {
		desc += "\n" + a.cacheError
	}
	return desc
}

func cacheMoveJournal() (string, error) {
	root, err := mygo.App.Path(mygo.PathUserData)
	return filepath.Join(root, "cache-move.json"), err
}

// Cleanup runs before any new transaction, without holding up account/page
// loading. The same guard prevents another move from replacing its journal.
func (a *app) recoverCacheDirectory() {
	journal, err := cacheMoveJournal()
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	a.cacheMoving = true
	go func() {
		err := cachemove.Recover(journal)
		a.post(func() {
			a.cacheMoving = false
			if a.cacheQuit {
				quitAfterCacheMove()
				return
			}
			if err != nil {
				a.cacheError = "清理上次缓存迁移失败：" + err.Error()
				a.redraw()
				return
			}
			a.upgradeCacheDirectory()
		})
	}()
}

func (a *app) cancelCacheChange() {
	if a.cacheCancel != nil {
		a.cacheCancelling = true
		a.cacheCancel()
		a.redraw()
	}
}

// Quit waits for rollback, rather than silently refusing the user's request.
func (a *app) deferCacheQuit() bool {
	if !a.cacheMoving {
		return false
	}
	a.cacheQuit = true
	a.cancelCacheChange()
	return true
}

func (a *app) cacheMoveLabel() string {
	if a.cacheCancelling {
		return "正在取消缓存迁移…"
	}
	if a.cacheTarget == "" {
		return "正在清理上次缓存迁移…"
	}
	if a.cacheTotal > 0 {
		return fmt.Sprintf("正在迁移缓存 %d%%", a.cacheCopied*100/a.cacheTotal)
	}
	return "正在准备缓存迁移…"
}

// upgradeCacheDirectory runs once for settings written before caches shared a
// root. The layout version is committed with the files, so failure/restart
// still opens every old location and conversion can be retried next launch.
func (a *app) upgradeCacheDirectory() {
	if a.settings.CacheLayout != 0 {
		return
	}
	if err := a.changeCacheDirectory(a.settings.CacheDir, true); err != nil {
		a.cacheError = "转换缓存目录失败，仍使用原路径：" + err.Error()
	}
}
