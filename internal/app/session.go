package app

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// session.json is an atomic full snapshot, also readable by older builds.
// A small sidecar advances it only if both files refer to the same queue.
type sessionFile struct {
	player.Session
	Token string `json:"token,omitempty"`
}

type sessionCheckpoint struct {
	Token    string            `json:"token"`
	At       int               `json:"at"`
	Shuffle  bool              `json:"shuffle"`
	Repeat   player.RepeatMode `json:"repeat"`
	Position int64             `json:"position"`
	Radio    string            `json:"radio,omitempty"`
}

type sessionQueue struct {
	tracks []netease.Track
	order  []int
}

type sessionWrite struct {
	path       string
	queue      *sessionQueue
	checkpoint sessionCheckpoint
}

// Only the UI thread changes snapshot/revision. A single writer owns disk
// state; at most one newer checkpoint is retained while it writes.
type sessionSaver struct {
	snapshot *sessionQueue
	revision uint64
	path     string
	mu       sync.Mutex
	pending  *sessionWrite
	done     chan struct{}
	written  sessionWrite
}

func sessionPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// saveSession captures queue/order once per revision. Track metadata in the
// queue is read-only; a UI event never serializes the queue or writes it.
func (a *app) saveSession() {
	s, ok := a.pl.Session()
	if !ok {
		return
	}
	w := &a.sessionSave
	if w.path == "" {
		path, err := sessionPath()
		if err != nil {
			return
		}
		w.path = path
	}
	if rev := a.pl.QueueRevision(); w.snapshot == nil || w.revision != rev {
		w.revision = rev
		w.snapshot = &sessionQueue{tracks: slices.Clone(s.Queue), order: slices.Clone(s.Order)}
	}
	w.submit(sessionWrite{path: w.path, queue: w.snapshot, checkpoint: sessionCheckpoint{
		At: s.At, Shuffle: s.Shuffle, Repeat: s.Repeat, Position: s.Position, Radio: s.Radio,
	}})
}

func (w *sessionSaver) submit(job sessionWrite) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = &job
	if w.done == nil {
		w.done = make(chan struct{})
		go w.run()
	}
}

func (w *sessionSaver) run() {
	for {
		w.mu.Lock()
		job := w.pending
		w.pending = nil
		if job == nil {
			close(w.done)
			w.done = nil
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		w.write(*job)
	}
}

// Wait only on shutdown (or in tests). The worker does not call the UI.
func (w *sessionSaver) wait() {
	w.mu.Lock()
	done := w.done
	w.mu.Unlock()
	if done != nil {
		<-done
	}
}

func checkpointPath(path string) string {
	return filepath.Join(filepath.Dir(path), "session-progress.json")
}

func (w *sessionSaver) write(job sessionWrite) {
	cp := &job.checkpoint
	if w.written.path != job.path || w.written.queue != job.queue {
		cp.Token = rand.Text()
		s := player.Session{Queue: job.queue.tracks, Order: job.queue.order,
			At: cp.At, Shuffle: cp.Shuffle, Repeat: cp.Repeat, Position: cp.Position, Radio: cp.Radio}
		if !writeSessionJSON(job.path, sessionFile{Session: s, Token: cp.Token}) {
			return
		}
	} else {
		cp.Token = w.written.checkpoint.Token
		if *cp == w.written.checkpoint || !writeSessionJSON(checkpointPath(job.path), cp) {
			return
		}
	}
	w.written = job
}

func writeSessionJSON(path string, value any) bool {
	b, err := json.Marshal(value)
	if err != nil || os.WriteFile(path+".part", b, 0o600) != nil {
		return false
	}
	if os.Rename(path+".part", path) != nil {
		_ = os.Remove(path + ".part")
		return false
	}
	return true
}

// sessionEvery is how long after playback changed, and how often as it
// plays, the session is kept.
const sessionEvery = 30 * time.Second

// keepSession saves the session a while after playback changed, and every
// while again as it plays. Paused, nothing is due, and the app sleeps.
func (a *app) keepSession() {
	if a.sessionDue {
		return
	}
	a.sessionDue = true
	sessionAfter(sessionEvery, func() {
		a.sessionDue = false
		a.saveSession()
		if a.pl.Playing() {
			a.keepSession()
		}
	})
}

// sessionAfter runs fn on the main thread after d, without redrawing a
// window as Update would.
var sessionAfter = func(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
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
	s, ok := readSession(path)
	if !ok {
		return
	}
	a.pl.Restore(s)
	if s.Radio != "" && a.pl.Current() != nil {
		a.pl.AttachRadio(s.Radio, a.radioFeed(s.Radio))
	}
}

func readSession(path string) (player.Session, bool) {
	var file sessionFile
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &file) != nil {
		return player.Session{}, false
	}
	if file.Token != "" {
		var cp sessionCheckpoint
		if b, err := os.ReadFile(checkpointPath(path)); err == nil && json.Unmarshal(b, &cp) == nil &&
			cp.Token == file.Token && cp.At >= 0 && cp.At < len(file.Queue) && cp.Position >= 0 {
			file.At, file.Shuffle, file.Repeat = cp.At, cp.Shuffle, cp.Repeat
			file.Position, file.Radio = cp.Position, cp.Radio
		}
	}
	return file.Session, true
}
