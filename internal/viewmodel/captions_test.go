package viewmodel

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/captionstore"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/speech"
)

func captionFixture(t *testing.T) (*Captions, CaptionServices, chan func()) {
	t.Helper()
	posts := make(chan func(), 16)
	root := t.TempDir()
	s := CaptionServices{Player: player.New(func(func()) {}), Engine: new(speech.Engine), Post: func(f func()) { posts <- f }, Enabled: func() bool { return false }, Available: func() bool { return true }, Enable: func() {}, Changed: func() {}, ModelDir: func() (string, error) { return root, nil }, TranscriptPath: func(id int64) (string, error) { return filepath.Join(root, fmt.Sprint(id)+".json"), nil }}
	return new(Captions), s, posts
}

// Downloading supersedes an in-flight model check, and rendering cannot restart that check during the download.
func TestCaptionModelDownloadSupersedesCheck(t *testing.T) {
	m, s, posts := captionFixture(t)
	var checks atomic.Int32
	s.Installed = func(string) bool { checks.Add(1); return false }
	m.ModelReady(s)
	oldCheck := wait(t, posts, "model check")
	release := make(chan struct{})
	s.Download = func(context.Context, string, func(int64, int64)) error { <-release; return nil }
	enabled := false
	s.Enable = func() { enabled = true }
	m.StartModelDownload(s)
	m.ModelReady(s)
	oldCheck()
	if !m.Model.Downloading || m.Model.Checking || m.Model.Checked || checks.Load() != 1 {
		t.Fatal("model check superseded download")
	}
	close(release)
	wait(t, posts, "download")()
	if m.Model.Downloading || !m.Model.Installed || !m.Model.Checked || !enabled {
		t.Fatal("download completion was discarded")
	}
}

// Failure to resolve the model directory leaves the installed state available for a retry.
func TestCaptionModelDeletePathFailure(t *testing.T) {
	m, s, _ := captionFixture(t)
	m.Model = CaptionModel{Checked: true, Installed: true}
	s.ModelDir = func() (string, error) { return "", errors.New("path unavailable") }
	m.DeleteModel(s)
	if !m.Model.Installed || m.Model.Deleting || m.Model.Error == "" {
		t.Fatal("path failure lost installed model state")
	}
}

// An obsolete same-episode completion cannot mark the replacement transcription as failed or complete.
func TestCaptionObsoleteCompletion(t *testing.T) {
	m, s, _ := captionFixture(t)
	old, current := &CaptionWork{Program: 42}, &CaptionWork{Program: 42, From: 10 * time.Second}
	m.Program, m.Transcript, m.Work = 42, &captionstore.Transcript{}, current
	for _, err := range []error{nil, errors.New("late failure")} {
		m.Done(old, err, false, s)
	}
	if m.Work != current || m.Failed != 0 || len(m.Transcript.Spans) != 0 {
		t.Fatal("old completion changed replacement")
	}
}

// Returning to the same episode waits for the latest transcript load instead of accepting its previous visit's result.
func TestCaptionTranscriptLoadIdentity(t *testing.T) {
	m, s, posts := captionFixture(t)
	s.Player.Cue([]netease.Track{{ID: 42, ProgramID: 42}})
	m.Sync(s)
	old := wait(t, posts, "first A")
	s.Player.Cue([]netease.Track{{ID: 8, ProgramID: 8}})
	m.Sync(s)
	middle := wait(t, posts, "B")
	s.Player.Cue([]netease.Track{{ID: 42, ProgramID: 42}})
	m.Sync(s)
	latest := wait(t, posts, "last A")
	old()
	middle()
	if m.Transcript != nil {
		t.Fatal("earlier visit published into current episode")
	}
	latest()
	if m.Transcript == nil {
		t.Fatal("latest transcript did not load")
	}
}
