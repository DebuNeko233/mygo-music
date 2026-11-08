package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/cachemove"
	"netease-music-mygo/internal/player"
)

// cacheDirectory is the shared root; only the managed subdirectories move.
// Other files in the system cache (such as downloaded models) stay put.
func cacheDirectory(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return mygo.App.Path(mygo.PathCache)
}

// Until a legacy conversion commits, continue reading the old song folder
// and the system's artwork/browsing folders, including after a failed move.
func cachePaths(s settings) (cachemove.Layout, error) {
	root, err := cacheDirectory(s.CacheDir)
	if err != nil {
		return cachemove.Layout{}, err
	}
	paths := cachemove.Layout{Root: root, Songs: filepath.Join(root, "songs"), Browse: filepath.Join(root, "browse-v1"), Artwork: filepath.Join(root, cachemove.ArtworkDir)}
	if s.CacheLayout == 0 {
		if s.CacheDir != "" {
			paths.Songs = s.CacheDir
		}
		system, err := cacheDirectory("")
		if err != nil {
			return cachemove.Layout{}, err
		}
		paths.Browse, paths.Artwork = filepath.Join(system, "browse-v1"), filepath.Join(system, cachemove.ArtworkDir)
	}
	return paths, nil
}

// newSongCache follows the committed layout, including a failed legacy
// conversion whose files still live at their original paths.
func newSongCache(s settings) *player.Cache {
	paths, err := cachePaths(s)
	if err != nil {
		return nil
	}
	return player.NewCache(paths.Songs, int64(s.CacheMB)<<20)
}

// applyCacheLimit gives the cache the limit the settings say.
func (a *app) applyCacheLimit() {
	if a.cacheMoving {
		// The folder moves: its songs are being counted, and dropped, there.
		return
	}
	if cache := a.pl.Cache; cache != nil {
		limit := int64(a.settings.CacheMB) << 20
		go func() {
			cache.SetLimit(limit)
			a.measureSongCache(cache)
		}()
	}
}

// measureCache finds how much the cache takes, on a goroutine, for the
// settings to show.
func (a *app) measureCache() {
	cache := a.pl.Cache
	if cache == nil {
		return
	}
	go a.measureSongCache(cache)
}

// Workers keep the cache captured on the UI thread. Reading a.pl.Cache here
// could race with relocation and measure a replacement instead of this cache.
func (a *app) measureSongCache(cache *player.Cache) {
	n := cache.Size()
	a.post(func() { a.cacheMeasured(cache, n) })
}

// cacheMeasured takes a count of what cache takes, unless it is of a cache
// that is no longer the one in use, or one taken as the folder moved.
func (a *app) cacheMeasured(cache *player.Cache, n int64) {
	if a.cacheMoving || a.pl.Cache != cache {
		return
	}
	a.cacheUsed = n
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) clearCache() {
	if a.cacheMoving {
		return
	}
	if cache := a.pl.Cache; cache != nil {
		a.cacheUsed = 0
		go func() {
			cache.Clear()
			a.measureSongCache(cache)
		}()
	}
}

// cacheUsage says how much the cache takes, once measured.
func (a *app) cacheUsage() string {
	switch n := a.cacheUsed; {
	case n < 0:
		return "正在计算已用空间…"
	case n >= 1<<30:
		return fmt.Sprintf("已用 %.1f GB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("已用 %d MB", n>>20)
	}
}
