package app

import (
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

func (a *app) dismissSuggestions() { a.searches.DismissSuggestions() }

func suggestionList(res netease.Suggestions) []searchvm.Suggestion {
	return searchvm.SuggestionList(res)
}
