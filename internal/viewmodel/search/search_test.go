package search

import (
	"errors"
	"testing"

	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Refresh and account reset discard earlier pages even when the same query returns last.
func TestSearchReplacementRejectsOldPage(t *testing.T) {
	for _, reset := range []bool{false, true} {
		m := new(Search)
		m.Reset()
		posts := make(chan func(), 4)
		s := SearchServices{Post: func(f func()) { posts <- f }, Fetch: func(string, netease.SearchType, int, int) (netease.SearchResult, error) {
			return netease.SearchResult{Total: 1, Songs: []netease.Track{{ID: 7}}}, nil
		}}
		r := navigation.Route{Page: navigation.Search, Query: "a", Kind: netease.SearchSongs}
		m.Load(r, false, s)
		old := testutil.Wait(t, posts, "first search")
		if reset {
			m.Reset()
		}
		m.Load(r, true, s)
		testutil.Wait(t, posts, "replacement search")()
		fresh := m.Pages.Peek(r)
		old()
		if m.Pages.Peek(r) != fresh || len(fresh.Res.Songs) != 1 || fresh.Page.Loading {
			t.Fatal("old result changed replacement")
		}
	}
}

// Duplicate page requests coalesce; a failed page retries at the unchanged offset.
func TestSearchPagingRetryPreservesResults(t *testing.T) {
	m := new(Search)
	m.Reset()
	posts := make(chan func(), 4)
	offsets := make(chan int, 4)
	errFetch := errors.New("offline")
	s := SearchServices{Post: func(f func()) { posts <- f }, Fetch: func(_ string, _ netease.SearchType, _ int, offset int) (netease.SearchResult, error) {
		offsets <- offset
		return netease.SearchResult{}, errFetch
	}}
	r := navigation.Route{Page: navigation.Search, Query: "a", Kind: netease.SearchSongs}
	d := &SearchPage{Res: netease.SearchResult{Total: 3, Songs: []netease.Track{{ID: 1}}}, Page: viewmodel.Page{Loaded: true, More: true}}
	m.Pages.Set(r, d)
	m.LoadMore(r, d, s)
	m.LoadMore(r, d, s)
	testutil.Wait(t, posts, "failed page")()
	if len(d.Res.Songs) != 1 || !d.Page.Loaded || d.Page.Failure == nil {
		t.Fatal("failed page lost existing results")
	}
	s.Fetch = func(_ string, _ netease.SearchType, _ int, offset int) (netease.SearchResult, error) {
		offsets <- offset
		return netease.SearchResult{Total: 3, Songs: []netease.Track{{ID: 2}}}, nil
	}
	m.LoadMore(r, d, s)
	testutil.Wait(t, posts, "retry")()
	if len(offsets) != 2 || <-offsets != 1 || <-offsets != 1 || len(d.Res.Songs) != 2 || d.Page.Failure != nil || !d.Page.More {
		t.Fatal("retry duplicated requests or skipped results")
	}
}
