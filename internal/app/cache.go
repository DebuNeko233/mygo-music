package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/player"
)

// songCacheDirectory is where the songs played are kept: the directory the
// settings name, or the app's own cache directory without one.
func songCacheDirectory(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "songs"), nil
}

// newSongCache returns the cache of songs played, in the directory the
// settings name, or nil without one.
func newSongCache(mb int, configured string) *player.Cache {
	dir, err := songCacheDirectory(configured)
	if err != nil {
		return nil
	}
	return player.NewCache(dir, int64(mb)<<20)
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
			a.measureCache()
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
	go func() {
		n := cache.Size()
		a.post(func() {
			a.cacheMeasured(cache, n)
		})
	}()
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
			a.measureCache()
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
