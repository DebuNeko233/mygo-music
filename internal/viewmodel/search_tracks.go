package viewmodel

import (
	"fmt"
	"strings"

	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
)

// Keep room for the query and online suggestions below the local matches.
const trackSuggestLimit = 3
const trackSuggestionPrefix = "track-song:"

type TrackSuggestion struct {
	Suggestion
	Song     int64 // Track.Key; local files use a negative identity
	Playable bool
}

// Loaders replace published playlists; removeFrom replaces the track slice.
// In-place track edits must go through Library.BeginEdit, whose revision also
// covers rollback. Don't mutate searchable fields outside those update paths.
type trackSuggestSource struct {
	query    string
	page     navigation.Page
	listener int64
	id       int64
	playlist *netease.Playlist
	first    *netease.Track
	count    int
	revision uint64
}

type trackSuggestState struct {
	source trackSuggestSource
	items  []TrackSuggestion
}

type TrackSearchList struct {
	Tracks   []netease.Track
	Playlist *netease.Playlist
	Revision uint64
	Label    string
	Owner    int64
}

// trackSuggestions shares a bounded, cached scan across playlists, cloud and local music.
func (m *Search) TrackSuggestions(q string, r navigation.Route, listener int64, blocked bool, list TrackSearchList, missing map[int64]bool) []TrackSuggestion {
	s := &m.Suggestions.tracks
	tracks := list.Tracks
	if q == "" || blocked || tracks == nil {
		*s = trackSuggestState{}
		return nil
	}
	source := trackSuggestSource{query: q, page: r.Page, listener: listener, id: r.ID, playlist: list.Playlist, count: len(tracks), revision: list.Revision}
	if len(tracks) > 0 {
		source.first = &tracks[0]
	}
	if source == s.source {
		return s.items
	}
	*s = trackSuggestState{source: source}
	terms := strings.Fields(strings.ToLower(q))
	add := func(tr *netease.Track) {
		s.items = append(s.items, TrackSuggestion{
			Suggestion: Suggestion{
				Key:  fmt.Sprintf("%s%d:%d:%d", trackSuggestionPrefix, r.Page, list.Owner, tr.Key()),
				Name: tr.Name, Detail: tr.ArtistNames(), Kind: list.Label,
			},
			Song: tr.Key(), Playable: TrackAvailable(tr, missing),
		})
	}
	// Disabled matches may fill spare slots, but must never hide playable
	// songs farther down the source list. Only format entries that will show.
	var unavailable [trackSuggestLimit]*netease.Track
	kept := 0
	for i := range tracks {
		tr := &tracks[i]
		if !TrackMatches(tr, terms) {
			continue
		}
		if !TrackAvailable(tr, missing) {
			if kept < len(unavailable) {
				unavailable[kept] = tr
				kept++
			}
			continue
		}
		add(tr)
		if len(s.items) == trackSuggestLimit {
			break
		}
	}
	for _, tr := range unavailable[:min(kept, trackSuggestLimit-len(s.items))] {
		add(tr)
	}
	return s.items
}

// Resolve by stable track key again when chosen: a refresh can reorder or remove songs
// while the popup is open. Never play the old index or another page's list.
func (m *Search) ResolveTrackSuggestion(key, q string, r navigation.Route, listener int64, blocked bool, list TrackSearchList, missing map[int64]bool) (int, bool) {
	if !strings.HasPrefix(key, trackSuggestionPrefix) {
		return -1, false
	}
	s := &m.Suggestions.tracks
	if q != s.source.query || r.Page != s.source.page || listener != s.source.listener || r.ID != s.source.id || blocked {
		return -1, true
	}
	tracks := list.Tracks
	if tracks == nil {
		return -1, true
	}
	for _, sg := range s.items {
		if sg.Key != key {
			continue
		}
		if i := TrackIndex(tracks, sg.Song); i >= 0 && TrackAvailable(&tracks[i], missing) {
			return i, true
		}
		return -1, true
	}
	return -1, true
}

func TrackAvailable(tr *netease.Track, missing map[int64]bool) bool {
	return tr.Playable() && (!tr.IsLocal() || !missing[tr.LocalID])
}

// All words must match, but may span the title, artists, album and aliases.
// Don't join artist/alias strings for every track on each keystroke.
func TrackMatches(tr *netease.Track, terms []string) bool {
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
func TrackIndex(tracks []netease.Track, key int64) int {
	for i := range tracks {
		if tracks[i].Key() == key {
			return i
		}
	}
	return -1
}
