package app

import (
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/cachemove"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
)

// openCacheDirectory asks for a folder, as the system's own picker does.
var openCacheDirectory = mygo.Dialog.Open
var quitAfterCacheMove = mygo.App.Quit

// changeCacheDirectory suspends all stores before moving them off the UI
// thread. Settings commit only after a successful copy; failure resumes the
// original stores. Playback resumes with any controls changed during the move.
func (a *app) changeCacheDirectory(directory string, migrate bool) error {
	if a.cacheSettings.Moving {
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
	if same {
		// Selecting the system root restores the default preference
		// without moving files already stored there.
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
	shared := isDefault
	if !shared {
		if err := cachemove.Validate(source.Root, target); err != nil {
			return err
		}
	}
	journal, err := cacheMoveJournal()
	if err != nil {
		return err
	}
	return a.cacheSettings.Change(viewmodel.CacheChange{Source: source, Target: target, Directory: directory, Journal: journal, Migrate: migrate, Shared: shared, Default: isDefault}, a.cacheServices())
}

// chooseCacheDirectory asks for the shared cache root.
func (a *app) chooseCacheDirectory() {
	if a.cacheSettings.Moving {
		return
	}
	paths, err := openCacheDirectory(mygo.OpenDialogOptions{
		Parent: a.settingsWin, Title: "选择缓存目录", Directory: true, CreateDirectories: true,
	})
	if err != nil {
		a.cacheSettings.Error = err.Error()
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
	if a.cacheSettings.Moving {
		return
	}
	target, err := cacheDirectory(directory)
	if err != nil {
		a.cacheSettings.Error = err.Error()
		return
	}
	source, err := cacheDirectory(a.settings.CacheDir)
	if err != nil {
		a.cacheSettings.Error = err.Error()
		return
	}
	same, err := cachemove.SameDirectory(source, target)
	if err != nil {
		a.cacheSettings.Error = err.Error()
		return
	}
	if same {
		if err := a.changeCacheDirectory(directory, false); err != nil {
			a.cacheSettings.Error = err.Error()
		}
		return
	}
	a.cacheSettings.Confirm = viewmodel.CacheConfirmation{Open: true, Directory: directory, Target: target}
}

// submitCacheDirectory moves all caches when requested; otherwise old files stay.
func (a *app) submitCacheDirectory(migrate bool) {
	d := &a.cacheSettings.Confirm
	if err := a.changeCacheDirectory(d.Directory, migrate); err != nil {
		d.Error = err.Error()
		return
	}
	d.Open = false
}

// cacheDirectoryDescription shows the root and any directory change failure.
func (a *app) cacheDirectoryDescription() string {
	dir, err := cacheDirectory(a.settings.CacheDir)
	if err != nil {
		return err.Error()
	}
	desc := dir
	if a.cacheSettings.Moving {
		desc += "\n" + a.cacheMoveLabel()
	}
	if a.cacheSettings.Error != "" {
		desc += "\n" + a.cacheSettings.Error
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
		a.cacheSettings.Error = err.Error()
		return
	}
	active, err := cachePaths(a.settings)
	if err != nil {
		a.cacheSettings.Error = err.Error()
		return
	}
	a.cacheSettings.Moving = true
	go func() {
		err := cachemove.Recover(journal, active)
		a.post(func() {
			a.cacheSettings.Moving = false
			if a.cacheSettings.Quit {
				quitAfterCacheMove()
				return
			}
			if err != nil {
				a.cacheSettings.Error = "清理上次缓存迁移失败：" + err.Error()
				a.redraw()
				return
			}
		})
	}()
}

func (a *app) cancelCacheChange() { a.cacheSettings.Cancel(); a.redraw() }
func (a *app) deferCacheQuit() bool {
	deferred := a.cacheSettings.DeferQuit()
	if deferred {
		a.redraw()
	}
	return deferred
}
func (a *app) cacheMoveLabel() string { return a.cacheSettings.Label() }
func (a *app) cacheServices() viewmodel.CacheServices {
	return viewmodel.CacheServices{Post: a.post, Player: a.pl, Browse: a.browseCache, Images: a.imageCache,
		Commit: func(directory string) error {
			next := a.settings
			next.CacheDir = directory
			if err := next.save(); err != nil {
				return err
			}
			a.settings = next
			return nil
		},
		NewCache: func() *player.Cache { return newSongCache(a.settings) }, Measure: func() { a.measureCache(); a.measureOfflineCache() }, Changed: func() {
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
			a.redraw()
		}, Quit: quitAfterCacheMove}
}
