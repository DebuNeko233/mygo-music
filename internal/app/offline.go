package app

import (
	"encoding/json"
	"fmt"
	"log"
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
func cacheBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Types identify a broken encoder without logging a session or payload.
		log.Printf("cache: cannot encode %T (%T)", v, err)
		return nil
	}
	return b
}
func (r cacheRequest) keep(b []byte) {
	if b != nil {
		r.store.Queue(r.scope, r.key, b, r.epoch)
	}
}

func (a *app) initOffline() {
	if paths, err := cachePaths(a.settings); err == nil {
		a.browseCache = diskcache.New(paths.Browse, 64<<20)
		a.imageCache = netease.NewArtworkStore(paths.Artwork, 256<<20)
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
	accountFailure              error
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
func (a *app) needsRecovery() bool { return a.failedLoads(false) }

// Manual refresh includes permanent failures, while timers and reconnection
// retry only temporary ones. The presentation string never controls scheduling.
func (a *app) failedLoads(manual bool) bool {
	retry := func(err error) bool { return manual || netease.RetryableError(err) }
	if a.recovery.accountPending && !a.recovery.accountBusy && (a.recovery.accountFailure == nil || retry(a.recovery.accountFailure)) {
		return true
	}
	if !a.homeLoading {
		for _, err := range a.homeFailures {
			if retry(err) {
				return true
			}
		}
	}
	if a.listRefresh.mine.failed && retry(a.listRefresh.mine.failure) || a.listRefresh.likes.failed && retry(a.listRefresh.likes.failure) {
		return true
	}
	switch a.route.page {
	case pagePlaylist:
		if r := a.listRefresh.playlists[a.route.id]; r != nil && !r.busy && r.failed && retry(r.failure) {
			return true
		}
	case pageAlbum:
		if d := a.albums[a.route.id]; d != nil && !d.loading && d.err != "" && retry(d.failure) {
			return true
		}
	case pageArtist:
		if d := a.artists[a.route.id]; d != nil {
			if !d.loading && d.err != "" && retry(d.failure) {
				return true
			}
			switch a.artistTab {
			case artistLatest:
				if !d.latestLoading && d.latestErr != "" && retry(d.latestFailure) {
					return true
				}
			case artistAlbums:
				if !d.albumsPage.loading && d.albumsPage.err != "" && retry(d.albumsPage.failure) {
					return true
				}
			}
		}
	case pageSearch:
		if d := a.searches[a.route]; d != nil && !d.page.loading && d.page.err != "" && retry(d.page.failure) {
			return true
		}
	}
	return a.art.hasFailed(manual) || a.pl.MetadataPending()
}
func (a *app) recoverLoads() { a.retryLoads(false) }
func (a *app) retryLoads(manual bool) {
	if a.network.offline {
		return
	}
	retry := func(err error) bool { return manual || netease.RetryableError(err) }
	if a.recovery.accountPending && (a.recovery.accountFailure == nil || retry(a.recovery.accountFailure)) {
		a.confirmAccount()
	}
	if !a.homeLoading && len(a.homeFailures) > 0 {
		a.loadHomeModules(true, manual)
	}
	if a.profile != nil && a.listRefresh.edits == 0 {
		if a.listRefresh.mine.failed && retry(a.listRefresh.mine.failure) {
			a.loadMine(a.profile.UserID)
		}
		if a.listRefresh.likes.failed && retry(a.listRefresh.likes.failure) {
			a.loadLiked(a.profile.UserID)
		}
	}
	switch a.route.page {
	case pagePlaylist:
		if r := a.listRefresh.playlists[a.route.id]; r != nil && !r.busy && r.failed && retry(r.failure) {
			r.at = time.Time{}
			a.loadPlaylist(a.route.id, false)
		}
	case pageAlbum:
		if d := a.albums[a.route.id]; d != nil && !d.loading && d.err != "" && retry(d.failure) {
			a.loadAlbum(a.route.id)
		}
	case pageArtist:
		if d := a.artists[a.route.id]; d != nil {
			if d.err != "" && retry(d.failure) {
				a.loadArtistProfile(a.route.id)
			}
			if a.artistTab == artistLatest && d.latestErr != "" && retry(d.latestFailure) {
				a.loadLatest(a.route.id)
			}
			if d.albumsPage.err != "" && a.artistTab == artistAlbums && retry(d.albumsPage.failure) {
				a.loadAlbums(a.route.id)
			}
		}
	case pageSearch:
		if d := a.searches[a.route]; d != nil && !d.page.loading && d.page.err != "" && retry(d.page.failure) {
			a.moreResults(a.route, d)
		}
	}
	a.art.retryFailed(manual)
	if manual {
		a.pl.RetryMetadata()
	} else {
		a.pl.RecoverMetadata()
	}
}

func (a *app) offlineNotice(c *ui.Context) {
	pending := a.failedLoads(true)
	a.network.recovering = pending || a.network.recovering && a.recoveryBusy()
	if !a.network.offline && !a.network.recovering {
		return
	}
	// Pending account verification alone is not evidence of being offline.
	if a.browseCache == nil && !a.network.known {
		return
	}
	ui.Row(c.Key("offline-status")).Absolute().Top(topBar(c) + 4).Right(24).
		Gap(2).Padding(3).Radius(999).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
		// A reachable network may still have failed resources. Keep the refresh
		// action, but never display the offline symbol for that online state.
		if a.network.offline {
			ui.Box(c.Key("offline-icon")).Size(30, 30).Center().Label("离线").Tooltip("离线，可浏览缓存").Children(func() {
				ui.Icon(c, iconOffline).Size(18, 18).TextColor(a.pal.muted)
			})
		}
		if iconButton(c.Key("refresh-connection"), iconReload, 18, a.pal.muted, "刷新内容").Size(30, 30).Tooltip("刷新内容").Clicked() {
			a.refreshConnection()
		}
	})
}

func (a *app) clearBrowseCache() {
	if a.cacheMoving {
		return
	}
	a.browseCache.Clear("", func() { a.post(func() { a.measureOfflineCache() }) })
}
func (a *app) clearImageCache() {
	if a.cacheMoving {
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
			if a.cacheMoving || a.browseCache != browse || a.imageCache != images || browse.Epoch() != browseEpoch || images.Epoch() != imageEpoch {
				return
			}
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
		return d != nil && (d.loading || a.artistTab == artistLatest && d.latestLoading || a.artistTab == artistAlbums && d.albumsPage.loading)
	case pageSearch:
		d := a.searches[a.route]
		return d != nil && d.page.loading
	}
	return false
}
