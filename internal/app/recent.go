package app

import (
	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

var (
	recentPlaylists = netease.RecentPlaylists
	recentAlbums    = netease.RecentAlbums
	recentPodcasts  = netease.RecentPodcasts
	recentVoices    = netease.RecentVoices
)

func (a *app) recentServices() viewmodel.RecentServices {
	gen := a.account.Generation
	return viewmodel.RecentServices{
		SignedIn: a.account.Profile != nil, Post: a.post, Now: a.listsNow,
		Valid:        func() bool { return a.account.Generation == gen },
		Cache:        func() diskcache.Snapshot { return a.cacheRequest("recent") },
		CacheCurrent: func(req diskcache.Snapshot) bool { return a.browseCache == req.Store && req.Store.Epoch() == req.Epoch },
		Songs:        recentPlays, Playlists: recentPlaylists, Albums: recentAlbums, Podcasts: recentPodcasts, Voices: recentVoices,
	}
}
func (a *app) loadRecent() {
	if a.account.Profile != nil && a.recent.NeedsLoad(a.listsNow()) {
		a.recent.Load(a.recentServices())
	}
}
func (a *app) loadRecentTab(tab int) { a.recent.LoadTab(tab, a.recentServices()) }
