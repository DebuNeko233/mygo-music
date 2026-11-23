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
