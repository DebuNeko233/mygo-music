/*
为播放器各视图提供封面请求与相邻歌曲预热，共享封面缓存结果。
*/

package app

import (
	"image"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/localmedia"
	"netease-music-mygo/internal/features/netease"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	"netease-music-mygo/internal/ui/view/components"
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
	embedded := &a.localView.embeddedArtwork
	key := source.ArtworkKey(px)
	return a.art.RequestCover(key, backdrop, true, func(string) (*ui.Bitmap, image.Image, int, error) {
		return artcache.DecodeSized(embedded.Get(source, localmedia.Cover), px)
	})
}

func (a *app) trackCover(c *ui.Context, tr *netease.Track, px int, size, radius float32) ui.Element {
	if !tr.IsLocal() {
		return a.views.artwork.ListCover(c, a.trackArtworkURL(tr), px, size, radius)
	}
	song := a.playbackLocalSong(tr)
	if !song.Cover {
		return a.views.artwork.ListCover(c, a.localArtworkURL(tr, song), px, size, radius)
	}
	source := artcache.LocalEmbeddedKey(song)
	embedded := &a.localView.embeddedArtwork
	return a.coverImage(c, size, radius, func(box ui.Element) *artcache.Image {
		return a.art.RequestListCover(a.nav.Current, source.ArtworkKey(px), box, func(string) (*ui.Bitmap, image.Image, int, error) {
			return artcache.DecodeSized(embedded.Get(source, localmedia.Cover), px)
		})
	}, components.ArtImmediate)
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

func (a *app) coverImage(c *ui.Context, size, radius float32, load func(ui.Element) *artcache.Image, presentation ...components.ArtPresentation) ui.Element {
	return a.views.artwork.Image(c, size, radius, func(el ui.Element) *ui.Bitmap { return components.Bitmap(load(el)) }, presentation...)
}

func viewImage(art *artcache.Image) components.Image {
	if art == nil {
		return nil
	}
	return art
}
