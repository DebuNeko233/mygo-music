package app

import (
	"netease-music-mygo/internal/netease"
	"time"
)

type keptLists struct{ Mine, Saved []netease.Playlist }

var featuredPlaylists = func() ([]netease.Playlist, error) { return netease.FeaturedPlaylists(30) }
var homeCharts = netease.Charts
var dailySongs = netease.DailySongs
var recommendedPlaylists = netease.RecommendedPlaylists

// Home modules finish independently: a missing daily response must not erase
// yesterday's recommendations, nor delay charts behind a series of timeouts.
func (a *app) loadHome()  { a.loadHomeModules(false) }
func (a *app) retryHome() { a.loadHomeModules(len(a.homeFailures) > 0) }
func (a *app) loadHomeModules(failedOnly bool) {
	if a.homeLoading {
		return
	}
	if !failedOnly {
		a.homeFailures = map[string]bool{}
	}
	a.homeLoading, a.homeErr = true, ""
	a.homePending = 0
	run := func(key string) bool {
		if failedOnly && !a.homeFailures[key] {
			return false
		}
		a.homePending++
		return true
	}
	if run("featured") {
		homeModule(a, "featured", featuredPlaylists, func(v []netease.Playlist, cached bool) {
			if !cached || a.featured == nil {
				a.featured = v
			}
		})
	}
	if run("charts") {
		homeModule(a, "charts", homeCharts, func(v []netease.Playlist, cached bool) {
			if !cached || a.charts == nil {
				a.charts = v
			}
		})
	}
	if a.profile != nil {
		if run("daily") {
			homeModule(a, "daily", dailySongs, func(v []netease.Track, cached bool) {
				if !cached || a.daily == nil {
					a.daily = v
				}
			})
		}
		if run("recommended") {
			homeModule(a, "recommended", recommendedPlaylists, func(v []netease.Playlist, cached bool) {
				if !cached || a.recPlaylists == nil {
					a.recPlaylists = v
				}
			})
		}
	}
	if a.homePending == 0 {
		a.homeLoading = false
	}
}

func homeModule[T any](a *app, key string, fetch func() (T, error), apply func(T, bool)) {
	req, gen := a.cacheRequest(key), a.homeGen
	go func() {
		var kept T
		if at := req.read(&kept); !at.IsZero() {
			a.post(func() {
				if a.homeGen == gen {
					apply(kept, true)
					a.cacheShownAt = at
				}
			})
		}
		value, err := fetch()
		bytes := cacheBytes(value)
		a.post(func() {
			if a.homeGen != gen {
				return
			}
			if err == nil {
				apply(value, false)
				req.keep(bytes)
				delete(a.homeFailures, key)
			} else {
				a.homeFailures[key] = true
			}
			a.homeFailed = len(a.homeFailures) > 0
			a.homePending--
			if a.homePending == 0 {
				a.homeLoading = false
				if !a.homeFailed {
					a.homeAt = time.Now()
				}
				if a.homeFailed && len(a.featured) == 0 && len(a.charts) == 0 {
					a.homeErr = "暂无离线内容，请检查网络后重试"
				}
			}
		})
	}()
}
