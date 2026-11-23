package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) searchServices() viewmodel.SearchServices {
	return viewmodel.SearchServices{Post: a.detailPost(), Fetch: searchResults, Suggest: searchSuggest}
}
func (a *app) suggest(q string) {
	if a.searches.NeedsSuggestions(q) {
		a.searches.Suggest(q, a.searchServices())
	}
}
func (a *app) dismissSuggestions() { a.searches.DismissSuggestions() }
func suggestionList(res netease.Suggestions) []viewmodel.Suggestion {
	return viewmodel.SuggestionList(res)
}
