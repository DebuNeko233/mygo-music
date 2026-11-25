/*
基准测量页面导航模型与应用接线的开销。
*/

package app

import (
	"testing"

	"netease-music-mygo/internal/netease"
	albumvm "netease-music-mygo/internal/viewmodel/album"
)

// Loaded navigation only adjusts history and opens existing page state.
func BenchmarkNavigationMVVM(b *testing.B) {
	a := catalogApp()
	a.settings = defaultSettings()
	a.albums.Pages.Set(8, &albumvm.AlbumPage{Album: &netease.AlbumProfile{Album: netease.Album{ID: 8}}})
	a.open(route{Page: pageCharts})
	a.open(route{Page: pageAlbum, ID: 8})
	b.ReportAllocs()
	for b.Loop() {
		a.back()
		a.goForward()
	}
}
