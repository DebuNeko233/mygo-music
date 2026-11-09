package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// Returning to a previously used default root must adopt its caches, including when selected explicitly.
func TestCacheDirectoryAdoptsPopulatedDefault(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "picker"}[explicit], func(t *testing.T) {
			a, source, _ := cacheTestApp(t)
			posts := testPosts(a)
			system := t.TempDir()
			previous, _ := mygo.App.Path(mygo.PathCache)
			mygo.App.SetPath(mygo.PathCache, system)
			t.Cleanup(func() { mygo.App.SetPath(mygo.PathCache, previous) })
			writeCachedSong(t, source)
			seedOfflineCaches(t, a)
			writeCachedSong(t, system)
			if err := os.WriteFile(filepath.Join(system, "songs", "123-exhigh.mp3"), []byte("prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			s := diskcache.New(filepath.Join(system, "browse-v1"), 1<<20)
			s.Put("7", "playlist/9", []byte("previous default"), 0)
			s.Close()
			selected := ""
			if explicit {
				selected = system
			}
			// A real conflict remains protected; switching can still adopt this root.
			if err := a.changeCacheDirectory(selected, true); err != nil {
				t.Fatal(err)
			}
			finishCacheChange(t, a, posts)
			if a.cacheError == "" || a.settings.CacheDir != source {
				t.Fatal("overwrote a conflicting destination")
			}
			if err := a.changeCacheDirectory(selected, false); err != nil {
				t.Fatal(err)
			}
			finishCacheChange(t, a, posts)
			if a.cacheError != "" || a.settings.CacheDir != "" || a.pl.Cache.Size() != 5 {
				t.Fatal("could not return to existing default caches", a.cacheError)
			}
			if b, _ := a.browseCache.Read("7", "playlist/9"); string(b) != "previous default" {
				t.Fatal("did not adopt default snapshots")
			}
			if b, err := os.ReadFile(filepath.Join(source, "songs", "123-exhigh.mp3")); err != nil || string(b) != "music" {
				t.Fatal("switch changed source", err)
			}
			if b, err := os.ReadFile(filepath.Join(system, "songs", "123-exhigh.mp3")); err != nil || string(b) != "prior" {
				t.Fatal("switch changed destination", err)
			}
		})
	}
}

// Clicking the migration banner's cancel action rolls back without waiting for settings to commit.
func TestCacheDirectoryCancelFromMainWindow(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	seedOfflineCaches(t, a)
	a.profile = &netease.Profile{UserID: 7, Nickname: "本地测试"}
	a.route = route{page: pagePlaylist, id: 9}
	a.playlists[9] = &netease.Playlist{ID: 9, Name: "已缓存歌单", Tracks: []netease.Track{{ID: 1, Name: "迁移期间仍可浏览的曲目", Duration: 180000}}}
	a.mine = []netease.Playlist{*a.playlists[9]}
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	for a.cacheTotal == 0 && a.cacheMoving {
		refreshPost(t, posts)
	}
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			suffix := "light"
			if dark {
				suffix = "dark"
			}
			f, err := os.Create(filepath.Join(dir, "cache-move-progress-"+suffix+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tt.Click("取消迁移"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheCancelling {
		t.Fatal("main-window action did not cancel the copy")
	}
	finishCacheChange(t, a, posts)
	if a.settings.CacheDir != source || a.pl.Cache.Size() != 5 {
		t.Fatal("cancel lost source cache")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancel left a published target", err)
	}
	if b, _ := a.browseCache.Read("7", "playlist/9"); string(b) != "page" {
		t.Fatal("cancel left page reads unavailable")
	}
}

// A quit requested during migration cancels safely and is retried after rollback instead of being ignored.
func TestCacheDirectoryQuitWaitsForRollback(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	old := quitAfterCacheMove
	quits := 0
	quitAfterCacheMove = func() { quits++ }
	t.Cleanup(func() { quitAfterCacheMove = old })
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	if !a.deferCacheQuit() || !a.cacheQuit || !a.cacheCancelling {
		t.Fatal("migration ignored the quit request")
	}
	for a.cacheMoving {
		refreshPost(t, posts)
	}
	if quits != 1 || a.settings.CacheDir != source {
		t.Fatal("quit did not wait for rollback", quits)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("quit left a published target", err)
	}
	if b, err := os.ReadFile(filepath.Join(source, "songs", "123-exhigh.mp3")); err != nil || string(b) != "music" {
		t.Fatal("quit lost original audio", err)
	}
}

// Startup cleanup finishes before a new migration, while a completed layout keeps its selected root.
func TestCacheDirectoryRecoveryKeepsCommittedLayout(t *testing.T) {
	a, source, _ := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	a.recoverCacheDirectory()
	for a.cacheMoving {
		refreshPost(t, posts)
	}
	if a.settings.CacheDir != source || a.pl.Cache.Size() != 5 || a.cacheError != "" {
		t.Fatal("startup cleanup changed a committed layout", a.cacheError)
	}
}

// Restoring defaults also clears an old explicit system-path preference without relocating its files.
func TestCacheDirectoryNormalizesExplicitDefault(t *testing.T) {
	a, source, _ := cacheTestApp(t)
	previous, _ := mygo.App.Path(mygo.PathCache)
	mygo.App.SetPath(mygo.PathCache, source)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathCache, previous) })
	writeCachedSong(t, source)
	cache := a.pl.Cache
	a.confirmCacheDirectory("")
	if a.cacheConfirm.open || a.cacheMoving || a.cacheError != "" || a.settings.CacheDir != "" || a.pl.Cache != cache || cache.Size() != 5 {
		t.Fatal("restoring defaults did not normalize the existing path", a.cacheError)
	}
	if readCacheSettings(t, a.settings.path).CacheDir != "" {
		t.Fatal("default preference was not persisted")
	}
}

// Confirming migration must preserve unrelated target data instead of demanding an empty root.
func TestCacheDirectoryAcceptsUnmanagedTargetContent(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := testPosts(a)
	writeCachedSong(t, source)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(target, ".DS_Store")
	if err := os.WriteFile(metadata, []byte("Finder metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(target, "notes.txt")
	if err := os.WriteFile(personal, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.confirmCacheDirectory(target)
	tt := ui.NewTester(a.settingsView, 800, 1100)
	if err := tt.Click("迁移并更改"); err != nil {
		t.Fatal(err)
	}
	if !a.cacheMoving {
		t.Fatal("unrelated target content prevented migration", a.cacheConfirm.err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError != "" || a.settings.CacheDir != target || a.pl.Cache.Size() != 5 {
		t.Fatal("could not adopt Finder folder", a.cacheError)
	}
	if b, err := os.ReadFile(metadata); err != nil || string(b) != "Finder metadata" {
		t.Fatal("changed Finder metadata", err)
	}
	if b, err := os.ReadFile(personal); err != nil || string(b) != "keep" {
		t.Fatal("changed unrelated file", err)
	}
}
