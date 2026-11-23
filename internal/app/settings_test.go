package app

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netease-music-mygo/internal/preferences"
)

// A transactional settings save must not be overwritten by an older queued slider change.
func TestSettingsSaveSupersedesPendingChanges(t *testing.T) {
	preferences.Flush()
	s := defaultSettings()
	s.Path = filepath.Join(t.TempDir(), "settings.json")
	s.CacheDir = "old"
	s.saveSoon()
	s.CacheDir = "new"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	preferences.Flush()
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CacheDir != "new" {
		t.Fatalf("saved cache directory %q, want the committed new directory", saved.CacheDir)
	}
}

// Turning immersive lyrics off must survive a reload with the feature enabled by default.
func TestSaveKeepsImmersiveLyricsDisabled(t *testing.T) {
	s := defaultSettings()
	s.Path = filepath.Join(t.TempDir(), "settings.json")
	s.LyricEdgeFade = false
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := defaultSettings()
	if err := json.Unmarshal(b, &reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.LyricEdgeFade {
		t.Fatal("immersive lyrics were re-enabled after saving them as disabled")
	}
}

// A failed final settings write leaves an error in the application log.
func TestFinalSettingsSaveReportsFailure(t *testing.T) {
	var output bytes.Buffer
	writer := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(writer) })
	a := newTestApp()
	a.settings.Path = t.TempDir()
	a.saveFinalSettings()
	if !strings.Contains(output.String(), "settings: final save:") {
		t.Fatalf("final save failure was not reported: %q", output.String())
	}
}

// TestSaveSoonKeepsTheLastOfABurst checks that settings kept off the drawing
// goroutine still reach the disk, and as they stand when the last one is taken:
// holding a shortcut that steps the lyric size takes them on every repeat.
func TestSaveSoonKeepsTheLastOfABurst(t *testing.T) {
	a := newTestApp()
	a.settings.Path = filepath.Join(t.TempDir(), "settings.json")

	a.settings.LyricSize = 28
	a.settings.saveSoon()
	a.settings.LyricSize = 30
	a.settings.saveSoon()
	a.settings.LyricSize = 32
	a.settings.saveSoon()
	preferences.Flush()

	b, err := os.ReadFile(a.settings.Path)
	if err != nil {
		t.Fatalf("read the settings back: %v", err)
	}
	var kept settings
	if err := json.Unmarshal(b, &kept); err != nil {
		t.Fatalf("decode the settings back: %v", err)
	}
	if kept.LyricSize != 32 {
		t.Fatalf("lyric size %v, for the last one set: 32", kept.LyricSize)
	}
}
