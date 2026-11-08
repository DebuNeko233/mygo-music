package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// A snapshot belongs to the account that requested it. It is serialized on
// the worker but committed only after the UI accepts its generation/revision.
type cacheRequest struct {
	store      *diskcache.Store
	scope, key string
	epoch      uint64
}

func (a *app) cacheRequest(key string) cacheRequest {
	return cacheRequest{a.browseCache, fmt.Sprint(a.listenerID()), key, a.browseCache.Epoch()}
}
func (r cacheRequest) read(value any) time.Time {
	b, at := r.store.Read(r.scope, r.key)
	if b == nil || json.Unmarshal(b, value) != nil {
		return time.Time{}
	}
	return at
}
func cacheBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func (r cacheRequest) keep(b []byte) {
	if b != nil {
		r.store.Queue(r.scope, r.key, b, r.epoch)
	}
}

func (a *app) initOffline() {
	if root, err := mygo.App.Path(mygo.PathCache); err == nil {
		a.browseCache = diskcache.New(filepath.Join(root, "browse-v1"), 64<<20)
		a.imageCache = diskcache.New(filepath.Join(root, "artwork-v1"), 256<<20)
		netease.UseImageCache(a.imageCache, func(url string) { a.post(func() { a.art.invalidate(url) }) })
	}
	if root, err := mygo.App.Path(mygo.PathUserData); err == nil {
		a.identityCache = diskcache.New(filepath.Join(root, "offline-account"), 64<<10)
	}
	a.bindOfflineLyrics()
}

var lyricsRequest = netease.Lyrics

type keptLyrics struct{ Original, Translation, Words string }

func (a *app) cachedLyricsFor(scope string) func(int64) (string, string, string, error) {
	store := a.browseCache
	fetch := lyricsRequest
	return func(id int64) (string, string, string, error) {
		key := fmt.Sprintf("lyrics/%d", id)
		epoch := store.Epoch()
		var kept keptLyrics
		b, at := store.Read(scope, key)
		if b != nil && json.Unmarshal(b, &kept) == nil {
			if time.Since(at) >= 7*24*time.Hour {
				go func() {
					l, t, w, err := fetch(id)
					if err != nil {
						return
					}
					store.Put(scope, key, cacheBytes(keptLyrics{l, t, w}), epoch)
					a.post(func() {
						if fmt.Sprint(a.listenerID()) == scope && store.Epoch() == epoch {
							a.pl.RefreshLyrics(id, l, t, w)
						}
					})
				}()
			}
			return kept.Original, kept.Translation, kept.Words, nil
		}
		l, t, w, err := fetch(id)
		if err == nil {
			store.Put(scope, key, cacheBytes(keptLyrics{l, t, w}), epoch)
			return l, t, w, nil
		}
		return "", "", "", err
	}
}

// Bind the loader on the main thread when the account changes; the player
// captures it when choosing a track, rather than reading UI state on a worker.
func (a *app) bindOfflineLyrics() { a.pl.FetchLyrics = a.cachedLyricsFor(fmt.Sprint(a.listenerID())) }

type recoveryState struct {
	timer                       *time.Timer
	attempts                    int
	active                      bool
	accountBusy, accountPending bool
}

var recoveryAfter = time.AfterFunc

func recoveryGap(attempt int) time.Duration {
	gaps := [...]time.Duration{2, 5, 15, 30, 60}
	return gaps[min(attempt, len(gaps)-1)] * time.Second
}

// Recovery sleeps without asking for animation frames. Only failures needed
// by the visible page are retried; healthy idle views have no timer.
func (a *app) recoveryFrame(active bool) {
	if a.browseCache == nil && !a.network.known {
		return
	}
	was := a.recovery.active
	a.recovery.active = active
	if !active || a.network.offline {
		if a.recovery.timer != nil {
			a.recovery.timer.Stop()
			a.recovery.timer = nil
		}
		return
	}
	if !was {
		a.recoverLoads()
	}
	if !a.needsRecovery() {
		if !a.recoveryBusy() {
			a.recovery.attempts = 0
		}
		if a.recovery.timer != nil {
			a.recovery.timer.Stop()
			a.recovery.timer = nil
		}
		return
	}
	if a.recovery.timer != nil {
		return
	}
	generation := a.homeGen
	var timer *time.Timer
	timer = recoveryAfter(recoveryGap(a.recovery.attempts), func() {
		a.post(func() {
			if a.recovery.timer != timer {
				return
			}
			a.recovery.timer = nil
			if a.recovery.active && a.homeGen == generation {
				a.recovery.attempts++
				a.recoverLoads()
			}
		})
	})
	a.recovery.timer = timer
}
func (a *app) needsRecovery() bool {
	if a.recovery.accountPending && !a.recovery.accountBusy || !a.homeLoading && (a.homeErr != "" || a.homeFailed) {
		return true
	}
	if a.listRefresh.mine.failed || a.listRefresh.likes.failed {
		return true
	}
	switch a.route.page {
	case pagePlaylist:
		if r := a.listRefresh.playlists[a.route.id]; r != nil && r.failed {
			return true
		}
	case pageAlbum:
		if d := a.albums[a.route.id]; d != nil && d.err != "" {
			return true
		}
	case pageArtist:
		if d := a.artists[a.route.id]; d != nil && (d.err != "" || d.latestErr != "" || d.albumsPage.err != "") {
			return true
		}
	case pageSearch:
		if d := a.searches[a.route]; d != nil && d.page.err != "" {
			return true
		}
	}
	return a.art.hasFailed() || a.pl.MetadataPending()
}
func (a *app) recoverLoads() {
	if a.network.offline {
		return
	}
	if a.recovery.accountPending {
		a.confirmAccount()
	}
	if (a.homeErr != "" || a.homeFailed) && !a.homeLoading {
		a.retryHome()
	}
	if a.profile != nil && a.listRefresh.edits == 0 {
		if a.listRefresh.mine.failed {
			a.loadMine(a.profile.UserID)
		}
		if a.listRefresh.likes.failed {
			a.loadLiked(a.profile.UserID)
		}
	}
	switch a.route.page {
	case pagePlaylist:
		if r := a.listRefresh.playlists[a.route.id]; r != nil && r.failed && !r.busy {
			r.at = time.Time{}
			a.loadPlaylist(a.route.id, false)
		}
	case pageAlbum:
		if d := a.albums[a.route.id]; d != nil && d.err != "" {
			a.loadAlbum(a.route.id)
		}
	case pageArtist:
		if d := a.artists[a.route.id]; d != nil {
			if d.err != "" {
				a.loadArtist(a.route.id)
			}
			if d.latestErr != "" {
				a.loadLatest(a.route.id)
			}
			if d.albumsPage.err != "" && a.artistTab == artistAlbums {
				a.loadAlbums(a.route.id)
			}
		}
	case pageSearch:
		if d := a.searches[a.route]; d != nil && d.page.err != "" {
			a.moreResults(a.route, d)
		}
	}
	a.art.retryFailed()
	a.pl.RetryMetadata()
}

func (a *app) offlineNotice(c *ui.Context) {
	pending := a.needsRecovery()
	a.network.recovering = pending || a.network.recovering && a.recoveryBusy()
	if !a.network.offline && !a.network.recovering {
		return
	}
	// Pending account verification alone is not evidence of being offline.
	if a.browseCache == nil && !a.network.known {
		return
	}
	description := "部分内容暂时无法加载，点击刷新重试"
	if a.network.offline {
		description = "当前离线；可继续浏览已缓存内容"
	}
	ui.Row(c.Key("offline-status")).Absolute().Top(topBar(c) + 4).Right(24).
		Gap(2).Padding(3).Radius(999).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
		ui.Box(c.Key("offline-icon")).Size(30, 30).Center().Label("网络连接状态").Tooltip(description).Children(func() {
			ui.Icon(c, iconOffline).Size(18, 18).TextColor(a.pal.muted)
		})
		if iconButton(c.Key("refresh-connection"), iconReload, 18, a.pal.muted, "刷新网络与内容").Size(30, 30).Tooltip("刷新网络与内容").Clicked() {
			a.refreshConnection()
		}
	})
}

func (a *app) clearBrowseCache() {
	a.browseCache.Clear("", func() { a.post(func() { a.measureOfflineCache() }) })
}
func (a *app) clearImageCache() {
	a.imageCache.Clear("", func() { a.post(func() { a.measureOfflineCache() }) })
}
func (a *app) measureOfflineCache() {
	browse, images := a.browseCache, a.imageCache
	if browse == nil && images == nil {
		return
	}
	go func() {
		b, i := browse.Size(), images.Size()
		a.post(func() {
			a.browseUsed, a.imagesUsed = b, i
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}

// persistLibrary is deferred until an edit's success or rollback has landed.
// Copies leave the UI thread; only the changed playlist needs serializing.
func (a *app) persistLibrary(ids ...int64) {
	if a.browseCache == nil || a.profile == nil {
		return
	}
	if a.dirtySnapshots == nil {
		a.dirtySnapshots = map[int64]bool{}
	}
	for _, id := range ids {
		a.dirtySnapshots[id] = true
	}
	if a.listRefresh.edits != 0 {
		return
	}
	keep := func(key string, value any) {
		req := a.cacheRequest(key)
		if write := req.store.Prepare(req.scope, key, req.epoch); write != nil {
			go func() { write(cacheBytes(value)) }()
		}
	}
	if a.mine != nil || a.saved != nil {
		keep("mine", keptLists{copyPlaylists(a.mine), copyPlaylists(a.saved)})
	}
	if a.liked != nil {
		likes := make([]int64, 0, len(a.liked))
		for id, liked := range a.liked {
			if liked {
				likes = append(likes, id)
			}
		}
		keep("likes", likes)
	}
	for id := range a.dirtySnapshots {
		if pl := a.playlists[id]; pl != nil {
			keep(fmt.Sprintf("playlist/%d", id), copyPlaylist(*pl))
		}
	}
	clear(a.dirtySnapshots)
}

// An in-flight recovery preserves its backoff, but needs no extra timer.
func (a *app) recoveryBusy() bool {
	if a.recovery.accountBusy || a.homeLoading || a.listRefresh.mine.busy || a.listRefresh.likes.busy || a.pl.MetadataLoading() {
		return true
	}
	if a.art.hasLoading() {
		return true
	}
	switch a.route.page {
	case pagePlaylist:
		r := a.listRefresh.playlists[a.route.id]
		return r != nil && r.busy
	case pageAlbum:
		d := a.albums[a.route.id]
		return d != nil && d.loading
	case pageArtist:
		d := a.artists[a.route.id]
		return d != nil && (d.loading || d.latestLoading || d.albumsPage.loading)
	case pageSearch:
		d := a.searches[a.route]
		return d != nil && d.page.loading
	}
	return false
}
