package app

import (
	"strings"

	"netease-music-mygo/internal/netease"
	searchvm "netease-music-mygo/internal/viewmodel/search"
)

// searchResults and searchSuggest search NetEase; tests replace them.
var (
	searchResults = netease.SearchPage
	searchSuggest = netease.SearchSuggest
)

// search searches for q, of the kind last searched for.
func (a *app) search(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	if a.openMusicInput(q) {
		return
	}
	a.cancelMusicLink()
	kind := a.searches.Last.Kind
	if kind == 0 {
		kind = netease.SearchSongs
	}
	a.searchFor(route{Page: pageSearch, Query: q, Kind: kind})
}

// searchFor opens the page of a search, remembering its query. Searched
// for again, it loads anew.
func (a *app) searchFor(r route) {
	a.rememberSearch(r.Query)
	a.dismissSuggestions()
	if r == a.nav.Current {
		a.playbackState.Open = false
		a.loadSearch(r, true)
		return
	}
	a.open(r)
}

func (a *app) loadSearch(r route, again bool) {
	if a.searches.NeedsLoad(r, again) {
		a.searches.Load(r, again, a.searchServices())
	} else {
		a.searches.Pages.Get(r)
	}
}

func (a *app) moreResults(r route, d *searchvm.SearchPage) {
	a.searches.LoadMore(r, d, a.searchServices())
}

// searchHistoryLimit is how many searches the history keeps.
const searchHistoryLimit = 20

// rememberSearch puts q first in the history of searches, once.
func (a *app) rememberSearch(q string) {
	history := []string{q}
	for _, s := range a.settings.SearchHistory {
		if s != q && len(history) < searchHistoryLimit {
			history = append(history, s)
		}
	}
	a.settings.SearchHistory = history
	a.settings.saveSoon()
}

func (a *app) clearSearchHistory() {
	a.settings.SearchHistory = nil
	a.settings.saveSoon()
}
