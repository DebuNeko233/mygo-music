package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	desktopview "netease-music-mygo/internal/desktop/view"
	"netease-music-mygo/internal/netease"
	recentvm "netease-music-mygo/internal/viewmodel/recent"
)

func recentPerfApp(count int) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.account.Profile = &netease.Profile{UserID: 7, Nickname: "小明"}
	a.nav.Current = route{Page: pageRecent}
	for i := range a.recent.Tabs {
		a.recent.Tabs[i] = recentvm.RecentTab{Shown: true, At: time.Now().Add(time.Hour)}
	}
	plays := make([]netease.Play, count)
	for i := range plays {
		plays[i] = netease.Play{Track: netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("最近歌曲 %03d", i+1), Artists: []netease.Artist{{Name: "MyGO!!!!!"}}, Duration: 180000}, At: time.Now().Add(-3 * time.Minute), Device: "iphone"}
	}
	a.recent.SetSongs(plays)
	for i := range count {
		a.recent.Playlists = append(a.recent.Playlists, netease.Played[netease.Playlist]{Item: netease.Playlist{ID: int64(i + 1), Name: fmt.Sprintf("最近歌单 %03d", i+1)}})
		a.recent.Albums = append(a.recent.Albums, netease.Played[netease.AlbumProfile]{Item: netease.AlbumProfile{Album: netease.Album{ID: int64(i + 1), Name: fmt.Sprintf("最近专辑 %03d", i+1)}}})
		a.recent.Podcasts = append(a.recent.Podcasts, netease.Played[netease.Podcast]{Item: netease.Podcast{ID: int64(i + 1), Name: fmt.Sprintf("最近播客 %03d", i+1)}})
		a.recent.Voices = append(a.recent.Voices, netease.Played[netease.Voice]{Item: netease.Voice{Program: netease.Program{ID: int64(i + 1), Name: fmt.Sprintf("最近声音 %03d", i+1), Duration: 180000}, Podcast: netease.Podcast{ID: 66, Name: "MyGO Radio"}}, At: time.Now().Add(-3 * time.Minute), Device: "web"})
	}
	a.recent.SetAlbums(a.recent.Albums)
	a.loadRecent()
	return a
}

// Rendering a recent tab measures stable view builds, including any derived data allocated each frame.
func BenchmarkRecentViewFrame(b *testing.B) {
	for tab, name := range desktopview.RecentTabTitles {
		b.Run(name, func(b *testing.B) {
			a := recentPerfApp(300)
			a.recent.Selected = tab
			tt := ui.NewTester(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.views.recent.Build(c) }, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Resting recent tabs should sleep without polling while their snapshots are fresh.
func TestRecentIdleFrameBudget(t *testing.T) {
	for tab, name := range desktopview.RecentTabTitles {
		t.Run(name, func(t *testing.T) {
			a := recentPerfApp(10)
			a.recent.Selected = tab
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) { builds++; a.pal = paletteFor(c.Theme()); a.views.recent.Build(c) }, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			before := builds
			tt.Advance(2 * time.Second)
			if builds != before {
				t.Fatalf("idle recent tab requested %d extra builds", builds-before)
			}
		})
	}
}

// All five recent tabs and their signed-out/loading/error states can be inspected in both themes.
func TestRecentMVVMFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save recent surfaces")
	}
	for _, scene := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8} {
		for _, dark := range []bool{false, true} {
			a := recentPerfApp(6)
			if scene < recentvm.RecentTabCount {
				a.recent.Selected = scene
			}
			switch scene {
			case 5:
				a.account.Profile = nil
			case 6:
				a.recent.Tabs[0].Shown = false
				a.recent.Tabs[0].Loading = true
			case 7:
				a.recent.Tabs[0].Shown = false
				a.recent.Tabs[0].Failure = netease.ErrOffline
			case 8:
				a.recent.SetSongs(nil)
			}
			tt := ui.NewTesterWith(func(c *ui.Context) { a.pal = paletteFor(c.Theme()); a.views.recent.Build(c) }, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}, Dark: dark})
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("recent-scene=%d-dark=%v.png", scene, dark)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Request orchestration revalidates five records with existing providers, excluding network/disk and view rendering.
func BenchmarkRecentLoad(b *testing.B) {
	a := newTestApp()
	a.account.Profile = &netease.Profile{UserID: 7}
	posts := testPosts(a)
	oldSongs, oldLists, oldAlbums, oldPodcasts, oldVoices := recentPlays, recentPlaylists, recentAlbums, recentPodcasts, recentVoices
	b.Cleanup(func() {
		recentPlays, recentPlaylists, recentAlbums, recentPodcasts, recentVoices = oldSongs, oldLists, oldAlbums, oldPodcasts, oldVoices
	})
	songs := []netease.Play{{Track: netease.Track{ID: 1}}}
	albums := []netease.Played[netease.AlbumProfile]{{Item: netease.AlbumProfile{Album: netease.Album{ID: 3}}}}
	recentPlays = func(int) ([]netease.Play, error) { return songs, nil }
	recentPlaylists = func(int) ([]netease.Played[netease.Playlist], error) { return nil, nil }
	recentAlbums = func(int) ([]netease.Played[netease.AlbumProfile], error) { return albums, nil }
	recentPodcasts = func(int) ([]netease.Played[netease.Podcast], error) { return nil, nil }
	recentVoices = func(int) ([]netease.Played[netease.Voice], error) { return nil, nil }
	b.ReportAllocs()
	for b.Loop() {
		for i := range a.recent.Tabs {
			a.recent.Tabs[i].At = time.Time{}
		}
		a.loadRecent()
		for a.recent.Loading() {
			(<-posts)()
		}
	}
}
