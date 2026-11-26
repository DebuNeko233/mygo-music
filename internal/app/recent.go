/*
接入最近播放记录的加载服务，并协调各记录分类的切换。
*/

package app

import (
	diskcache "netease-music-mygo/internal/features/cache/disk"
	"netease-music-mygo/internal/features/netease"
	recentvm "netease-music-mygo/internal/ui/viewmodel/recent"
)

var (
	recentPlaylists = netease.RecentPlaylists
	recentAlbums    = netease.RecentAlbums
	recentPodcasts  = netease.RecentPodcasts
	recentVoices    = netease.RecentVoices
)

func (a *app) recentServices() recentvm.RecentServices {
	gen := a.account.Generation
	return recentvm.RecentServices{
		SignedIn: a.account.Profile != nil, Post: a.post, Now: a.library.Now,
		Valid:        func() bool { return a.account.Generation == gen },
		Cache:        func() diskcache.Snapshot { return a.cacheRequest("recent") },
		CacheCurrent: func(req diskcache.Snapshot) bool { return a.browseCache == req.Store && req.Store.Epoch() == req.Epoch },
		Songs:        recentPlays, Playlists: recentPlaylists, Albums: recentAlbums, Podcasts: recentPodcasts, Voices: recentVoices,
	}
}

func (a *app) loadRecent() {
	if a.account.Profile != nil && a.recent.NeedsLoad(a.library.Now()) {
		a.recent.Load(a.recentServices())
	}
}

func (a *app) loadRecentTab(tab int) { a.recent.LoadTab(tab, a.recentServices()) }
