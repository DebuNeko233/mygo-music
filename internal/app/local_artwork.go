package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/netease"
)

var searchLocalArtwork = netease.Search
var fetchLocalArtwork = netease.Fetch

type localArtworkKey struct {
	id, stamp, size int64
	temporary       bool
}

func (s localSong) artworkKey(temporary bool) localArtworkKey {
	return localArtworkKey{s.Track.LocalID, s.Stamp, s.Size, temporary}
}

type localArtworkResult struct {
	url         string
	offlineOnly bool
}

type localArtworkState struct {
	embedded  embeddedArtworkCache
	known     map[localArtworkKey]localArtworkResult
	searching bool
	announced *netease.Track
}

func (a *app) announceLocalArtwork(t *netease.Track, load func() []byte) {
	if a.local.artwork.announced == t {
		return
	}
	a.local.artwork.announced = t
	go func() {
		if b := load(); len(b) > 0 {
			a.post(func() { a.pl.ShowLocalArtwork(t, b) })
		}
	}()
}

func (a *app) localArtworkURL(t *netease.Track, song localSong) string {
	if song.Cover {
		return ""
	}
	return a.local.artwork.known[song.artworkKey(t.Temporary)].url
}

// Search only for what plays. Quick skips don't queue a search for every song.
func (a *app) syncLocalArtwork() {
	t := a.pl.Current()
	if a.quitting || t == nil || !t.IsLocal() {
		return
	}
	song := a.playbackLocalSong(t)
	if song.Path == "" {
		return
	}
	state := &a.local.artwork
	if song.Cover {
		key := song.embeddedArtworkKey()
		a.announceLocalArtwork(t, func() []byte { return state.embedded.get(key, localmedia.Cover) })
		return
	}
	key := song.artworkKey(t.Temporary)
	offline := a.network.offline
	if known, ok := state.known[key]; ok && (known.url != "" || offline || !known.offlineOnly) {
		if known.url != "" {
			fetch := fetchLocalArtwork
			a.announceLocalArtwork(t, func() []byte {
				b, err := fetch(netease.Sized(known.url, nowPlayingCoverPixels))
				if err != nil {
					return nil
				}
				return b
			})
		}
		return
	}
	if state.searching {
		return
	}
	state.searching = true
	var cache cacheRequest
	if !t.Temporary {
		cache = cacheRequest{a.browseCache, "local-artwork", fmt.Sprintf("%d/%d/%d", key.id, key.stamp, key.size), a.browseCache.Epoch()}
	}
	search := searchLocalArtwork
	go func() {
		result := localArtworkResult{offlineOnly: offline}
		cache.read(&result.url)
		if result.url == "" && !offline {
			result.url = findLocalArtwork(song, search)
			if result.url != "" {
				cache.store.Put(cache.scope, cache.key, cacheBytes(result.url), cache.epoch)
			}
		}
		a.post(func() {
			state.searching = false
			// A second file open replaces the temporary song, even if it
			// reuses the same catalog ID.
			if !t.Temporary || a.pl.Current() == t {
				if state.known == nil {
					state.known = make(map[localArtworkKey]localArtworkResult)
				}
				state.known[key] = result
			}
			a.syncLocalArtwork()
		})
	}()
}

func findLocalArtwork(song localSong, search func(string, int) ([]netease.Track, error)) string {
	track := song.Track
	if track.Album.Name == "" && len(track.Artists) == 0 &&
		track.Name == strings.TrimSuffix(filepath.Base(song.Path), filepath.Ext(song.Path)) {
		if artist, title, ok := strings.Cut(track.Name, " - "); ok {
			track.Name = title
			track.Artists = []netease.Artist{{Name: artist}}
		}
	}
	if !canMatchArtwork(&track) {
		return ""
	}
	query := localTrackQuery(&track)
	if track.ArtistNames() == "" {
		query = strings.TrimSpace(query + " " + track.Album.Name)
	}
	candidates, err := search(query, 20)
	if err != nil {
		return ""
	}
	return chooseLocalArtwork(&track, candidates)
}

func localTrackQuery(t *netease.Track) string {
	return strings.TrimSpace(t.Name + " " + t.ArtistNames())
}

func artworkTag(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, norm.NFKC.String(s))
}

func canMatchArtwork(track *netease.Track) bool {
	return track.Duration > 0 && artworkTag(track.Name) != "" &&
		(artworkTag(track.ArtistNames()) != "" || artworkTag(track.Album.Name) != "")
}

// Require matching tags and a close duration. If two nearly equally close
// versions have different art, keep the placeholder.
func chooseLocalArtwork(local *netease.Track, candidates []netease.Track) string {
	if !canMatchArtwork(local) {
		return ""
	}
	title, artist, album := artworkTag(local.Name), artworkTag(local.ArtistNames()), artworkTag(local.Album.Name)
	const tolerance = 5000
	best, other := int64(tolerance+1000), int64(tolerance+1000)
	url := ""
	for _, t := range candidates {
		if t.Album.PicURL == "" || t.Duration <= 0 || artworkTag(t.Name) != title ||
			artist != "" && artworkTag(t.ArtistNames()) != artist || album != "" && artworkTag(t.Album.Name) != album {
			continue
		}
		diff := max(t.Duration-local.Duration, local.Duration-t.Duration)
		if diff > tolerance {
			continue
		}
		if t.Album.PicURL == url {
			best = min(best, diff)
		} else if diff < best {
			best, other, url = diff, best, t.Album.PicURL
		} else {
			other = min(other, diff)
		}
	}
	if other-best < 1000 {
		return ""
	}
	return url
}
