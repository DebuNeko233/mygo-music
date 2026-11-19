package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

type playlistRowKey struct {
	song       int64
	occurrence int
}

// Playlist updates publish a new track slice when rows change. Cache their
// keys for the rendered list so scrolling and repainting do not scan it.
type playlistRows struct {
	first *netease.Track
	keys  []playlistRowKey
}

func (a *app) playlistRowKeys(tracks []netease.Track) func(int) any {
	var first *netease.Track
	if len(tracks) != 0 {
		first = &tracks[0]
	}
	rows := &a.playlistRows
	if rows.first != first || len(rows.keys) != len(tracks) {
		keys := make([]playlistRowKey, len(tracks))
		occurrences := make(map[int64]int, len(tracks))
		for i := range tracks {
			id := tracks[i].Key()
			keys[i] = playlistRowKey{id, occurrences[id]}
			occurrences[id]++
		}
		a.keepPlaylistPlace(rows.keys, keys)
		rows.first, rows.keys = first, keys
	}
	keys := rows.keys
	return func(row int) any {
		if row == 0 || row > len(keys) {
			return row // header or footer
		}
		return keys[row-1]
	}
}

// MyGo's keyed list follows the top row with its exact inset when the row
// survives and moves within its search range. For a larger move, or if the
// top row was deleted, explicitly place it or the next surviving visible row.
func (a *app) keepPlaylistPlace(old, next []playlistRowKey) {
	top, bottom := a.trackList.Visible()
	if top <= 0 || top > len(old) || top > bottom {
		return
	}
	key := old[top-1]
	if top <= len(next) && next[top-1] == key {
		return
	}
	if at := slices.Index(next, key); at >= 0 {
		const mygoAnchorSearch = 1000 // ListState.find in the pinned MyGo version
		row := at + 1
		if row < top-mygoAnchorSearch || row > top+mygoAnchorSearch {
			a.trackList.ScrollTo(row, ui.Start)
		}
		return
	}
	for _, visible := range old[top:min(bottom, len(old))] {
		if at := slices.Index(next, visible); at >= 0 {
			a.trackList.ScrollTo(at+1, ui.Start)
			return
		}
	}
}
