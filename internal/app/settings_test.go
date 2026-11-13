package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
