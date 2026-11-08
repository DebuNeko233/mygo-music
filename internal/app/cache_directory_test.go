package app

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// cacheTestApp is an app whose songs are kept in a directory of the test's,
// with another one beside it to move them to.
func cacheTestApp(t *testing.T) (*app, string, string) {
	t.Helper()
	isolateUserData(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsPage = "cache"
	a.settings.CacheDir = source
	a.settings.path = filepath.Join(root, "settings.json")
	a.pl.Cache = newSongCache(a.settings.CacheMB, source)
	return a, source, target
}

func readCacheSettings(t *testing.T, path string) settings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// cachePosts takes what the change posts for the main thread, for the test
// to run when it wants.
func cachePosts(t *testing.T) chan func() {
	t.Helper()
	old := postCacheDirectory
	posts := make(chan func(), 16)
	postCacheDirectory = func(_ *app, fn func()) { posts <- fn }
	t.Cleanup(func() { postCacheDirectory = old })
	return posts
}

func finishCacheChange(t *testing.T, a *app, posts chan func()) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for a.cacheMoving {
		select {
		case fn := <-posts:
			fn()
		case <-timeout.C:
			t.Fatal("directory change did not finish")
		}
	}
}

// writeCachedSong keeps a song of five bytes in the directory, as the cache
// would have.
func writeCachedSong(t *testing.T, source string) {
	t.Helper()
	meta, _ := json.Marshal(netease.Stream{Size: 5, Type: "mp3", Level: "exhigh"})
	for name, data := range map[string][]byte{"123-exhigh.mp3": []byte("music"), "123-exhigh.json": meta} {
		if err := os.WriteFile(filepath.Join(source, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Choosing a directory asks first, then starts one change in the background.
func TestCacheDirectoryControls(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	oldOpen := openCacheDirectory
	defer func() { openCacheDirectory = oldOpen }()
	opens := 0
	openCacheDirectory = func(mygo.OpenDialogOptions) ([]string, error) { opens++; return []string{target}, nil }
	tt := ui.NewTester(a.settingsView, 800, 1800)
	if err := tt.Click("更改歌曲缓存目录"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheConfirm.open || a.cacheMoving || a.settings.CacheDir != source {
		t.Fatal("started changing before confirmation")
	}
	if err := tt.Click("迁移并更改"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheMoving || a.cacheConfirm.open || a.settings.CacheDir != source {
		t.Fatal("did not start safely after confirmation")
	}
	a.chooseCacheDirectory()
	if opens != 1 {
		t.Fatal("allowed a concurrent change")
	}
	finishCacheChange(t, a, posts)
	saved := readCacheSettings(t, a.settings.path)
	if a.cacheError != "" || saved.CacheDir != target {
		t.Fatalf("change failed: %s %+v", a.cacheError, saved)
	}
}

// Choosing the directory the songs are in already changes nothing, and saves
// nothing.
func TestCacheDirectorySameDirectory(t *testing.T) {
	a, source, _ := cacheTestApp(t)
	writeCachedSong(t, source)
	cache := a.pl.Cache
	oldOpen := openCacheDirectory
	defer func() { openCacheDirectory = oldOpen }()
	targets := []string{source, source + string(filepath.Separator) + "."}
	if runtime.GOOS == "windows" {
		targets = append(targets, strings.ToUpper(source))
	}
	for _, target := range targets {
		openCacheDirectory = func(mygo.OpenDialogOptions) ([]string, error) { return []string{target}, nil }
		tt := ui.NewTester(a.settingsView, 800, 1800)
		if err := tt.Click("更改歌曲缓存目录"); err != nil {
			t.Fatal(err)
		}
		if a.cacheConfirm.open || a.cacheMoving || a.cacheError != "" || a.settings.CacheDir != source || a.pl.Cache != cache {
			t.Fatalf("selecting current directory changed state: %s", target)
		}
		if err := a.changeCacheDirectory(target, true); err != nil || a.cacheMoving || a.pl.Cache != cache {
			t.Fatalf("same-directory change was not a no-op: %v", err)
		}
	}
	if cache.Size() != 5 {
		t.Fatal("existing cache was modified")
	}
	if _, err := os.Stat(a.settings.path); !os.IsNotExist(err) {
		t.Fatalf("no-op saved settings: %v", err)
	}
}

// Closing the picker, or the dialog with its button or Escape, leaves the
// songs where they are.
func TestCacheDirectoryDialogCancel(t *testing.T) {
	for _, cancel := range []string{"picker", "button", "escape"} {
		t.Run(cancel, func(t *testing.T) {
			a, source, target := cacheTestApp(t)
			cache := a.pl.Cache
			oldOpen := openCacheDirectory
			defer func() { openCacheDirectory = oldOpen }()
			openCacheDirectory = func(mygo.OpenDialogOptions) ([]string, error) {
				if cancel == "picker" {
					return nil, nil
				}
				return []string{target}, nil
			}
			tt := ui.NewTester(a.settingsView, 800, 1800)
			if err := tt.Click("更改歌曲缓存目录"); err != nil {
				t.Fatal(err)
			}
			if cancel == "button" {
				if err := tt.Click("取消"); err != nil {
					t.Fatal(err)
				}
			} else if cancel == "escape" {
				tt.Key(0, ui.KeyEscape)
			}
			if a.cacheConfirm.open || a.settings.CacheDir != source || a.cacheMoving || a.pl.Cache != cache {
				t.Fatal("cancel changed active cache")
			}
		})
	}
}

// The songs move while the app runs, and what the listener changes meanwhile
// is kept.
func TestCacheDirectoryImmediate(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	writeCachedSong(t, source)
	if err := a.settings.save(); err != nil {
		t.Fatal(err)
	}
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	// The settings are still the listener's as the songs copy: a change made
	// then must not be overwritten with the ones from before it.
	a.settings.Quality = "lossless"
	finishCacheChange(t, a, posts)
	if a.cacheError != "" || a.settings.CacheDir != target {
		t.Fatalf("migration failed: %s", a.cacheError)
	}
	saved := readCacheSettings(t, a.settings.path)
	if saved.CacheDir != target || saved.Quality != "lossless" {
		t.Fatalf("settings lost: %+v", saved)
	}
	if a.pl.Cache == nil || a.pl.Cache.Size() != 5 {
		t.Fatal("player did not use the migrated cache")
	}
	if _, err := os.Stat(filepath.Join(source, "123-exhigh.mp3")); !os.IsNotExist(err) {
		t.Fatalf("old cache remains: %v", err)
	}
}

// Changing without migration keeps the songs after it in the new directory,
// and leaves the old ones where they are.
func TestCacheDirectoryWithoutMigration(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	writeCachedSong(t, source)
	a.confirmCacheDirectory(target)
	tt := ui.NewTester(a.settingsView, 780, 640)
	if err := tt.Click("仅更改目录"); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError != "" || a.pl.Cache.Size() != 0 || a.settings.CacheDir != target {
		t.Fatal("failed to switch cache")
	}
	if _, err := os.Stat(filepath.Join(source, "123-exhigh.mp3")); err != nil {
		t.Fatal(err)
	}
}

// Settings that cannot be saved leave the songs where they were, in the
// cache that was playing.
func TestCacheDirectorySaveFailure(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	writeCachedSong(t, source)
	cache := a.pl.Cache
	a.settings.path = source // a directory: no settings may be written there
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError == "" || a.settings.CacheDir != source || a.pl.Cache != cache || cache.Size() != 5 {
		t.Fatal("failed to restore old cache")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed target remains: %v", err)
	}
}

// A directory that holds files is refused before anything stops playing.
func TestCacheDirectoryRejectsOccupied(t *testing.T) {
	a, source, target := cacheTestApp(t)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "personal.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.changeCacheDirectory(target, true); err == nil {
		t.Fatal("accepted occupied directory")
	}
	if a.cacheMoving || a.settings.CacheDir != source {
		t.Fatal("invalid target changed live state")
	}
}

// A directory that turns out to hold files keeps the dialog open with what
// went wrong, and touches nothing.
func TestCacheDirectoryConfirmationFailure(t *testing.T) {
	a, source, target := cacheTestApp(t)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(target, "personal.txt")
	if err := os.WriteFile(personal, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.confirmCacheDirectory(target)
	tt := ui.NewTester(a.settingsView, 640, 420)
	if err := tt.Click("迁移并更改"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheConfirm.open || a.cacheConfirm.err == "" || a.cacheMoving || a.settings.CacheDir != source {
		t.Fatal("validation failure dismissed confirmation or changed cache")
	}
	if data, err := os.ReadFile(personal); err != nil || string(data) != "keep" {
		t.Fatal("occupied target was modified")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.cacheConfirm.open {
		t.Fatal("cannot dismiss failed confirmation")
	}
}

// A count of the old cache that comes in late does not report the new one's
// usage.
func TestCacheDirectoryStaleMeasurement(t *testing.T) {
	a, _, target := cacheTestApp(t)
	old := a.pl.Cache
	a.cacheUsed = 123
	a.cacheMoving = true
	a.cacheMeasured(old, 0)
	if a.cacheUsed != 123 {
		t.Fatal("measurement changed usage during migration")
	}
	a.cacheMoving = false
	a.pl.Cache = newSongCache(a.settings.CacheMB, target)
	a.cacheMeasured(old, 456)
	if a.cacheUsed != 123 {
		t.Fatal("stale cache measurement overwrote usage")
	}
	a.cacheMeasured(a.pl.Cache, 789)
	if a.cacheUsed != 789 {
		t.Fatal("current cache measurement was ignored")
	}
}

// With NM_FRAMES set, saves the settings as the songs move, to look at.
func TestCacheDirectoryFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, _, target := cacheTestApp(t)
	a.cacheMoving, a.cacheTarget = true, target
	tt := ui.NewTester(a.settingsView, 780, 640)
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "cache-directory.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
