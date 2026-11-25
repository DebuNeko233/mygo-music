package captions

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
	"netease-music-mygo/internal/viewmodel/internal/testutil"
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
	oldCheck := testutil.Wait(t, posts, "model check")
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
	testutil.Wait(t, posts, "download")()
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
	old := testutil.Wait(t, posts, "first A")
	s.Player.Cue([]netease.Track{{ID: 8, ProgramID: 8}})
	m.Sync(s)
	middle := testutil.Wait(t, posts, "B")
	s.Player.Cue([]netease.Track{{ID: 42, ProgramID: 42}})
	m.Sync(s)
	latest := testutil.Wait(t, posts, "last A")
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

// Caption language follows new text and out-of-order segments while duplicate segments leave the list alone.
func TestCaptionLanguageAfterAppendAndInsert(t *testing.T) {
	work := &CaptionWork{Program: 42}
	captions := Captions{Program: 42, Transcript: &captionstore.Transcript{}, Work: work}
	english := speech.Segment{Start: 5 * time.Second, End: 7 * time.Second, Text: "First caption"}
	japanese := speech.Segment{Start: 8 * time.Second, End: 10 * time.Second, Text: "次のことば"}
	captions.Said(work, english)
	if captions.Language != "" || len(captions.Lines) != 1 {
		t.Fatal("first caption did not keep the default language")
	}
	captions.Said(work, japanese)
	captions.Said(work, japanese)
	if got := captions; got.Language != "ja" || len(got.Lines) != 2 {
		t.Fatalf("append or duplicate corrupted transcript: %q, %v", got.Language, got.Lines)
	}
	captions.Said(work, speech.Segment{Start: time.Second, End: 3 * time.Second, Text: "Earlier"})
	if got := captions.Lines; len(got) != 3 || got[0].Text != "Earlier" || captions.Language != "ja" {
		t.Fatalf("out-of-order insert corrupted transcript: %v", got)
	}
}
