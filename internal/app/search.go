package app

import (
	"strings"

	"netease-music-mygo/internal/netease"
	searchvm "netease-music-mygo/internal/viewmodel/search"
)

func (a *app) searchServices() searchvm.SearchServices {
	return searchvm.SearchServices{Post: a.detailPost(), Fetch: searchResults, Suggest: searchSuggest}
}

func (a *app) suggest(q string) {
	if a.searches.NeedsSuggestions(q) {
		a.searches.Suggest(q, a.searchServices())
	}
}

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
	a.searches.DismissSuggestions()
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

func (a *app) rememberSearch(q string) {
	a.settings.SearchHistory = searchvm.RememberQuery(a.settings.SearchHistory, q)
	a.settings.saveSoon()
}

func (a *app) clearSearchHistory() {
	a.settings.SearchHistory = nil
	a.settings.saveSoon()
}

// choose goes where the option chosen in the search field's popup goes.
func (a *app) choose(key, q string) {
	if a.chooseTrackSuggestion(key, q) {
		return
	}
	if h, ok := strings.CutPrefix(key, "history:"); ok {
		a.searches.Query = h
		a.search(h)
		return
	}
	if key == "query" {
		a.search(q)
		return
	}
	for _, sg := range a.searches.Suggestions.Items {
		if sg.Key != key {
			continue
		}
		if sg.Route.Page == pageSearch {
			a.searches.Query = sg.Route.Query
			a.searchFor(sg.Route)
			return
		}
		a.rememberSearch(q)
		a.searches.DismissSuggestions()
		a.open(sg.Route)
		return
	}
}

func (a *app) searchTracks() searchvm.TrackSearchList {
	switch a.nav.Current.Page {
	case pagePlaylist:
		if pl := a.library.Playlists[a.nav.Current.ID]; pl != nil {
			return searchvm.TrackSearchList{Tracks: pl.Tracks, Playlist: pl, Revision: a.library.Refresh.Revision, Label: "当前歌单", Owner: a.nav.Current.ID}
		}
	case pageCloud:
		if a.account.Profile != nil {
			return searchvm.TrackSearchList{Tracks: a.cloud.Tracks, Revision: a.library.Refresh.Revision, Label: "网盘", Owner: a.listenerID()}
		}
	case pageLocal:
		return searchvm.TrackSearchList{Tracks: a.local.Tracks, Revision: a.local.Revision, Label: "本地"}
	}
	return searchvm.TrackSearchList{}
}

func (a *app) trackSuggestions(q string) []searchvm.TrackSuggestion {
	return a.searches.TrackSuggestions(q, a.nav.Current, a.listenerID(), a.playbackState.Open, a.searchTracks(), a.local.Missing)
}

func (a *app) chooseTrackSuggestion(key, q string) bool {
	list := a.searchTracks()
	i, handled := a.searches.ResolveTrackSuggestion(key, q, a.nav.Current, a.listenerID(), a.playbackState.Open, list, a.local.Missing)
	if i >= 0 {
		a.rememberSearch(q)
		a.searches.DismissSuggestions()
		a.playTrackQueue(list.Tracks, i)
		a.views.tracks.Reveal(i)
	}
	return handled
}

func (a *app) suggestionPlayable(tr *netease.Track) bool {
	return searchvm.TrackAvailable(tr, a.local.Missing)
}
