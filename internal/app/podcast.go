package app

import (
	"netease-music-mygo/internal/netease"
	podcastvm "netease-music-mygo/internal/viewmodel/podcast"
)

func (a *app) podcastServices() podcastvm.PodcastServices {
	return podcastvm.PodcastServices{
		SignedIn: a.account.Profile != nil, Post: a.detailPost(), Detail: podcastDetail, Programs: podcastPrograms,
		Subscribe: subscribePodcast, Subscriptions: subscribedPodcasts, Recommended: recommendedPodcasts, Hot: hotPodcasts,
		QueryLikes: a.askProgramLikes, Play: a.pl.PlayQueue, QueueRevision: a.pl.QueueRevision, CurrentTrack: a.pl.Current,
		Notify: func(message string) { a.toast = message },
	}
}

func (a *app) loadPodcast(id int64) {
	if !a.podcasts.NeedsLoad(id) {
		a.podcasts.Pages.Get(id)
		return
	}
	a.podcasts.Load(id, a.podcastServices())
}

func (a *app) loadPrograms(id int64) { a.podcasts.LoadPrograms(id, a.podcastServices()) }

func (a *app) playPodcast(id int64) { a.podcasts.RequestPlay(id, a.podcastServices()) }

func (a *app) playProgram(d *podcastvm.PodcastPage, i int) {
	a.podcasts.PlayProgram(d, i, a.podcastServices())
}

func (a *app) toggleSubscribe(d *podcastvm.PodcastPage) {
	if d.Podcast == nil || d.Subscribing {
		return
	}
	if a.account.Profile == nil {
		a.openLogin()
		return
	}
	a.podcasts.ToggleSubscribe(d, a.podcastServices())
}

func (a *app) loadPodcastTab() {
	if a.podcasts.NeedsHomeLoad() {
		a.podcasts.LoadHome(a.podcastServices())
	}
}

func (a *app) loadHotPodcasts() { a.podcasts.LoadHot(a.podcastServices()) }

func (a *app) podcastRecoveryScope() (id int64, home, subscriptions bool) {
	switch a.nav.Current.Page {
	case pagePodcast:
		id = a.nav.Current.ID
	case pagePodcasts:
		home = true
		subscriptions = a.account.Profile != nil
	case pageUser:
		subscriptions = a.account.Profile != nil && a.listenerID() == a.nav.Current.ID
	}
	return
}

// These load a podcast and its episodes from NetEase, and subscribe to it,
// and the podcasts the tab picks and the most popular; tests replace them.
var (
	podcastDetail    = netease.PodcastDetail
	podcastPrograms  = netease.PodcastPrograms
	subscribePodcast = netease.SubscribePodcast

	recommendedPodcasts = netease.RecommendedPodcasts
	hotPodcasts         = netease.HotPodcasts
)
