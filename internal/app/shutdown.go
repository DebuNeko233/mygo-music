package app

import (
	"log"

	"netease-music-mygo/internal/desktop/lifetime"
	"netease-music-mygo/internal/preferences"
)

// saveFinalSettings reports a write failure before the process exits, when a
// later view can no longer carry a toast.
func (a *app) saveFinalSettings() {
	if err := a.settings.save(); err != nil {
		log.Printf("settings: final save: %v", err)
	}
}

// cancelWork retires producers before the final snapshots. UI dispatch stays
// open until cache moves and download tag writes have allowed quit to proceed.
func (a *app) cancelWork() {
	if a.sessionCancel != nil {
		a.sessionCancel()
		a.sessionCancel = nil
	}
	a.sessionDue = false
	a.local.CancelOpen()
	a.cacheSettings.Cancel()
	a.account.CloseLogin()
	a.cancelMusicLink()
	a.cloud.Stop()
	a.downloads.CancelAll()
	a.captions.CancelModelDownload()
	a.captions.Pause()
	a.radio.CancelPending()
	if a.recovery.timer != nil {
		a.recovery.timer.Stop()
		a.recovery.timer = nil
	}
	if a.network.stop != nil {
		a.network.stop()
		a.network.stop = nil
	}
}

// stopNative runs before the event loop ends. It never waits for a worker
// which may still be trying to publish a result to that loop.
func (a *app) stopNative() {
	if a.native.Closed {
		return
	}
	a.quitting = true
	a.cancelWork()
	if a.pl != nil {
		a.flushPlayReports()
		a.pl.Close()
	}
	a.unregisterGlobals()
	if a.notch != nil {
		a.notch.Close()
	}
	if a.lyrics != nil {
		a.lyrics.Close()
	}
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
	}
	if a.mix != nil {
		a.mix.Close()
		a.mix = nil
	}
	a.native.Close()
}

// release also handles an error before the main window was created.
func (a *app) release() {
	if a.released {
		return
	}
	a.released = true
	a.stopNative()
	a.art.Close()
	a.closePosts()
	if a.pl != nil {
		a.pl.Close()
	}
	a.captionEngine.Close()
	a.browseCache.Close()
	a.imageCache.Close()
	a.identityCache.Close()
}

func (a *app) requestQuit(sessionEnding bool) bool {
	if a.quitting {
		return true
	}
	// Logout/shutdown cannot be prevented by the OS. Still cancel producers
	// and save the latest committed state instead of skipping final snapshots.
	if !sessionEnding {
		cache, downloads := a.deferCacheQuit(), a.downloads.DeferQuit()
		if cache || downloads {
			return false
		}
	}
	a.quitting = true
	a.cancelWork()
	if !a.initialized {
		return true
	}
	a.settings.Volume = a.playbackState.Volume
	if a.lyrics != nil {
		// Where the lyrics were left, before the window goes.
		a.lyrics.Remember()
	}
	// A write still owed from a key repeat would otherwise land
	// after this one, with the settings as they were before it.
	preferences.Flush()
	a.saveFinalSettings()
	if a.pl != nil {
		a.saveSession()
	}
	a.sessionSave.Wait()
	if write := a.transcriptWrite(); write != nil {
		write()
	}
	return true
}

type appLifetime struct {
	native        lifetime.Subscriptions
	released      bool
	initialized   bool
	sessionCancel func()
}
