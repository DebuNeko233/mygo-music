package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// With NM_FRAMES set, saves the offline account hint and both cache controls
// in light and dark themes, without accessing an account or changing networks.
func TestOfflineFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a, _ := offlineFixture(t)
		a.route = route{page: pagePlaylist, id: 9}
		a.playlists[9] = &netease.Playlist{ID: 9, Name: "离线歌单", Tracks: []netease.Track{{ID: 1, Name: "已缓存的音乐", Artists: []netease.Artist{{Name: "歌手"}}, Duration: 180000}}}
		a.recovery.active, a.recovery.accountPending, a.recovery.accountBusy = true, true, true
		a.network = networkState{known: true, offline: true}
		a.settingsPage = "cache"
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		for _, view := range []struct {
			name string
			view func(*ui.Context)
			w, h int
		}{
			{"offline-", a.view, 1180, 780}, {"offline-settings-", a.settingsView, 780, 820},
		} {
			tt := ui.NewTester(view.view, view.w, view.h)
			tt.SetDark(dark)
			tt.Frame()
			file, err := os.Create(filepath.Join(dir, view.name+suffix+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err = png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
			file.Close()
		}
	}
}

// With NM_FRAMES set, saves settled uncached audio/lyrics failures after a
// system disconnect, rather than a buffering label and animated skeleton.
func TestOfflineUnavailableFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a, posts := offlineFixture(t)
		a.pl = player.New(a.post)
		a.bindOfflineLyrics()
		a.networkChanged(mygo.NetworkStatus{})
		a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "尚未缓存的音乐", Artists: []netease.Artist{{Name: "歌手"}}, Duration: 180000}}, 0)
		for a.pl.Loading() || a.pl.MetadataLoading() {
			refreshPost(t, posts)
		}
		a.nowPlaying, a.npPane = true, paneLyrics
		tt := ui.NewTester(a.view, 1180, 780)
		tt.SetDark(dark)
		tt.Frame()
		tt.Advance(2 * time.Second)
		tt.Frame()
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		file, err := os.Create(filepath.Join(dir, "offline-unavailable-"+suffix+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
}
