package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
	"testing"
)

// Loaded navigation only adjusts history and opens existing page state.
func BenchmarkNavigationMVVM(b *testing.B) {
	a := catalogApp()
	a.settings = defaultSettings()
	a.albums.Pages.Set(8, &viewmodel.AlbumPage{Album: &netease.AlbumProfile{Album: netease.Album{ID: 8}}})
	a.open(route{Page: pageCharts})
	a.open(route{Page: pageAlbum, ID: 8})
	b.ReportAllocs()
	for b.Loop() {
		a.back()
		a.goForward()
	}
}
