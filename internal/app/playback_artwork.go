package app

import "netease-music-mygo/internal/netease"

type playbackCover struct {
	pixels           int
	backdrop         bool
	prefetchPrevious bool
}

const barCoverPixels = 120
const nowPlayingCoverPixels = 600

var barCoverRequest = playbackCover{pixels: barCoverPixels, prefetchPrevious: true}
var nowPlayingCoverRequest = playbackCover{pixels: nowPlayingCoverPixels, backdrop: true}

// playbackArtwork requests the visible cover first, then warms the exact
// cache entries this view will use after a skip.
func (a *app) playbackArtwork(url string, cover playbackCover) *artwork {
	current := netease.Sized(url, cover.pixels)
	art := a.art.get(current, cover.backdrop)
	if a.network.offline {
		return art
	}
	next, prev := a.pl.AdjacentTracks()
	if !cover.prefetchPrevious {
		prev = nil
	}
	seen := current
	for _, tr := range [...]*netease.Track{next, prev} {
		if tr == nil || tr.Album.PicURL == "" {
			continue
		}
		key := netease.Sized(tr.Album.PicURL, cover.pixels)
		if key != current && key != seen {
			a.art.get(key, cover.backdrop)
			seen = key
		}
	}
	return art
}
