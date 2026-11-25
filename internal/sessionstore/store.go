package sessionstore

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/userdata"
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

// Saver captures snapshots on its owning thread. A single writer owns disk
// state; at most one newer checkpoint is retained while it writes.
type Saver struct {
	snapshot *sessionQueue
	revision uint64
	mu       sync.Mutex
	pending  *sessionWrite
	done     chan struct{}
	written  sessionWrite
}

func (w *Saver) submit(job sessionWrite) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = &job
	if w.done == nil {
		w.done = make(chan struct{})
		go w.run()
	}
}

func (w *Saver) run() {
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

// Wait finishes pending writes; call it after the last Save on shutdown.
// The worker never calls back into the owner.
func (w *Saver) Wait() {
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

func (w *Saver) write(job sessionWrite) {
	cp := &job.checkpoint
	if w.written.path != job.path || w.written.queue != job.queue {
		cp.Token = rand.Text()
		s := player.Session{Queue: job.queue.tracks, Order: job.queue.order,
			At: cp.At, Shuffle: cp.Shuffle, Repeat: cp.Repeat, Position: cp.Position, Radio: cp.Radio}
		if !userdata.WriteJSON(job.path, sessionFile{Session: s, Token: cp.Token}) {
			return
		}
	} else {
		cp.Token = w.written.checkpoint.Token
		if *cp == w.written.checkpoint || !userdata.WriteJSON(checkpointPath(job.path), cp) {
			return
		}
	}
	w.written = job
}

// Read restores a snapshot and applies only matching, valid progress.
func Read(path string) (player.Session, bool) {
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

// Save captures queue/order once per revision on the caller's thread. Calls
// must be serialized by that owner; disk writes run on a single worker.
// Increment revision whenever the queue or play order changes.
func (w *Saver) Save(path string, s player.Session, revision uint64) {
	if w.snapshot == nil || w.revision != revision {
		w.revision = revision
		w.snapshot = &sessionQueue{tracks: slices.Clone(s.Queue), order: slices.Clone(s.Order)}
	}
	w.submit(sessionWrite{path: path, queue: w.snapshot, checkpoint: sessionCheckpoint{
		At: s.At, Shuffle: s.Shuffle, Repeat: s.Repeat, Position: s.Position, Radio: s.Radio,
	}})
}
