package app

import (
	"fmt"
	"image"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/netease"
)

type playbackCover struct {
	pixels           int
	backdrop         bool
	prefetchPrevious bool
}

const barCoverPixels = components.BarCoverPixels
const nowPlayingCoverPixels = components.NowPlayingCoverPixels

var barCoverRequest = playbackCover{pixels: barCoverPixels, prefetchPrevious: true}
var nowPlayingCoverRequest = playbackCover{pixels: nowPlayingCoverPixels, backdrop: true}

func (a *app) trackArtwork(tr *netease.Track, px int, backdrop bool) *artcache.Image {
	if !tr.IsLocal() {
		return a.art.Get(netease.Sized(a.trackArtworkURL(tr), px), backdrop)
	}
	song := a.playbackLocalSong(tr)
	if !song.Cover {
		return a.art.Get(netease.Sized(a.localArtworkURL(tr, song), px), backdrop)
	}
	source := artcache.LocalEmbeddedKey(song)
	key := fmt.Sprintf("local-cover/%d/%d/%d/%s", source.Stamp, source.Size, px, source.Path)
	return a.art.RequestCover(key, backdrop, true, func(string) (*ui.Bitmap, image.Image, int, error) {
		return artcache.DecodeSized(a.localView.embeddedArtwork.Get(source, localmedia.Cover), px)
	})
}

func (a *app) trackCover(c *ui.Context, tr *netease.Track, px int, size, radius float32) ui.Element {
	if !tr.IsLocal() {
		return a.views.artwork.Cover(c, a.trackArtworkURL(tr), px, size, radius)
	}
	if url := a.localArtworkURL(tr, a.playbackLocalSong(tr)); url != "" {
		return a.views.artwork.Cover(c, url, px, size, radius)
	}
	return a.coverImage(c, size, radius, func(ui.Element) *artcache.Image { return a.trackArtwork(tr, px, false) })
}

func (a *app) hasTrackCover(tr *netease.Track) bool {
	if tr.IsLocal() {
		song := a.playbackLocalSong(tr)
		return song.Cover || a.localArtworkURL(tr, song) != ""
	}
	return a.trackArtworkURL(tr) != ""
}

// playbackTrackArtwork is the common request path for the bar and Now Playing.
func (a *app) playbackTrackArtwork(tr *netease.Track, cover playbackCover) *artcache.Image {
	if tr.IsLocal() {
		return a.trackArtwork(tr, cover.pixels, cover.backdrop)
	}
	return a.playbackArtwork(a.trackArtworkURL(tr), cover)
}

// playbackArtwork requests the visible cover first, then warms the exact
// cache entries this view will use after a skip.
func (a *app) playbackArtwork(url string, cover playbackCover) *artcache.Image {
	current := netease.Sized(url, cover.pixels)
	art := a.art.Get(current, cover.backdrop)
	if a.network.offline {
		return art
	}
	next, prev := a.pl.AdjacentTracks()
	if !cover.prefetchPrevious {
		prev = nil
	}
	seen := current
	for _, tr := range [...]*netease.Track{next, prev} {
		if tr == nil {
			continue
		}
		url := a.trackArtworkURL(tr)
		if url == "" {
			continue
		}
		key := netease.Sized(url, cover.pixels)
		if key != current && key != seen {
			a.art.Prefetch(key, cover.backdrop)
			seen = key
		}
	}
	return art
}

func (a *app) coverImage(c *ui.Context, size, radius float32, load func(ui.Element) *artcache.Image) ui.Element {
	return a.views.artwork.Image(c, size, radius, func(el ui.Element) *ui.Bitmap { return components.Bitmap(load(el)) })
}

func viewImage(art *artcache.Image) components.Image {
	if art == nil {
		return nil
	}
	return art
}
