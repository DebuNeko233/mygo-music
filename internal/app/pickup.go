package app

import (
	"time"

	"netease-music-mygo/internal/desktop/view/components"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	playbackvm "netease-music-mygo/internal/viewmodel/playback"
)

// Signed in, the listener finds what they played last, on their phone or
// wherever, waiting in the player: paused, with where they heard it under
// its title.

// recentPlays returns the listener's recent plays; tests replace it.
var recentPlays = netease.RecentPlays

// pickUp queues the songs the listener played last, paused, if nothing is
// in the player.
func (a *app) pickUp(uid int64) {
	go func() {
		plays, err := recentPlays(100)
		a.post(func() {
			if err != nil || a.account.Profile == nil || a.account.Profile.UserID != uid || a.pl.Current() != nil {
				return
			}
			songs, last, ok := playbackvm.LastStretch(plays, time.Now())
			if !ok {
				return
			}
			order := make([]int, len(songs))
			for i := range order {
				order[i] = i
			}
			a.pl.Restore(player.Session{Queue: songs, Order: order, At: len(songs) - 1})
			a.picked = playbackview.PickedUp{ID: last.Track.ID, Heard: components.HeardAt(last, time.Now())}
			where := "上次"
			if d := components.DeviceName(last.Device); d != "" {
				where = "在" + d + "上"
			}
			a.toast = "已接上你" + where + "听的《" + last.Track.Name + "》，按空格键继续"
		})
	}()
}
