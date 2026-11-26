/*
验证缓存目录设置、选择确认、迁移失败和统计结果失效等交互。
*/

package app

import (
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/preferences"
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
	a.preferences.Section = "cache"
	a.settings.CacheDir = source
	a.settings.Path = filepath.Join(root, "settings.json")
	a.pl.Cache = newSongCache(a.settings)
	return a, source, target
}

func readCacheSettings(t *testing.T, path string) settings {
	t.Helper()
	// The window keeps the settings on a goroutine of its own, so what is
	// owed to the disk is waited for before it is read back.
	preferences.Flush()
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

func finishCacheChange(t *testing.T, a *app, posts chan func()) {
	t.Helper()
	// Complete the follow-up measurements as well, before restoring the
	// test dispatcher or starting another conversion with the same stores.
	a.cacheSettings.Used, a.browseUsed, a.imagesUsed = -1, -1, -1
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for a.cacheSettings.Moving || a.cacheSettings.Used < 0 || (a.browseCache != nil || a.imageCache != nil) && a.browseUsed < 0 {
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
	writeCachedSongIn(t, filepath.Join(source, "songs"))
}

func writeCachedSongIn(t *testing.T, source string) {
	t.Helper()
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}

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
	posts := testPosts(a)
	oldOpen := openCacheDirectory
	defer func() { openCacheDirectory = oldOpen }()
	opens := 0
	openCacheDirectory = func(mygo.OpenDialogOptions) ([]string, error) { opens++; return []string{target}, nil }
	tt := ui.NewTester(a.views.settings.Build, 800, 1800)
	if err := tt.Click("更改缓存目录"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheSettings.Confirm.Open || a.cacheSettings.Moving || a.settings.CacheDir != source {
		t.Fatal("started changing before confirmation")
	}
	if err := tt.Click("迁移并更改"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheSettings.Moving || a.cacheSettings.Confirm.Open || a.settings.CacheDir != source {
		t.Fatal("did not start safely after confirmation")
	}
	a.chooseCacheDirectory()
	if opens != 1 {
		t.Fatal("allowed a concurrent change")
	}
	finishCacheChange(t, a, posts)
	saved := readCacheSettings(t, a.settings.Path)
	if a.cacheSettings.Error != "" || saved.CacheDir != target {
		t.Fatalf("change failed: %s %+v", a.cacheSettings.Error, saved)
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
		tt := ui.NewTester(a.views.settings.Build, 800, 1800)
		if err := tt.Click("更改缓存目录"); err != nil {
			t.Fatal(err)
		}
		if a.cacheSettings.Confirm.Open || a.cacheSettings.Moving || a.cacheSettings.Error != "" || a.settings.CacheDir != source || a.pl.Cache != cache {
			t.Fatalf("selecting current directory changed state: %s", target)
		}
		if err := a.changeCacheDirectory(target, true); err != nil || a.cacheSettings.Moving || a.pl.Cache != cache {
			t.Fatalf("same-directory change was not a no-op: %v", err)
		}
	}
	if cache.Size() != 5 {
		t.Fatal("existing cache was modified")
	}
	if _, err := os.Stat(a.settings.Path); !os.IsNotExist(err) {
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
			tt := ui.NewTester(a.views.settings.Build, 800, 1800)
			if err := tt.Click("更改缓存目录"); err != nil {
				t.Fatal(err)
			}
			if cancel == "button" {
				if err := tt.Click("取消"); err != nil {
					t.Fatal(err)
				}
			} else if cancel == "escape" {
				tt.Key(0, ui.KeyEscape)
			}
			if a.cacheSettings.Confirm.Open || a.settings.CacheDir != source || a.cacheSettings.Moving || a.pl.Cache != cache {
				t.Fatal("cancel changed active cache")
			}
		})
	}
}

// The songs move while the app runs, and what the listener changes meanwhile
// is kept.
func TestCacheDirectoryImmediate(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
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
	if a.cacheSettings.Error != "" || a.settings.CacheDir != target {
		t.Fatalf("migration failed: %s", a.cacheSettings.Error)
	}
	saved := readCacheSettings(t, a.settings.Path)
	if saved.CacheDir != target || saved.Quality != "lossless" {
		t.Fatalf("settings lost: %+v", saved)
	}
	if a.pl.Cache == nil || a.pl.Cache.Size() != 5 {
		t.Fatal("player did not use the migrated cache")
	}
	if _, err := os.Stat(filepath.Join(source, "songs", "123-exhigh.mp3")); !os.IsNotExist(err) {
		t.Fatalf("old cache remains: %v", err)
	}
}

// Changing without migration keeps the songs after it in the new directory,
// and leaves the old ones where they are.
func TestCacheDirectoryWithoutMigration(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	a.confirmCacheDirectory(target)
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	if err := tt.Click("仅更改目录"); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheSettings.Error != "" || a.pl.Cache.Size() != 0 || a.settings.CacheDir != target {
		t.Fatal("failed to switch cache")
	}
	if _, err := os.Stat(filepath.Join(source, "songs", "123-exhigh.mp3")); err != nil {
		t.Fatal(err)
	}
}

// Settings that cannot be saved leave the songs where they were, in the
// cache that was playing.
func TestCacheDirectorySaveFailure(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	cache := a.pl.Cache
	a.settings.Path = source // a directory: no settings may be written there
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheSettings.Error == "" || a.settings.CacheDir != source || a.pl.Cache != cache || cache.Size() != 5 {
		t.Fatal("failed to restore old cache")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed target remains: %v", err)
	}
}

// An occupied cache child is refused before anything stops playing.
func TestCacheDirectoryRejectsOccupied(t *testing.T) {
	a, source, target := cacheTestApp(t)
	if err := os.MkdirAll(filepath.Join(target, "songs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "songs", "personal.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.changeCacheDirectory(target, true); err == nil {
		t.Fatal("accepted occupied directory")
	}
	if a.cacheSettings.Moving || a.settings.CacheDir != source {
		t.Fatal("invalid target changed live state")
	}
}

// A cache child that turns out to hold files keeps the dialog open with what
// went wrong, and touches nothing.
func TestCacheDirectoryConfirmationFailure(t *testing.T) {
	a, source, target := cacheTestApp(t)
	if err := os.MkdirAll(filepath.Join(target, "songs"), 0o700); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(target, "songs", "personal.txt")
	if err := os.WriteFile(personal, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.confirmCacheDirectory(target)
	tt := ui.NewTester(a.views.settings.Build, 640, 420)
	if err := tt.Click("迁移并更改"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheSettings.Confirm.Open || a.cacheSettings.Confirm.Error == "" || a.cacheSettings.Moving || a.settings.CacheDir != source {
		t.Fatal("validation failure dismissed confirmation or changed cache")
	}
	if data, err := os.ReadFile(personal); err != nil || string(data) != "keep" {
		t.Fatal("occupied target was modified")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.cacheSettings.Confirm.Open {
		t.Fatal("cannot dismiss failed confirmation")
	}
}

// A count of the old cache that comes in late does not report the new one's
// usage.
func TestCacheDirectoryStaleMeasurement(t *testing.T) {
	a, _, target := cacheTestApp(t)
	posts := testPosts(a)
	old := a.pl.Cache
	a.cacheSettings.Used = 123
	a.cacheSettings.Moving = true
	a.cacheMeasured(old, 0)
	if a.cacheSettings.Used != 123 {
		t.Fatal("measurement changed usage during migration")
	}
	a.cacheSettings.Moving = false
	next := a.settings
	next.CacheDir = target
	a.pl.Cache = newSongCache(next)
	go a.measureSongCache(old)
	refreshPost(t, posts)
	if a.cacheSettings.Used != 123 {
		t.Fatal("stale cache measurement overwrote usage")
	}
	a.cacheMeasured(a.pl.Cache, 789)
	if a.cacheSettings.Used != 789 {
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
	a.cacheSettings.Moving, a.cacheSettings.Target = true, target
	tt := ui.NewTester(a.views.settings.Build, 780, 640)
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "cache-directory.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(png.Encode(f, tt.Image()), f.Close()); err != nil {
		t.Fatal(err)
	}
}
