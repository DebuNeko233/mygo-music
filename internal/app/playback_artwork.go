package app

import (
	"fmt"
	"image"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/netease"
)

type playbackCover struct {
	pixels           int
	backdrop         bool
	prefetchPrevious bool
}

const barCoverPixels = 120
const nowPlayingCoverPixels = 600

var barCoverRequest = playbackCover{pixels: barCoverPixels, prefetchPrevious: true}
var nowPlayingCoverRequest = playbackCover{pixels: nowPlayingCoverPixels, backdrop: true}

func (a *app) trackArtwork(tr *netease.Track, px int, backdrop bool) *artwork {
	if !tr.IsLocal() {
		return a.art.get(netease.Sized(tr.Album.PicURL, px), backdrop)
	}
	song := a.playbackLocalSong(tr)
	if !song.Cover {
		return a.art.get(netease.Sized(a.localArtworkURL(tr, song), px), backdrop)
	}
	source := song.embeddedArtworkKey()
	key := fmt.Sprintf("local-cover/%d/%d/%d/%s", source.stamp, source.size, px, source.path)
	return a.art.requestCover(key, backdrop, true, func(string) (*ui.Bitmap, image.Image, int, error) {
		return decodeArtSized(a.local.artwork.embedded.get(source, localmedia.Cover), px)
	})
}

func (a *app) trackCover(c *ui.Context, tr *netease.Track, px int, size, radius float32) ui.Element {
	if !tr.IsLocal() {
		return a.cover(c, tr.Album.PicURL, px, size, radius)
	}
	if url := a.localArtworkURL(tr, a.playbackLocalSong(tr)); url != "" {
		return a.cover(c, url, px, size, radius)
	}
	return a.coverImage(c, size, radius, func(ui.Element) *artwork { return a.trackArtwork(tr, px, false) })
}

func (a *app) hasTrackCover(tr *netease.Track) bool {
	if tr.IsLocal() {
		song := a.playbackLocalSong(tr)
		return song.Cover || a.localArtworkURL(tr, song) != ""
	}
	return tr.Album.PicURL != ""
}

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
			a.art.prefetch(key, cover.backdrop)
			seen = key
		}
	}
	return art
}
