package playback

import (
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

// LastStretch returns the songs of the listener's last stretch of
// listening, in the order they played, and the last play of them: on one
// device, each song within half an hour of the next, the last in the past
// week. Songs they may not play are left out, and songs played twice show
// where they played last.
func LastStretch(plays []netease.Play, now time.Time) (songs []netease.Track, last netease.Play, ok bool) {
	if len(plays) == 0 || now.Sub(plays[0].At) > 7*24*time.Hour {
		return nil, netease.Play{}, false
	}
	seen := map[int64]bool{}
	for i, p := range plays {
		if i > 0 && (p.Device != plays[0].Device || plays[i-1].At.Sub(p.At) > 30*time.Minute) || len(songs) == 50 {
			break
		}
		if !p.Track.Playable() || seen[p.Track.ID] {
			continue
		}
		seen[p.Track.ID] = true
		if len(songs) == 0 {
			last = p
		}
		songs = append(songs, p.Track)
	}
	slices.Reverse(songs)
	return songs, last, len(songs) > 0
}
