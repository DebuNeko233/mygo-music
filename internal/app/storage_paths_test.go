/*
验证正式版和开发版使用独立数据目录，保留旧文件，并正常恢复新目录中的设置。
*/

package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// Fresh processes exercise the packaged name without inheriting other tests' path overrides.
func TestStorageUsesIndependentDirectories(t *testing.T) {
	if name := os.Getenv("NYAGO_STORAGE_TEST_NAME"); name != "" {
		checkIndependentStorage(t, name)
		return
	}
	b, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Name string }
	if err := json.Unmarshal(b, &config); err != nil || config.Name == "" {
		t.Fatal("invalid package configuration", err)
	}
	for _, suffix := range []string{"", " Dev"} {
		t.Run(config.Name+suffix, func(t *testing.T) {
			home := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestStorageUsesIndependentDirectories$")
			cmd.Env = append(os.Environ(),
				"NYAGO_STORAGE_TEST_NAME="+config.Name+suffix,
				"HOME="+home, "USERPROFILE="+home,
				"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
				"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
				"AppData="+filepath.Join(home, "AppData", "Roaming"),
				"LocalAppData="+filepath.Join(home, "AppData", "Local"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated storage check: %v\n%s", err, out)
			}
		})
	}
}

func checkIndependentStorage(t *testing.T, name string) {
	t.Helper()
	mygo.App.SetName(name)
	dataBase, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	legacyName := "MyGo 云音乐"
	if strings.HasSuffix(name, " Dev") {
		legacyName += " Dev"
	}
	legacyData := filepath.Join(dataBase, legacyName)
	legacyCache := filepath.Join(cacheBase, legacyName)
	oldSettings := []byte(`{"appearance":"dark","volume":0.13}`)
	for path, data := range map[string][]byte{
		filepath.Join(legacyData, "settings.json"):      oldSettings,
		filepath.Join(legacyCache, "songs", "old-song"): []byte("old audio"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := loadSettings()
	if s.Appearance != defaultSettings().Appearance || s.Volume != defaultSettings().Volume {
		t.Fatal("new application read legacy settings")
	}
	if s.Path != filepath.Join(dataBase, name, "settings.json") || filepath.Dir(s.Path) == legacyData {
		t.Fatal("settings did not use the new application directory", s.Path)
	}
	paths, err := cachePaths(s)
	if err != nil || paths.Root != filepath.Join(cacheBase, name) || paths.Root == legacyCache {
		t.Fatal("cache did not use the new application directory", paths, err)
	}
	if _, err := os.Stat(filepath.Join(paths.Songs, "old-song")); !os.IsNotExist(err) {
		t.Fatal("legacy cache was copied into the new root", err)
	}
	for _, pathFor := range []func() (string, error){sessionPath, modelDir} {
		path, err := pathFor()
		if err != nil || filepath.Dir(path) != filepath.Dir(s.Path) {
			t.Fatal("persistent data escaped the application root", path, err)
		}
	}
	transcript, err := transcriptPath(7)
	if err != nil || transcript != filepath.Join(paths.Root, "transcripts", "7.json") {
		t.Fatal("transcript escaped the new cache", transcript, err)
	}
	s.Volume = 0.42
	s.CacheDir = filepath.Join(t.TempDir(), "custom-cache")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	reloaded := loadSettings()
	customPaths, err := cachePaths(reloaded)
	if err != nil || reloaded.Volume != s.Volume || customPaths.Root != s.CacheDir {
		t.Fatal("new settings or custom cache directory were not restored", err)
	}
	if b, err := os.ReadFile(filepath.Join(legacyData, "settings.json")); err != nil || string(b) != string(oldSettings) {
		t.Fatal("legacy settings were changed", err)
	}
	if b, err := os.ReadFile(filepath.Join(legacyCache, "songs", "old-song")); err != nil || string(b) != "old audio" {
		t.Fatal("legacy cache was changed", err)
	}
}
