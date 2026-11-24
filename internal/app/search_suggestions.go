package app

import (
	"strings"
)

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
		a.dismissSuggestions()
		a.open(sg.Route)
		return
	}
}
