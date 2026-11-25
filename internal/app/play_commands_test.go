/*
验证页面播放命令选择正确的队列、起始歌曲和播放模式。
*/

package app

import (
	"testing"

	"netease-music-mygo/internal/netease"
)

// Bound page actions keep ordered playback, artist shuffle and an explicitly
// chosen daily song distinct when the player already has another shuffle mode.
func TestPagePlaybackActions(t *testing.T) {
	tracks := []netease.Track{{ID: 1, Fee: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	for _, tc := range []struct {
		name        string
		shuffle     bool
		play        func(*app, []netease.Track)
		wantShuffle bool
		wantID      int64
	}{
		{"album", true, func(a *app, tracks []netease.Track) { a.views.album.Play(tracks) }, false, 2},
		{"playlist", true, func(a *app, tracks []netease.Track) { a.views.library.Play(tracks) }, false, 2},
		{"artist ordered", true, func(a *app, tracks []netease.Track) { a.views.artist.Play(tracks, false) }, false, 2},
		{"artist shuffled", false, func(a *app, tracks []netease.Track) { a.views.artist.Play(tracks, true) }, true, 2},
		{"daily ordered", false, func(a *app, tracks []netease.Track) { a.views.daily.Play(tracks, 2) }, false, 3},
		{"daily shuffled", true, func(a *app, tracks []netease.Track) { a.views.daily.Play(tracks, 2) }, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.pl.SuspendCache()
			if tc.shuffle {
				a.pl.ToggleShuffle()
			}
			tc.play(a, tracks)
			if current := a.pl.Current(); current == nil || current.ID != tc.wantID {
				t.Fatalf("current = %+v, want track %d", current, tc.wantID)
			}
			if a.pl.Shuffle() != tc.wantShuffle || len(a.pl.Queue()) != len(tracks) {
				t.Fatalf("shuffle = %v, queue length = %d", a.pl.Shuffle(), len(a.pl.Queue()))
			}
		})
	}
}
