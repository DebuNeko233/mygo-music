/*
为可见页面调度失败重试与离线提示，区分手动刷新和自动恢复。
*/

package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	desktopview "netease-music-mygo/internal/ui/view"
	artistvm "netease-music-mygo/internal/ui/viewmodel/artist"
	cloudvm "netease-music-mygo/internal/ui/viewmodel/cloud"
)

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
	if id, home, subs := a.podcastRecoveryScope(); a.podcasts.NeedsRecovery(id, home, subs, manual) {
		return true
	}
	switch a.nav.Current.Page {
	case pageCloud:
		if !a.cloud.Loading() && a.cloud.Failure != nil && retry(a.cloud.Failure) {
			return true
		}
	case pageRecent:
		if tab := &a.recent.Tabs[a.recent.Selected]; !tab.Loading && tab.Failure != nil && retry(tab.Failure) {
			return true
		}
	case pageAlbum:
		if d := a.albums.Pages.Get(a.nav.Current.ID); d != nil && !d.Loading && d.Error != "" && retry(d.Failure) {
			return true
		}
	case pageArtist:
		if d := a.artists.Pages.Get(a.nav.Current.ID); d != nil {
			if !d.Loading && d.Error != "" && retry(d.Failure) {
				return true
			}
			switch a.artists.Tab {
			case artistvm.ArtistLatest:
				if !d.LatestLoading && d.LatestError != "" && retry(d.LatestFailure) {
					return true
				}
			case artistvm.ArtistAlbums:
				if !d.AlbumsPage.Loading && d.AlbumsPage.Error != "" && retry(d.AlbumsPage.Failure) {
					return true
				}
			case artistvm.ArtistEvents:
				if !d.EventsPage.Loading && d.EventsPage.Error != "" && retry(d.EventsPage.Failure) {
					return true
				}
			}
		}
	case pageSearch:
		if d := a.searches.Pages.Get(a.nav.Current); d != nil && !d.Page.Loading && d.Page.Error != "" && retry(d.Page.Failure) {
			return true
		}

	}
	return a.art.HasFailed(manual) || a.pl.MetadataPending()
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
	if id, home, subs := a.podcastRecoveryScope(); a.podcasts.NeedsRecovery(id, home, subs, manual) {
		a.podcasts.Retry(id, home, subs, manual, a.podcastServices())
	}
	if a.library.NeedsRecovery(a.visiblePlaylist(), manual) {
		a.library.RetryFailed(a.listenerID(), a.visiblePlaylist(), manual, a.libraryServices())
	}
	switch a.nav.Current.Page {
	case pageCloud:
		if a.cloud.Failure != nil && retry(a.cloud.Failure) {
			a.loadCloud(cloudvm.CloudNextPage)
		}
	case pageRecent:
		if tab := &a.recent.Tabs[a.recent.Selected]; tab.Failure != nil && retry(tab.Failure) {
			a.loadRecentTab(a.recent.Selected)
		}
	case pageAlbum:
		if d := a.albums.Pages.Get(a.nav.Current.ID); d != nil && !d.Loading && d.Error != "" && retry(d.Failure) {
			a.loadAlbum(a.nav.Current.ID)
		}
	case pageArtist:
		if d := a.artists.Pages.Get(a.nav.Current.ID); d != nil {
			if d.Error != "" && retry(d.Failure) {
				a.loadArtistProfile(a.nav.Current.ID)
			}
			if a.artists.Tab == artistvm.ArtistLatest && d.LatestError != "" && retry(d.LatestFailure) {
				a.loadLatest(a.nav.Current.ID)
			}
			if d.AlbumsPage.Error != "" && a.artists.Tab == artistvm.ArtistAlbums && retry(d.AlbumsPage.Failure) {
				a.loadAlbums(a.nav.Current.ID)
			}
			if d.EventsPage.Error != "" && a.artists.Tab == artistvm.ArtistEvents && retry(d.EventsPage.Failure) {
				a.loadEvents(a.nav.Current.ID)
			}
		}
	case pageSearch:
		if d := a.searches.Pages.Get(a.nav.Current); d != nil && !d.Page.Loading && d.Page.Error != "" && retry(d.Page.Failure) {
			a.moreResults(a.nav.Current, d)
		}

	}
	a.retryTrackArtwork(manual)
	a.art.RetryFailed(manual)
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
	desktopview.OfflineNotice(c, a.pal, a.network.offline, a.refreshConnection)
}

// An in-flight recovery preserves its backoff, but needs no extra timer.
func (a *app) recoveryBusy() bool {
	if id, home, subs := a.podcastRecoveryScope(); a.podcasts.Busy(id, home, subs) {
		return true
	}
	if a.account.Busy || a.home.Loading || a.library.Refresh.Mine.Busy || a.library.Refresh.Likes.Busy || a.pl.MetadataLoading() {
		return true
	}
	if a.trackArt.searching || a.trackArt.publication.loading && a.trackArt.publication.track == a.pl.Current() || a.art.HasLoading() {
		return true
	}
	switch a.nav.Current.Page {
	case pageCloud:
		return a.cloud.Loading()
	case pageRecent:
		return a.recent.ReadingSnapshot || a.recent.Tabs[a.recent.Selected].Loading
	case pagePlaylist:
		r := a.library.Refresh.Playlists[a.nav.Current.ID]
		return r != nil && r.Busy
	case pageAlbum:
		d := a.albums.Pages.Get(a.nav.Current.ID)
		return d != nil && d.Loading
	case pageArtist:
		d := a.artists.Pages.Get(a.nav.Current.ID)
		return d != nil && (d.Loading || a.artists.Tab == artistvm.ArtistLatest && d.LatestLoading || a.artists.Tab == artistvm.ArtistAlbums && d.AlbumsPage.Loading || a.artists.Tab == artistvm.ArtistEvents && d.EventsPage.Loading)
	case pageSearch:
		d := a.searches.Pages.Get(a.nav.Current)
		return d != nil && d.Page.Loading

	}
	return false
}
