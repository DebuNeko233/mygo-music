package app

import (
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/cachemove"
)

// openCacheDirectory asks for a folder, as the system's own picker does.
var openCacheDirectory = mygo.Dialog.Open

// postCacheDirectory runs fn on the main thread: tests swap it out, having no
// window to post to.
var postCacheDirectory = func(a *app, fn func()) { a.post(fn) }

// changeCacheDirectory keeps the songs in directory from now on, and moves
// the ones kept there when migrate says: what plays is let go of first, so
// that no file of the folder is open as it moves, and plays again from where
// it was once it has. Nothing waits for the copy: it runs on a goroutine,
// with the songs counted, and the queue still taking a next track, a seek or
// a toggle, which apply when playback comes back.
func (a *app) changeCacheDirectory(directory string, migrate bool) error {
	if a.cacheMoving {
		return errors.New("正在更改缓存目录，请稍候")
	}
	source, err := songCacheDirectory(a.settings.CacheDir)
	if err != nil {
		return err
	}
	target, err := songCacheDirectory(directory)
	if err != nil {
		return err
	}
	same, err := cachemove.SameDirectory(source, target)
	if err != nil || same {
		// The folder the songs are in already: nothing to change.
		return err
	}
	if err := cachemove.Validate(source, target); err != nil {
		return err
	}
	a.cacheMoving, a.cacheTarget, a.cacheError = true, target, ""
	cache := a.pl.Cache
	resume := a.pl.SuspendCache()
	go func() {
		unblock := cache.Quiesce()
		warning, err := cachemove.Move(source, target, migrate, func() error {
			// The settings are saved where they are read and changed: on
			// the main thread, from the ones there then, so that what the
			// listener changed as the songs copied is kept.
			saved := make(chan error, 1)
			postCacheDirectory(a, func() {
				next := a.settings
				next.CacheDir = directory
				err := next.save()
				if err == nil {
					a.settings = next
				}
				saved <- err
			})
			return <-saved
		})
		if err != nil {
			// The songs stay where they were: the cache takes them again.
			unblock()
		}
		postCacheDirectory(a, func() {
			a.cacheMoving, a.cacheTarget = false, ""
			if err != nil {
				a.cacheError = "更改缓存目录失败，仍使用原目录：" + err.Error()
			} else {
				// The old folder is gone: its cache stays shut, so that a
				// load that came late writes nothing into it.
				a.pl.Cache = newSongCache(a.settings.CacheMB, a.settings.CacheDir)
				if warning != nil {
					a.cacheError = "新目录已启用，但部分旧缓存未能删除：" + warning.Error()
				}
			}
			resume()
			a.cacheUsed = -1
			a.measureCache()
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
	return nil
}

// chooseCacheDirectory asks for the folder to keep the songs in.
func (a *app) chooseCacheDirectory() {
	if a.cacheMoving {
		return
	}
	paths, err := openCacheDirectory(mygo.OpenDialogOptions{
		Parent: a.settingsWin, Title: "选择空文件夹作为歌曲缓存目录", Directory: true, CreateDirectories: true,
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

// confirmCacheDirectory asks whether to move the songs kept to directory, or
// only to keep the ones after them there.
func (a *app) confirmCacheDirectory(directory string) {
	if a.cacheMoving {
		return
	}
	target, err := songCacheDirectory(directory)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	source, err := songCacheDirectory(a.settings.CacheDir)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	same, err := cachemove.SameDirectory(source, target)
	if err != nil {
		a.cacheError = err.Error()
		return
	}
	if same {
		return
	}
	a.cacheConfirm = cacheDirectoryConfirmation{open: true, directory: directory, target: target}
}

// submitCacheDirectory does what the confirmation asks: with migrate, the
// songs kept move; without, they stay where they are.
func (a *app) submitCacheDirectory(migrate bool) {
	d := &a.cacheConfirm
	if err := a.changeCacheDirectory(d.directory, migrate); err != nil {
		d.err = err.Error()
		return
	}
	d.open = false
}

// cacheDirectoryDescription says where the songs are kept, and what is
// happening to them.
func (a *app) cacheDirectoryDescription() string {
	dir, err := songCacheDirectory(a.settings.CacheDir)
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
