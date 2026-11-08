package app

import (
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/cachemove"
)

// openCacheDirectory asks for a folder, as the system's own picker does.
var openCacheDirectory = mygo.Dialog.Open

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
	if same && !upgrade {
		return nil
	}
	shared := upgrade || directory == ""
	if !shared {
		if err := cachemove.Validate(source.Root, target); err != nil {
			return err
		}
	}
	a.cacheMoving, a.cacheTarget, a.cacheError = true, target, ""
	cache := a.pl.Cache
	browse, images := a.browseCache, a.imageCache
	resume := a.pl.SuspendCache()
	go func() {
		unblock := cache.Quiesce()
		resumeBrowse, err := browse.Suspend()
		resumeImages := func(string) {}
		if err == nil {
			resumeImages, err = images.Suspend()
		}
		var warning error
		if err == nil {
			warning, err = cachemove.MoveLayout(source, target, migrate, shared, func() error {
				// Merge on the main thread so settings changed during the
				// copy are preserved. MoveLayout rolls back if saving fails.
				saved := make(chan error, 1)
				a.post(func() {
					next := a.settings
					next.CacheDir = directory
					next.CacheLayout = 1
					err := next.save()
					if err == nil {
						a.settings = next
					}
					saved <- err
				})
				return <-saved
			})
		}
		if err != nil {
			unblock()
			resumeBrowse("")
			resumeImages("")
		} else {
			resumeBrowse(filepath.Join(target, "browse-v1"))
			resumeImages(filepath.Join(target, cachemove.ArtworkDir))
		}
		a.post(func() {
			a.cacheMoving, a.cacheTarget = false, ""
			if err != nil {
				a.cacheError = "更改缓存目录失败，仍使用原目录：" + err.Error()
			} else {
				// The old audio cache stays shut: delayed downloads must
				// not recreate files at its former location.
				a.pl.Cache = newSongCache(a.settings)
				if warning != nil {
					a.cacheError = "新目录已启用，但部分旧缓存未能删除：" + warning.Error()
				}
			}
			resume()
			a.cacheUsed = -1
			a.measureCache()
			a.measureOfflineCache()
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
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
		desc += "\n正在更改为：" + a.cacheTarget + "\n完成后自动恢复播放，请勿退出应用"
	}
	if a.cacheError != "" {
		desc += "\n" + a.cacheError
	}
	return desc
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
