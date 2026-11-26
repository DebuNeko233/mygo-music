/*
协调各类应用缓存的初始化、容量统计、清理及离线歌词读取。
*/

package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	diskcache "netease-music-mygo/internal/features/cache/disk"
	cachemove "netease-music-mygo/internal/features/cache/move"
	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
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

// A snapshot belongs to the account that requested it. It is serialized on
// the worker but committed only after the UI accepts its generation/revision.
func (a *app) cacheRequest(key string) diskcache.Snapshot {
	return diskcache.Snapshot{Store: a.browseCache, Scope: fmt.Sprint(a.listenerID()), Key: key, Epoch: a.browseCache.Epoch()}
}

func (a *app) initOffline() {
	if paths, err := cachePaths(a.settings); err == nil {
		a.browseCache = diskcache.New(paths.Browse, 64<<20)
		a.imageCache = netease.NewArtworkStore(paths.Artwork, 256<<20)
		netease.UseImageCache(a.imageCache, func(url string, change netease.ImageChange) {
			a.post(func() { a.art.ImageChanged(url, change) })
		})
	}
	if root, err := mygo.App.Path(mygo.PathUserData); err == nil {
		a.identityCache = diskcache.New(filepath.Join(root, "offline-account"), 64<<10)
	}
	a.bindOfflineLyrics()
}

var lyricsRequest = netease.Lyrics

func (a *app) cachedLyricsFor(scope string) lyrics.Cache {
	store := a.browseCache
	return lyrics.Cache{Store: store, Scope: scope, Fetch: lyricsRequest,
		Publish: func(epoch uint64, update func()) {
			a.post(func() {
				if fmt.Sprint(a.listenerID()) == scope && store.Epoch() == epoch {
					update()
				}
			})
		},
	}
}

// Bind the loader on the main thread when the account changes; the player
// captures it when choosing a track, rather than reading UI state on a worker.
func (a *app) bindOfflineLyrics() {
	a.pl.FetchCachedLyrics = a.cachedLyricsFor(fmt.Sprint(a.listenerID())).Load
}

func (a *app) clearBrowseCache() {
	if a.cacheSettings.Moving {
		return
	}
	a.browseCache.Clear("", func() { a.post(func() { a.measureOfflineCache() }) })
}

func (a *app) clearImageCache() {
	if a.cacheSettings.Moving {
		return
	}
	a.imageCache.Clear("", func() { a.post(func() { a.measureOfflineCache() }) })
}

func (a *app) measureOfflineCache() {
	browse, images := a.browseCache, a.imageCache
	browseEpoch, imageEpoch := browse.Epoch(), images.Epoch()
	if browse == nil && images == nil {
		return
	}
	go func() {
		b, i := browse.Size(), images.Size()
		a.post(func() {
			if a.cacheSettings.Moving || a.browseCache != browse || a.imageCache != images || browse.Epoch() != browseEpoch || images.Epoch() != imageEpoch {
				return
			}
			a.browseUsed, a.imagesUsed = b, i
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}
