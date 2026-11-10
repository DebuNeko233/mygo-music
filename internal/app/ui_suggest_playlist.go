package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Keep room for the query and online suggestions below the local matches.
const playlistSuggestLimit = 3

type playlistSuggestion struct {
	suggestion
	song     int64
	playable bool
}

// Loaders replace published playlists; removeFrom replaces the track slice.
// In-place track edits must go through beginLibraryEdit, whose revision also
// covers rollback. Don't mutate searchable fields outside those update paths.
type playlistSuggestSource struct {
	query    string
	id       int64
	playlist *netease.Playlist
	first    *netease.Track
	count    int
	revision uint64
}

type playlistSuggestState struct {
	source playlistSuggestSource
	items  []playlistSuggestion
}

// playlistSuggestions searches the displayed playlist, not the playback queue.
// Cache only this query's few results: animation frames must not rescan a large
// playlist. Refreshes replace the playlist or its tracks; edits bump revision.
func (a *app) playlistSuggestions(q string) []playlistSuggestion {
	s := &a.suggestions.playlist
	pl := a.playlists[a.route.id]
	if q == "" || a.route.page != pagePlaylist || a.nowPlaying || pl == nil {
		*s = playlistSuggestState{}
		return nil
	}
	source := playlistSuggestSource{query: q, id: a.route.id, playlist: pl, count: len(pl.Tracks), revision: a.listRefresh.revision}
	if len(pl.Tracks) > 0 {
		source.first = &pl.Tracks[0]
	}
	if source == s.source {
		return s.items
	}
	*s = playlistSuggestState{source: source}
	terms := strings.Fields(strings.ToLower(q))
	add := func(tr *netease.Track) {
		s.items = append(s.items, playlistSuggestion{
			suggestion: suggestion{
				key:  fmt.Sprintf("playlist-song:%d:%d", a.route.id, tr.ID),
				name: tr.Name, detail: tr.ArtistNames(), kind: "当前歌单",
			},
			song: tr.ID, playable: tr.Playable(),
		})
	}
	// Disabled matches may fill spare slots, but must never hide playable
	// songs farther down the playlist. Only format entries that will show.
	var unavailable [playlistSuggestLimit]*netease.Track
	kept := 0
	for i := range pl.Tracks {
		tr := &pl.Tracks[i]
		if !playlistTrackMatches(tr, terms) {
			continue
		}
		if !tr.Playable() {
			if kept < len(unavailable) {
				unavailable[kept] = tr
				kept++
			}
			continue
		}
		add(tr)
		if len(s.items) == playlistSuggestLimit {
			break
		}
	}
	for _, tr := range unavailable[:min(kept, playlistSuggestLimit-len(s.items))] {
		add(tr)
	}
	return s.items
}

// All words must match, but may span the title, artists, album and aliases.
// Don't join artist/alias strings for every track on each keystroke.
func playlistTrackMatches(tr *netease.Track, terms []string) bool {
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

// Resolve by song ID again when chosen: a refresh can reorder or remove songs
// while the popup is open. Never play the old index or another page's playlist.
func (a *app) choosePlaylistSuggestion(key, q string) bool {
	if !strings.HasPrefix(key, "playlist-song:") {
		return false
	}
	s := &a.suggestions.playlist
	if q != s.source.query || a.route.page != pagePlaylist || a.route.id != s.source.id || a.nowPlaying {
		return true
	}
	pl := a.playlists[a.route.id]
	if pl == nil {
		return true
	}
	for _, sg := range s.items {
		if sg.key != key {
			continue
		}
		for i, tr := range pl.Tracks {
			if tr.ID != sg.song {
				continue
			}
			if tr.Playable() {
				a.rememberSearch(q)
				a.dismissSuggestions()
				a.pl.PlayQueue(pl.Tracks, i)
				a.trackList.ScrollTo(i+1, ui.Center) // Row zero is the playlist header.
			}
			return true
		}
		return true
	}
	return true
}
