/*
验证设置路径隔离、显式保存优先级及延迟保存保留最终值。
*/

package preferences

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Settings input cannot redirect later writes through a JSON field resembling its storage path.
func TestSettingsStoragePathIsNotJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	other := filepath.Join(root, "elsewhere.json")
	data, _ := json.Marshal(map[string]any{"Path": other, "quality": "lossless"})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, ok := Load(path)
	if !ok || s.Path != path || s.Quality != "lossless" {
		t.Fatal("file changed storage identity")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("settings escaped storage path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(data, &decoded) != nil {
		t.Fatal("invalid saved JSON")
	}
	if _, ok := decoded["Path"]; ok {
		t.Fatal("leaked internal path into persisted schema")
	}
}

// A transactional settings save must not be overwritten by an older queued slider change.
func TestSettingsSaveSupersedesPendingChanges(t *testing.T) {
	Flush()
	s := Default()
	s.Path = filepath.Join(t.TempDir(), "settings.json")
	s.CacheDir = "old"
	s.SaveSoon()
	s.CacheDir = "new"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	Flush()
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CacheDir != "new" {
		t.Fatalf("saved cache directory %q, want the committed new directory", saved.CacheDir)
	}
}

// Turning immersive lyrics off must survive a reload with the feature enabled by default.
func TestSaveKeepsImmersiveLyricsDisabled(t *testing.T) {
	s := Default()
	s.Path = filepath.Join(t.TempDir(), "settings.json")
	s.LyricEdgeFade = false
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := Default()
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
	s := Default()
	s.Path = filepath.Join(t.TempDir(), "settings.json")

	s.LyricSize = 28
	s.SaveSoon()
	s.LyricSize = 30
	s.SaveSoon()
	s.LyricSize = 32
	s.SaveSoon()
	Flush()

	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatalf("read the settings back: %v", err)
	}
	var kept Settings
	if err := json.Unmarshal(b, &kept); err != nil {
		t.Fatalf("decode the settings back: %v", err)
	}
	if kept.LyricSize != 32 {
		t.Fatalf("lyric size %v, for the last one set: 32", kept.LyricSize)
	}
}
