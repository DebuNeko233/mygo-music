package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

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
			a.post(func() { a.art.imageChanged(url, change) })
		})
	}
	if root, err := mygo.App.Path(mygo.PathUserData); err == nil {
		a.identityCache = diskcache.New(filepath.Join(root, "offline-account"), 64<<10)
	}
	a.bindOfflineLyrics()
}

var lyricsRequest = netease.Lyrics

type keptLyrics = lyrics.Text

func (a *app) cachedLyricsFor(scope string) func(int64, func([]lyrics.Line, lyrics.Display)) (string, string, string, error) {
	store := a.browseCache
	fetch := lyricsRequest
	return func(id int64, refresh func([]lyrics.Line, lyrics.Display)) (string, string, string, error) {
		key := fmt.Sprintf("lyrics/%d", id)
		epoch := store.Epoch()
		var kept keptLyrics
		b, at := store.Read(scope, key)
		if b != nil && json.Unmarshal(b, &kept) == nil {
			if refresh != nil && time.Since(at) >= 7*24*time.Hour {
				go func() {
					l, t, w, err := fetch(id)
					if err != nil {
						return
					}
					store.Put(scope, key, cacheBytes(keptLyrics{Original: l, Translation: t, Words: w}), epoch)
					lines, display := lyrics.BuildDisplay(l, t, w)
					a.post(func() {
						if fmt.Sprint(a.listenerID()) == scope && store.Epoch() == epoch {
							refresh(lines, display)
						}
					})
				}()
			}
			return kept.Original, kept.Translation, kept.Words, nil
		}
		l, t, w, err := fetch(id)
		if err == nil {
			store.Put(scope, key, cacheBytes(keptLyrics{Original: l, Translation: t, Words: w}), epoch)
			return l, t, w, nil
		}
		return "", "", "", err
	}
}

// Bind the loader on the main thread when the account changes; the player
// captures it when choosing a track, rather than reading UI state on a worker.
func (a *app) bindOfflineLyrics() {
	a.pl.FetchCachedLyrics = a.cachedLyricsFor(fmt.Sprint(a.listenerID()))
}

type recoveryState struct {
	timer    *time.Timer
	attempts int
	active   bool
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
	generation := a.account.Generation
	var timer *time.Timer
	timer = recoveryAfter(recoveryGap(a.recovery.attempts), func() {
		a.post(func() {
			if a.recovery.timer != timer {
				return
			}
			a.recovery.timer = nil
			if a.recovery.active && a.account.Generation == generation {
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
	if err := a.trackArtworkFailure(); err != nil && retry(err) {
		return true
	}
	if a.account.Pending && !a.account.Busy && (a.account.Failure == nil || retry(a.account.Failure)) {
		return true
	}
	if a.home.NeedsRetry(manual) {
		return true
	}
	if a.library.NeedsRecovery(a.visiblePlaylist(), manual) {
		return true
	}
	switch a.route.page {
	case pageCloud:
		if !a.cloud.Loading() && a.cloud.Failure != nil && retry(a.cloud.Failure) {
			return true
		}
	case pageRecent:
		if tab := &a.recent.Tabs[a.recent.Selected]; !tab.Loading && tab.Failure != nil && retry(tab.Failure) {
			return true
		}
	case pageAlbum:
		if d := a.albums.Pages.Get(a.route.id); d != nil && !d.Loading && d.Error != "" && retry(d.Failure) {
			return true
		}
	case pageArtist:
		if d := a.artists.Pages.Get(a.route.id); d != nil {
			if !d.Loading && d.Error != "" && retry(d.Failure) {
				return true
			}
			switch a.artists.Tab {
			case viewmodel.ArtistLatest:
				if !d.LatestLoading && d.LatestError != "" && retry(d.LatestFailure) {
					return true
				}
			case viewmodel.ArtistAlbums:
				if !d.AlbumsPage.Loading && d.AlbumsPage.Error != "" && retry(d.AlbumsPage.Failure) {
					return true
				}
			case viewmodel.ArtistEvents:
				if !d.EventsPage.Loading && d.EventsPage.Error != "" && retry(d.EventsPage.Failure) {
					return true
				}
			}
		}
	case pageSearch:
		if d := a.searches.Get(a.route); d != nil && !d.page.Loading && d.page.Error != "" && retry(d.page.Failure) {
			return true
		}
	case pagePodcasts:
		d := a.podcastTab
		if !d.loading && (d.err != "" && retry(d.failure) || !d.hotPage.Loading && d.hotPage.Error != "" && retry(d.hotPage.Failure)) {
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
	if a.account.Pending && (a.account.Failure == nil || retry(a.account.Failure)) {
		a.confirmAccount()
	}
	a.home.Retry(manual, a.homeServices())
	if a.library.NeedsRecovery(a.visiblePlaylist(), manual) {
		a.library.RetryFailed(a.listenerID(), a.visiblePlaylist(), manual, a.libraryServices())
	}
	switch a.route.page {
	case pageCloud:
		if a.cloud.Failure != nil && retry(a.cloud.Failure) {
			a.loadCloud(viewmodel.CloudNextPage)
		}
	case pageRecent:
		if tab := &a.recent.Tabs[a.recent.Selected]; tab.Failure != nil && retry(tab.Failure) {
			a.loadRecentTab(a.recent.Selected)
		}
	case pageAlbum:
		if d := a.albums.Pages.Get(a.route.id); d != nil && !d.Loading && d.Error != "" && retry(d.Failure) {
			a.loadAlbum(a.route.id)
		}
	case pageArtist:
		if d := a.artists.Pages.Get(a.route.id); d != nil {
			if d.Error != "" && retry(d.Failure) {
				a.loadArtistProfile(a.route.id)
			}
			if a.artists.Tab == viewmodel.ArtistLatest && d.LatestError != "" && retry(d.LatestFailure) {
				a.loadLatest(a.route.id)
			}
			if d.AlbumsPage.Error != "" && a.artists.Tab == viewmodel.ArtistAlbums && retry(d.AlbumsPage.Failure) {
				a.loadAlbums(a.route.id)
			}
			if d.EventsPage.Error != "" && a.artists.Tab == viewmodel.ArtistEvents && retry(d.EventsPage.Failure) {
				a.loadEvents(a.route.id)
			}
		}
	case pageSearch:
		if d := a.searches.Get(a.route); d != nil && !d.page.Loading && d.page.Error != "" && retry(d.page.Failure) {
			a.moreResults(a.route, d)
		}
	case pagePodcasts:
		d := a.podcastTab
		if !d.loading && d.err != "" && retry(d.failure) {
			d.hotPage.Loaded = false
			a.loadPodcastTab()
		} else if !d.hotPage.Loading && d.hotPage.Error != "" && retry(d.hotPage.Failure) {
			a.loadHotPodcasts()
		}
	}
	a.retryTrackArtwork(manual)
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

func (a *app) persistLibrary(ids ...int64) {
	a.persistLibraryChanges(viewmodel.SnapshotLists|viewmodel.SnapshotAlbums|viewmodel.SnapshotLikes, ids...)
}

// Accumulate changes across overlapping edits and persist only after the
// final success or rollback has landed.
func (a *app) persistLibraryChanges(changed viewmodel.LibrarySnapshots, ids ...int64) {
	// An edit can temporarily pin cached playlists, even without a disk store.
	defer a.trimPlaylistCache()
	if a.browseCache == nil || a.account.Profile == nil {
		return
	}
	if a.library.DirtySnapshots == nil {
		a.library.DirtySnapshots = map[int64]bool{}
	}
	for _, id := range ids {
		a.library.DirtySnapshots[id] = true
	}
	a.library.Dirty |= changed
	if a.library.Refresh.Edits != 0 {
		return
	}
	keep := func(key string, value any) {
		req := a.cacheRequest(key)
		if write := req.Store.Prepare(req.Scope, key, req.Epoch); write != nil {
			go func() { write(cacheBytes(value)) }()
		}
	}
	if a.library.Dirty&viewmodel.SnapshotLists != 0 && (a.library.Mine != nil || a.library.Saved != nil) {
		keep("mine", library.Lists{Mine: copyPlaylists(a.library.Mine), Saved: copyPlaylists(a.library.Saved)})
	}
	if a.library.Dirty&viewmodel.SnapshotAlbums != 0 && a.library.SubsShown {
		keep("albums", slices.Clone(a.library.SubAlbums))
	}
	if a.library.Dirty&viewmodel.SnapshotLikes != 0 && a.library.Liked != nil {
		likes := make([]int64, 0, len(a.library.Liked))
		for id, liked := range a.library.Liked {
			if liked {
				likes = append(likes, id)
			}
		}
		slices.Sort(likes)
		keep("likes", likes)
	}
	for id := range a.library.DirtySnapshots {
		if pl := a.library.Playlists[id]; pl != nil {
			req := a.cacheRequest(fmt.Sprintf("playlist/%d", id))
			snapshot := copyPlaylist(*pl)
			a.savePlaylistSnapshot(req, func() []byte { return cacheBytes(snapshot) })
		}
	}
	clear(a.library.DirtySnapshots)
	a.library.Dirty = 0
}

// An in-flight recovery preserves its backoff, but needs no extra timer.
func (a *app) recoveryBusy() bool {
	if a.account.Busy || a.home.Loading || a.library.Refresh.Mine.Busy || a.library.Refresh.Likes.Busy || a.pl.MetadataLoading() {
		return true
	}
	if a.trackArt.searching || a.trackArt.publication.loading && a.trackArt.publication.track == a.pl.Current() || a.art.hasLoading() {
		return true
	}
	switch a.route.page {
	case pageCloud:
		return a.cloud.Loading()
	case pageRecent:
		return a.recent.ReadingSnapshot || a.recent.Tabs[a.recent.Selected].Loading
	case pagePlaylist:
		r := a.library.Refresh.Playlists[a.route.id]
		return r != nil && r.Busy
	case pageAlbum:
		d := a.albums.Pages.Get(a.route.id)
		return d != nil && d.Loading
	case pageArtist:
		d := a.artists.Pages.Get(a.route.id)
		return d != nil && (d.Loading || a.artists.Tab == viewmodel.ArtistLatest && d.LatestLoading || a.artists.Tab == viewmodel.ArtistAlbums && d.AlbumsPage.Loading || a.artists.Tab == viewmodel.ArtistEvents && d.EventsPage.Loading)
	case pageSearch:
		d := a.searches.Get(a.route)
		return d != nil && d.page.Loading
	case pagePodcasts:
		d := a.podcastTab
		return d.loading || d.hotPage.Loading
	}
	return false
}

func cacheBytes(v any) []byte { return diskcache.Encode(v) }
