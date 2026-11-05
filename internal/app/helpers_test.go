package app

import (
	"testing"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// isolateUserData keeps what the app writes, the settings, the session and
// the transcripts, in a directory of the test's: it wrote them to the real
// one, which fails on a machine where that cannot be written.
func isolateUserData(t *testing.T) {
	t.Helper()
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
}

func newTestApp() *app {
	a := &app{
		playlists: map[int64]*netease.Playlist{}, loadingPl: map[int64]bool{}, plPlay: map[int64]bool{}, plErr: map[int64]string{},
		artists: map[int64]*artistData{}, albums: map[int64]*albumData{}, users: map[int64]*userData{},
		podcasts: map[int64]*podcastData{}, podcastTab: &podcastTab{}, searches: map[route]*searchData{},
		lyricAt: -1, queueAt: -1,
	}
	a.art = newArtCache(func(func()) {})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}
