package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func trackMatches(tr *netease.Track, terms []string) bool { return viewmodel.TrackMatches(tr, terms) }
func trackIndex(tracks []netease.Track, key int64) int    { return viewmodel.TrackIndex(tracks, key) }

// Keep a queue started from an incomplete cloud listing attached to that load.
// Other sources still use the player's normal snapshot queue.
func (a *app) playTrackQueue(tracks []netease.Track, at int) {
	a.cloud.FollowQueue(0)
	a.pl.PlayQueueFrom(tracks, at, a.playSource())
	if a.route.Page == pageCloud {
		a.cloud.FollowQueue(a.pl.QueueRevision())
	}
}
