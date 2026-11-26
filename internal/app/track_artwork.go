/*
解析本地及网盘歌曲封面，管理匹配结果发布、离线复用和失败恢复。
*/

package app

import (
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	diskcache "netease-music-mygo/internal/cache/disk"
	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/netease"
)

var searchTrackArtwork = netease.Search
var fetchTrackArtwork = netease.Fetch

type trackArtworkKey struct {
	id, stamp, size int64
	temporary       bool
	remote          bool
	listener        int64
	tags            string
}

func localArtworkKey(s localcatalog.Song, temporary bool) trackArtworkKey {
	return trackArtworkKey{id: s.Track.LocalID, stamp: s.Stamp, size: s.Size, temporary: temporary}
}

func (a *app) remoteArtworkKey(t *netease.Track) trackArtworkKey {
	return trackArtworkKey{id: t.ID, remote: true, listener: a.listenerID(), tags: fmt.Sprintf("%s\x00%s\x00%s\x00%d", t.Name, t.ArtistNames(), t.Album.Name, t.Duration)}
}

// trackArtworkURL is shared by lists, the bar, Now Playing and the mini player.
func (a *app) trackArtworkURL(t *netease.Track) string {
	if t.IsLocal() {
		return a.localArtworkURL(t, a.playbackLocalSong(t))
	}
	if url := netease.CoverURL(t.Album.PicURL); url != "" {
		return url
	}
	if len(a.trackArt.known) == 0 {
		return ""
	}
	return a.trackArt.known[a.remoteArtworkKey(t)].url
}

type trackArtworkResult struct {
	url         string
	offlineOnly bool
	failure     error
}

type artworkPublication struct {
	track         *netease.Track
	loading, done bool
	failure       error
}

type trackArtworkState struct {
	known       map[trackArtworkKey]trackArtworkResult
	searching   bool
	publication artworkPublication
}

func (a *app) artworkKey(t *netease.Track) trackArtworkKey {
	if t.IsLocal() {
		return localArtworkKey(a.playbackLocalSong(t), t.Temporary)
	}
	return a.remoteArtworkKey(t)
}

func (a *app) trackArtworkFailure() error {
	t := a.pl.Current()
	if t == nil {
		return nil
	}
	if published := &a.trackArt.publication; published.track == t && published.failure != nil {
		return published.failure
	}
	if len(a.trackArt.known) == 0 {
		return nil
	}
	if t.IsLocal() {
		if a.playbackLocalSong(t).Cover {
			return nil
		}
	} else if netease.CoverURL(t.Album.PicURL) != "" {
		return nil
	}
	return a.trackArt.known[a.artworkKey(t)].failure
}

// Use the page/image recovery scheduler rather than retrying from each build.
func (a *app) retryTrackArtwork(manual bool) {
	err := a.trackArtworkFailure()
	if err == nil || !manual && !netease.RetryableError(err) {
		return
	}
	if published := &a.trackArt.publication; published.track == a.pl.Current() && published.failure != nil {
		*published = artworkPublication{}
	} else {
		delete(a.trackArt.known, a.artworkKey(a.pl.Current()))
	}
	a.syncTrackArtwork()
}

func (a *app) announceTrackArtwork(t *netease.Track, load func() ([]byte, error)) {
	published := &a.trackArt.publication
	if published.track == t && (published.loading || published.done || published.failure != nil) {
		return
	}
	*published = artworkPublication{track: t, loading: true}
	go func() {
		b, err := load()
		if err == nil && len(b) == 0 {
			err = io.ErrUnexpectedEOF
		}
		a.post(func() {
			if published.track != t {
				return
			}
			if a.quitting || a.pl.Current() != t {
				*published = artworkPublication{}
				return
			}
			published.loading, published.failure, published.done = false, err, err == nil
			if err == nil {
				a.pl.ShowArtwork(t, b)
			}
		})
	}()
}

func (a *app) localArtworkURL(t *netease.Track, song localcatalog.Song) string {
	if song.Cover {
		return ""
	}
	return netease.CoverURL(a.trackArt.known[localArtworkKey(song, t.Temporary)].url)
}

// Search only for what plays. Quick skips don't queue a search for every song.
func (a *app) syncTrackArtwork() {
	t := a.pl.Current()
	if a.quitting || t == nil {
		return
	}
	track := *t
	key := a.artworkKey(t)
	if t.IsLocal() {
		song := a.playbackLocalSong(t)
		if song.Path == "" {
			return
		}
		if song.Cover {
			embedded := artcache.LocalEmbeddedKey(song)
			a.announceTrackArtwork(t, func() ([]byte, error) { return a.localView.embeddedArtwork.Get(embedded, localmedia.Cover), nil })
			return
		}
		if track.Name == strings.TrimSuffix(filepath.Base(song.Path), filepath.Ext(song.Path)) {
			track = artcache.FilenameTags(track)
		}
	} else {
		if netease.CoverURL(t.Album.PicURL) != "" {
			return
		}
		if track.Cloud {
			track = artcache.FilenameTags(track)
		}
	}
	state := &a.trackArt
	offline := a.network.offline
	if known, ok := state.known[key]; ok && (known.url != "" || known.failure != nil || offline || !known.offlineOnly) {
		if known.url != "" {
			fetch := fetchTrackArtwork
			a.announceTrackArtwork(t, func() ([]byte, error) {
				return fetch(netease.Sized(known.url, nowPlayingCoverPixels))
			})
		}
		return
	}
	if state.searching {
		return
	}
	state.searching = true
	var cache diskcache.Snapshot
	if !t.Temporary {
		cache = diskcache.Snapshot{Store: a.browseCache, Scope: "local-artwork", Key: fmt.Sprintf("%d/%d/%d", key.id, key.stamp, key.size), Epoch: a.browseCache.Epoch()}
		if key.remote {
			cache.Scope = fmt.Sprintf("track-artwork/%d", key.listener)
			cache.Key = fmt.Sprintf("%d/%x", key.id, sha256.Sum256([]byte(key.tags)))
		}
	}
	search := searchTrackArtwork
	go func() {
		result := trackArtworkResult{offlineOnly: offline}
		cache.Read(&result.url)
		result.url = netease.CoverURL(result.url)
		if result.url == "" && !offline {
			result.url, result.failure = artcache.FindTrack(track, search)
			if result.url != "" {
				cache.Store.Put(cache.Scope, cache.Key, diskcache.Encode(result.url), cache.Epoch)
			}
		}
		a.post(func() {
			state.searching = false
			// A second file open replaces the temporary song, even if it
			// reuses the same catalog ID.
			if !t.Temporary || a.pl.Current() == t {
				if state.known == nil {
					state.known = make(map[trackArtworkKey]trackArtworkResult)
				}
				state.known[key] = result
			}
			// Continue with the latest song after a quick skip. A failed lookup
			// stays pending for recovery instead of immediately retrying itself.
			a.syncTrackArtwork()
		})
	}()
}
