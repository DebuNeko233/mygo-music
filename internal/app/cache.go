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

// cachePaths keeps all managed caches under the selected root.
func cachePaths(s settings) (cachemove.Layout, error) {
	root, err := cacheDirectory(s.CacheDir)
	if err != nil {
		return cachemove.Layout{}, err
	}
	return cachemove.Layout{Root: root, Songs: filepath.Join(root, "songs"), Browse: filepath.Join(root, "browse-v1"), Artwork: filepath.Join(root, cachemove.ArtworkDir)}, nil
}

// newSongCache opens songs under the committed cache root.
func newSongCache(s settings) *player.Cache {
	paths, err := cachePaths(s)
	if err != nil {
		return nil
	}
	return player.NewCache(paths.Songs, int64(s.CacheMB)<<20)
}

// applyCacheLimit gives the cache the limit the settings say.
func (a *app) applyCacheLimit() {
	if a.cacheSettings.Moving {
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
	if a.cacheSettings.Moving || a.pl.Cache != cache {
		return
	}
	a.cacheSettings.Used = n
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) clearCache() {
	if a.cacheSettings.Moving {
		return
	}
	if cache := a.pl.Cache; cache != nil {
		a.cacheSettings.Used = 0
		go func() {
			cache.Clear()
			a.measureSongCache(cache)
		}()
	}
}

// cacheUsage says how much the cache takes, once measured.
func (a *app) cacheUsage() string {
	switch n := a.cacheSettings.Used; {
	case n < 0:
		return "正在计算已用空间…"
	case n >= 1<<30:
		return fmt.Sprintf("已用 %.1f GB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("已用 %d MB", n>>20)
	}
}
