/*
测量详情视图与模型访问开销，并验证已加载页面的空闲帧预算。
*/

package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	artistvm "netease-music-mygo/internal/viewmodel/artist"
)

func detailPerfApp(scene string) *app {
	a := catalogApp()
	songs := make([]netease.Track, 8000)
	for i := range songs {
		songs[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("歌曲 %04d", i+1), Duration: 180000}
	}
	d := a.artists.Pages.Get(7)
	d.Profile.Songs = songs
	d.Latest = songs[:100]
	a.albums.Pages.Get(8).Album.Songs = songs
	switch scene {
	case "artist-hot":
		a.nav.Current = route{Page: pageArtist, ID: 7}
		a.artists.Tab = artistvm.ArtistHot
	case "artist-latest":
		a.nav.Current = route{Page: pageArtist, ID: 7}
		a.artists.Tab = artistvm.ArtistLatest
	case "artist-albums":
		a.nav.Current = route{Page: pageArtist, ID: 7}
		a.artists.Tab = artistvm.ArtistAlbums
	case "artist-events":
		a.nav.Current = route{Page: pageArtist, ID: 7}
		a.artists.Tab = artistvm.ArtistEvents
	case "album":
		a.nav.Current = route{Page: pageAlbum, ID: 8}
	case "user":
		a.nav.Current = route{Page: pageUser, ID: 9}
	}
	return a
}

// Detail views borrow published collections even when the underlying playlist contains 8000 songs.
func BenchmarkDetailViewFrame(b *testing.B) {
	for _, scene := range []string{"artist-hot", "artist-latest", "artist-albums", "artist-events", "album", "user"} {
		b.Run(scene, func(b *testing.B) {
			a := detailPerfApp(scene)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Reopening loaded details must not schedule network/disk jobs or allocate request adapters.
func BenchmarkDetailLoadedAccess(b *testing.B) {
	a := detailPerfApp("album")
	b.ReportAllocs()
	for b.Loop() {
		a.loadAlbum(8)
		a.loadArtist(7)
		a.loadUser(9)
	}
}

// Fully loaded detail pages must sleep without polling or repeatedly requesting redraws.
func TestDetailMVVMIdleFrameBudget(t *testing.T) {
	for _, scene := range []string{"artist-hot", "artist-latest", "artist-albums", "artist-events", "album", "user"} {
		t.Run(scene, func(t *testing.T) {
			a := detailPerfApp(scene)
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			before := builds
			tt.Advance(2 * time.Second)
			if builds != before {
				t.Fatalf("idle detail requested %d extra builds", builds-before)
			}
		})
	}
}
