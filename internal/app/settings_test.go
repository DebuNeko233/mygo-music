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

// Existing shared opacity initializes both pages, while explicit split values survive the next save.
func TestSettingsMigratesBackdropOpacity(t *testing.T) {
	for _, tc := range []struct {
		name, json    string
		home, playing float64
	}{
		{"shared", `{"backdropOpacity":0.62}`, 0.62, 0.62},
		{"shared zero", `{"backdropOpacity":0}`, 0, 0},
		{"partial migration", `{"backdropOpacity":0.62,"backdropHomeOpacity":0}`, 0, 0.62},
		{"split values", `{"backdropOpacity":0.62,"backdropHomeOpacity":0.2,"backdropPlayingOpacity":0.8}`, 0.2, 0.8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserData(t)
			path, err := settingsPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.json), 0o600); err != nil {
				t.Fatal(err)
			}
			check := func(s settings) {
				t.Helper()
				if s.BackdropHomeOpacity != tc.home || s.BackdropPlayingOpacity != tc.playing {
					t.Fatalf("backdrop opacity = %v, %v, want %v, %v", s.BackdropHomeOpacity, s.BackdropPlayingOpacity, tc.home, tc.playing)
				}
			}
			s := loadSettings()
			check(s)
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			check(loadSettings())
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]json.RawMessage
			if err := json.Unmarshal(b, &stored); err != nil {
				t.Fatal(err)
			}
			if _, ok := stored["backdropOpacity"]; ok {
				t.Fatal("saved the shared opacity after migration")
			}
		})
	}
}
