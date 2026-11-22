package app

import (
	"fmt"
	"strings"

	"netease-music-mygo/internal/netease"
)

// Keep room for the query and online suggestions below the local matches.
const trackSuggestLimit = 3
const trackSuggestionPrefix = "track-song:"

type trackSuggestion struct {
	suggestion
	song     int64 // Track.Key; local files use a negative identity
	playable bool
}

// Loaders replace published playlists; removeFrom replaces the track slice.
// In-place track edits must go through beginLibraryEdit, whose revision also
// covers rollback. Don't mutate searchable fields outside those update paths.
type trackSuggestSource struct {
	query    string
	page     pageKind
	listener int64
	id       int64
	playlist *netease.Playlist
	first    *netease.Track
	count    int
	revision uint64
}

type trackSuggestState struct {
	source trackSuggestSource
	items  []trackSuggestion
}

// trackSearchList gathers the source-specific data once. Matching, caching and
// selection use the same path for playlists, cloud uploads and local files.
type trackSearchList struct {
	tracks   []netease.Track
	playlist *netease.Playlist
	revision uint64
	label    string
	owner    int64
}

func (a *app) searchTracks() trackSearchList {
	switch a.route.page {
	case pagePlaylist:
		if pl := a.playlists[a.route.id]; pl != nil {
			return trackSearchList{tracks: pl.Tracks, playlist: pl, revision: a.listRefresh.revision, label: "当前歌单", owner: a.route.id}
		}
	case pageCloud:
		if a.profile != nil {
			return trackSearchList{tracks: a.cloud.tracks, revision: a.listRefresh.revision, label: "网盘", owner: a.listenerID()}
		}
	case pageLocal:
		return trackSearchList{tracks: a.local.tracks, revision: a.local.revision, label: "本地"}
	}
	return trackSearchList{}
}

// trackSuggestions shares a bounded, cached scan across playlists, cloud and local music.
func (a *app) trackSuggestions(q string) []trackSuggestion {
	s := &a.suggestions.tracks
	list := a.searchTracks()
	tracks := list.tracks
	if q == "" || a.nowPlaying || tracks == nil {
		*s = trackSuggestState{}
		return nil
	}
	source := trackSuggestSource{query: q, page: a.route.page, listener: a.listenerID(), id: a.route.id, playlist: list.playlist, count: len(tracks), revision: list.revision}
	if len(tracks) > 0 {
		source.first = &tracks[0]
	}
	if source == s.source {
		return s.items
	}
	*s = trackSuggestState{source: source}
	terms := strings.Fields(strings.ToLower(q))
	add := func(tr *netease.Track) {
		s.items = append(s.items, trackSuggestion{
			suggestion: suggestion{
				key:  fmt.Sprintf("%s%d:%d:%d", trackSuggestionPrefix, a.route.page, list.owner, tr.Key()),
				name: tr.Name, detail: tr.ArtistNames(), kind: list.label,
			},
			song: tr.Key(), playable: a.suggestionPlayable(tr),
		})
	}
	// Disabled matches may fill spare slots, but must never hide playable
	// songs farther down the source list. Only format entries that will show.
	var unavailable [trackSuggestLimit]*netease.Track
	kept := 0
	for i := range tracks {
		tr := &tracks[i]
		if !trackMatches(tr, terms) {
			continue
		}
		if !a.suggestionPlayable(tr) {
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
func (a *app) chooseTrackSuggestion(key, q string) bool {
	if !strings.HasPrefix(key, trackSuggestionPrefix) {
		return false
	}
	s := &a.suggestions.tracks
	if q != s.source.query || a.route.page != s.source.page || a.listenerID() != s.source.listener || a.route.id != s.source.id || a.nowPlaying {
		return true
	}
	tracks := a.searchTracks().tracks
	if tracks == nil {
		return true
	}
	for _, sg := range s.items {
		if sg.key != key {
			continue
		}
		if i := trackIndex(tracks, sg.song); i >= 0 && a.suggestionPlayable(&tracks[i]) {
			a.rememberSearch(q)
			a.dismissSuggestions()
			a.playTrackQueue(tracks, i)
			a.revealTrack(i)
		}
		return true
	}
	return true
}

// Use the existing availability snapshot; never stat files while typing.
func (a *app) suggestionPlayable(tr *netease.Track) bool {
	return tr.Playable() && (!tr.IsLocal() || !a.local.missing[tr.LocalID])
}
