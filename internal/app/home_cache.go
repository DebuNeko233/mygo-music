package app

import (
	"time"

	"netease-music-mygo/internal/netease"
	homevm "netease-music-mygo/internal/viewmodel/home"
)

var featuredPlaylists = func() ([]netease.Playlist, error) { return netease.FeaturedPlaylists(30) }

var homeCharts = netease.Charts

var dailySongs = netease.DailySongs

var recommendedPlaylists = netease.RecommendedPlaylists

func (a *app) homeServices() homevm.HomeServices {
	generation := a.account.Generation
	return homevm.HomeServices{
		Personal: a.account.Profile != nil, Cache: a.cacheRequest(""), Now: time.Now,
		Featured: featuredPlaylists, Charts: homeCharts, Daily: dailySongs, Recommended: recommendedPlaylists,
		Post: a.post, Valid: func() bool { return a.account.Generation == generation },
	}
}

func (a *app) loadHome() { a.home.Load(a.homeServices()) }
