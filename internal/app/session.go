package app

import (
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/sessionstore"
)

func sessionPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// sessionEvery is how long after playback changed, and how often as it
// plays, the session is kept.
const sessionEvery = 30 * time.Second

// keepSession saves the session a while after playback changed, and every
// while again as it plays. Paused, nothing is due, and the app sleeps.
func (a *app) keepSession() {
	if a.sessionDue || a.quitting {
		return
	}
	a.sessionDue = true
	a.sessionCancel = sessionAfter(sessionEvery, func() {
		a.sessionDue = false
		a.sessionCancel = nil
		if a.quitting {
			return
		}
		a.saveSession()
		if a.pl.Playing() {
			a.keepSession()
		}
	})
}

// sessionAfter runs fn on the main thread after d, without redrawing a
// window as Update would.
var sessionAfter = func(d time.Duration, fn func()) func() {
	timer := time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
	return func() { timer.Stop() }
}

// restoreSession picks up playback where it was left, paused, once the
// listener's session is back: the songs they may play depend on it.
func (a *app) restoreSession() {
	if a.restored {
		return
	}
	a.restored = true
	path, err := sessionPath()
	if err != nil {
		return
	}
	s, ok := sessionstore.Read(path)
	if !ok {
		return
	}
	for i, track := range s.Queue {
		if song := a.local.Song(track.LocalID); track.IsLocal() && song.Track.IsLocal() {
			s.Queue[i] = song.Track
		}
	}
	a.pl.Restore(s)
	if s.Radio != "" && a.pl.Current() != nil {
		a.pl.AttachRadio(s.Radio, a.radioFeed(s.Radio))
	}
}

// saveSession captures playback on the UI thread and schedules its disk write.
func (a *app) saveSession() {
	s, ok := a.pl.Session()
	if !ok {
		return
	}
	if a.sessionSavePath == "" {
		path, err := sessionPath()
		if err != nil {
			return
		}
		a.sessionSavePath = path
	}
	a.sessionSave.Save(a.sessionSavePath, s, a.pl.QueueRevision())
}
