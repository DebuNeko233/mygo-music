package app

import (
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

var featuredPlaylists = func() ([]netease.Playlist, error) { return netease.FeaturedPlaylists(30) }
var homeCharts = netease.Charts
var dailySongs = netease.DailySongs
var recommendedPlaylists = netease.RecommendedPlaylists

func (a *app) homeServices() viewmodel.HomeServices {
	generation := a.account.Generation
	return viewmodel.HomeServices{
		Personal: a.account.Profile != nil, Cache: a.cacheRequest(""), Now: time.Now,
		Featured: featuredPlaylists, Charts: homeCharts, Daily: dailySongs, Recommended: recommendedPlaylists,
		Post: a.post, Valid: func() bool { return a.account.Generation == generation },
	}
}
func (a *app) loadHome() { a.home.Load(a.homeServices()) }
