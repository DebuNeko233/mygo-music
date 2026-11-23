package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func searchPerfApp() *app {
	a := catalogApp()
	r := route{Page: pageSearch, Query: "MyGO", Kind: netease.SearchSongs}
	a.route = r
	songs := make([]netease.Track, 300)
	for i := range songs {
		songs[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("歌曲 %03d", i), Artists: []netease.Artist{{Name: "MyGO!!!!!"}}}
	}
	a.searches.Pages.Set(r, &viewmodel.SearchPage{Res: netease.SearchResult{Songs: songs, Total: 300}, Page: viewmodel.Page{Loaded: true}})
	return a
}

// Rebuilding loaded search results must not trigger network requests or extra state allocation.
func BenchmarkSearchMVVMFrame(b *testing.B) {
	a := searchPerfApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}

// Cached navigation stays allocation-free while preserving page-cache access order.
func BenchmarkSearchMVVMLoaded(b *testing.B) {
	a := searchPerfApp()
	b.ReportAllocs()
	for b.Loop() {
		a.loadSearch(a.route, false)
	}
}
