/*
生成稳定的歌曲行身份，保持列表更新后的滚动位置并选择首个可播歌曲。
*/

package tracklist

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

type trackRowKey struct {
	song       int64
	occurrence int
}

// List updates publish a new track slice when rows change. Cache their
// keys for the rendered list so scrolling and repainting do not scan it.
type trackRowsCache struct {
	first *netease.Track
	keys  []trackRowKey
}

func (a *View) RowKeys(tracks []netease.Track) func(int) any {
	var first *netease.Track
	if len(tracks) != 0 {
		first = &tracks[0]
	}
	rows := &a.State.rows
	if rows.first != first || len(rows.keys) != len(tracks) {
		keys := make([]trackRowKey, len(tracks))
		occurrences := make(map[int64]int, len(tracks))
		for i := range tracks {
			id := tracks[i].Key()
			keys[i] = trackRowKey{id, occurrences[id]}
			occurrences[id]++
		}
		a.keepTrackPlace(rows.keys, keys)
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
func (a *View) keepTrackPlace(old, next []trackRowKey) {
	top, bottom := a.State.List.Visible()
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
			a.State.List.ScrollTo(row, ui.Start)
		}
		return
	}
	for _, visible := range old[top:min(bottom, len(old))] {
		if at := slices.Index(next, visible); at >= 0 {
			a.State.List.ScrollTo(at+1, ui.Start)
			return
		}
	}
}

func FirstPlayable(tracks []netease.Track) int {
	for i := range tracks {
		if tracks[i].Playable() {
			return i
		}
	}
	return 0
}
