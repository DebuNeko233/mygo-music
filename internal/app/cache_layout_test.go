package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

func seedOfflineCaches(t *testing.T, a *app) {
	t.Helper()
	paths, err := cachePaths(a.settings)
	if err != nil {
		t.Fatal(err)
	}
	a.browseCache = diskcache.New(paths.Browse, 1<<20)
	a.imageCache = netease.NewArtworkStore(paths.Artwork, 1<<20)
	t.Cleanup(func() { a.browseCache.Close(); a.imageCache.Close() })
	a.browseCache.Put("7", "playlist/9", []byte("page"), 0)
	a.browseCache.Put("7", "lyrics/1", []byte("lyrics"), 0)
	a.imageCache.Put("content", "cover", []byte("image"), 0)
	a.imageCache.Put("requests", "cover", []byte(`{"content":"cover"}`), 0)
}

// A live directory change moves all cache stores and rejects writes captured
// before the move, retaining shared store handles used by artwork and lyrics.
func TestCacheDirectoryMovesOfflineResources(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	writeCachedSong(t, source)
	seedOfflineCaches(t, a)
	browse, images := a.browseCache, a.imageCache
	epoch := browse.Epoch()
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError != "" || a.browseCache != browse || a.imageCache != images || a.pl.Cache.Size() != 5 {
		t.Fatal("lost stores or audio", a.cacheError)
	}
	for key, want := range map[string]string{"playlist/9": "page", "lyrics/1": "lyrics"} {
		if b, _ := browse.Read("7", key); string(b) != want {
			t.Fatalf("lost %s", key)
		}
	}
	if b, _ := images.Read("content", "cover"); string(b) != "image" {
		t.Fatal("lost shared artwork")
	}
	if b, _ := images.Read("requests", "cover"); string(b) != `{"content":"cover"}` {
		t.Fatal("lost shared artwork index")
	}
	browse.Put("7", "stale", []byte("old"), epoch)
	browse.Put("7", "new", []byte("fresh"), browse.Epoch())
	old := diskcache.New(filepath.Join(source, "browse-v1"), 1<<20)
	defer old.Close()
	fresh := diskcache.New(filepath.Join(target, "browse-v1"), 1<<20)
	defer fresh.Close()
	if b, _ := old.Read("7", "new"); b != nil {
		t.Fatal("new write recreated old cache")
	}
	if b, _ := fresh.Read("7", "new"); string(b) != "fresh" {
		t.Fatal("new write missed selected directory")
	}
	if b, _ := fresh.Read("7", "stale"); b != nil {
		t.Fatal("old callback wrote into new cache")
	}
}

// A failed settings save resumes every original store; no resource is lost
// and subsequent cache writes still target the original directory.
func TestCacheDirectoryOfflineRollback(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	seedOfflineCaches(t, a)
	a.settings.path = source
	if err := a.changeCacheDirectory(target, true); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError == "" || a.settings.CacheDir != source {
		t.Fatal("committed a failed move")
	}
	if b, _ := a.browseCache.Read("7", "playlist/9"); string(b) != "page" {
		t.Fatal("lost original page")
	}
	if b, _ := a.imageCache.Read("content", "cover"); string(b) != "image" {
		t.Fatal("lost original image")
	}
	if b, _ := a.imageCache.Read("requests", "cover"); string(b) != `{"content":"cover"}` {
		t.Fatal("rollback lost the original index")
	}
	a.browseCache.Put("7", "new", []byte("fresh"), a.browseCache.Epoch())
	old := diskcache.New(filepath.Join(source, "browse-v1"), 1<<20)
	defer old.Close()
	if b, _ := old.Read("7", "new"); string(b) != "fresh" {
		t.Fatal("did not resume original store")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed target remains", err)
	}
}

// Old song-only settings automatically consolidate separate cache folders,
// preserve login data and persist a layout that opens the same files on restart.
func TestCacheDirectoryLegacyConversion(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			a, source, _ := cacheTestApp(t)
			posts := cachePosts(t)
			system := t.TempDir()
			previous, _ := mygo.App.Path(mygo.PathCache)
			mygo.App.SetPath(mygo.PathCache, system)
			t.Cleanup(func() { mygo.App.SetPath(mygo.PathCache, previous) })
			a.settings.CacheLayout = 0
			if !custom {
				a.settings.CacheDir = ""
				source = system
			}
			a.settings.path, _ = settingsPath()
			if err := a.settings.save(); err != nil {
				t.Fatal(err)
			}
			a.settings = loadSettings()
			paths, err := cachePaths(a.settings)
			if err != nil || a.settings.CacheLayout != 0 {
				t.Fatal("legacy setting was not detected", err)
			}
			writeCachedSongIn(t, paths.Songs)
			a.pl.Cache = newSongCache(a.settings)
			seedOfflineCaches(t, a)
			identity := diskcache.New(filepath.Join(t.TempDir(), "identity"), 1<<20)
			defer identity.Close()
			identity.Put("bound", "profile", []byte("login"), 0)
			a.identityCache = identity
			savedPath := a.settings.path
			a.settings.path = source
			a.upgradeCacheDirectory()
			finishCacheChange(t, a, posts)
			if a.cacheError == "" || a.settings.CacheLayout != 0 || a.pl.Cache.Size() != 5 {
				t.Fatal("failed upgrade did not preserve legacy paths")
			}
			a.settings.path = savedPath
			a.upgradeCacheDirectory()
			finishCacheChange(t, a, posts)
			if a.cacheError != "" || a.settings.CacheLayout != 1 || a.pl.Cache.Size() != 5 {
				t.Fatal("conversion lost audio", a.cacheError)
			}
			if b, _ := a.browseCache.Read("7", "lyrics/1"); string(b) != "lyrics" {
				t.Fatal("conversion lost lyrics")
			}
			if b, _ := a.imageCache.Read("content", "cover"); string(b) != "image" {
				t.Fatal("conversion lost images")
			}
			if b, _ := identity.Read("bound", "profile"); string(b) != "login" {
				t.Fatal("conversion affected login")
			}
			restarted := loadSettings()
			next, err := cachePaths(restarted)
			if err != nil || restarted.CacheLayout != 1 || next.Songs != filepath.Join(source, "songs") || next.Browse != filepath.Join(source, "browse-v1") || newSongCache(restarted).Size() != 5 {
				t.Fatal("restart did not retain converted layout", err)
			}
			if custom {
				if _, err := os.Stat(filepath.Join(paths.Songs, "123-exhigh.mp3")); !os.IsNotExist(err) {
					t.Fatal("legacy flat audio remains", err)
				}
			}
			a.upgradeCacheDirectory()
			if a.cacheMoving {
				t.Fatal("converted layout was migrated again")
			}
		})
	}
}

// Switching without migration gives all three resources a new store while
// leaving existing offline content intact at the old location.
func TestCacheDirectorySwitchOfflineResources(t *testing.T) {
	a, source, target := cacheTestApp(t)
	posts := cachePosts(t)
	seedOfflineCaches(t, a)
	if err := a.changeCacheDirectory(target, false); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if b, _ := a.browseCache.Read("7", "lyrics/1"); b != nil {
		t.Fatal("switch unexpectedly migrated lyrics")
	}
	if b, _ := a.imageCache.Read("content", "cover"); b != nil {
		t.Fatal("switch unexpectedly migrated artwork")
	}
	if b, _ := a.imageCache.Read("requests", "cover"); b != nil {
		t.Fatal("switch retained the old in-memory index")
	}
	old := diskcache.New(filepath.Join(source, "browse-v1"), 1<<20)
	defer old.Close()
	if b, _ := old.Read("7", "lyrics/1"); string(b) != "lyrics" {
		t.Fatal("switch deleted old content")
	}
}

// Restoring the system cache moves only managed children, preserving models
// and other application files already kept beside them in the default root.
func TestCacheDirectoryRestoreDefault(t *testing.T) {
	a, source, _ := cacheTestApp(t)
	posts := cachePosts(t)
	system := t.TempDir()
	previous, _ := mygo.App.Path(mygo.PathCache)
	mygo.App.SetPath(mygo.PathCache, system)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathCache, previous) })
	model := filepath.Join(system, "model.bin")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCachedSong(t, source)
	seedOfflineCaches(t, a)
	if err := a.changeCacheDirectory("", true); err != nil {
		t.Fatal(err)
	}
	finishCacheChange(t, a, posts)
	if a.cacheError != "" || a.settings.CacheDir != "" || a.pl.Cache.Size() != 5 {
		t.Fatal("failed to restore default", a.cacheError)
	}
	if b, _ := a.browseCache.Read("7", "playlist/9"); string(b) != "page" {
		t.Fatal("lost default page cache")
	}
	if b, err := os.ReadFile(model); err != nil || string(b) != "model" {
		t.Fatal("changed unrelated model")
	}
}
