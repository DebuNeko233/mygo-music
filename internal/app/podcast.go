package app

import (
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) podcastServices() viewmodel.PodcastServices {
	return viewmodel.PodcastServices{
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
func (a *app) playPodcast(id int64)  { a.podcasts.RequestPlay(id, a.podcastServices()) }
func (a *app) playProgram(d *viewmodel.PodcastPage, i int) {
	a.podcasts.PlayProgram(d, i, a.podcastServices())
}
func (a *app) toggleSubscribe(d *viewmodel.PodcastPage) {
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
	switch a.route.page {
	case pagePodcast:
		id = a.route.id
	case pagePodcasts:
		home = true
		subscriptions = a.account.Profile != nil
	case pageUser:
		subscriptions = a.account.Profile != nil && a.listenerID() == a.route.id
	}
	return
}
