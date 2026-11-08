package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
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
