package app

import (
	"strings"

	"netease-music-mygo/internal/netease"
)

// All words must match, but may span the title, artists, album and aliases.
// Don't join artist/alias strings for every track on each keystroke.
func trackMatches(tr *netease.Track, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(strings.ToLower(tr.Name), term) || strings.Contains(strings.ToLower(tr.Album.Name), term) {
			continue
		}
		found := false
		for _, ar := range tr.Artists {
			if strings.Contains(strings.ToLower(ar.Name), term) {
				found = true
				break
			}
		}
		for _, alias := range tr.Alias {
			if found || strings.Contains(strings.ToLower(alias), term) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// trackIndex resolves an item in the displayed source, never its queue order.
func trackIndex(tracks []netease.Track, key int64) int {
	for i := range tracks {
		if tracks[i].Key() == key {
			return i
		}
	}
	return -1
}

// Keep a queue started from an incomplete cloud listing attached to that load.
// Other sources still use the player's normal snapshot queue.
func (a *app) playTrackQueue(tracks []netease.Track, at int) {
	a.cloud.queue = 0
	a.pl.PlayQueueFrom(tracks, at, a.playSource())
	if a.route.page == pageCloud && a.cloud.more && a.cloud.pending == nil {
		a.cloud.queue = a.pl.QueueRevision()
	}
}
