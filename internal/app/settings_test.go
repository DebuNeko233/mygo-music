package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Turning immersive lyrics off must survive a reload with the feature enabled by default.
func TestSaveKeepsImmersiveLyricsDisabled(t *testing.T) {
	s := defaultSettings()
	s.path = filepath.Join(t.TempDir(), "settings.json")
	s.LyricEdgeFade = false
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.path)
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

// TestSaveSoonKeepsTheLastOfABurst checks that settings kept off the drawing
// goroutine still reach the disk, and as they stand when the last one is taken:
// holding a shortcut that steps the lyric size takes them on every repeat.
func TestSaveSoonKeepsTheLastOfABurst(t *testing.T) {
	a := newTestApp()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")

	a.settings.LyricSize = 28
	a.settings.saveSoon()
	a.settings.LyricSize = 30
	a.settings.saveSoon()
	a.settings.LyricSize = 32
	a.settings.saveSoon()
	settingsWrites.wait()

	b, err := os.ReadFile(a.settings.path)
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
