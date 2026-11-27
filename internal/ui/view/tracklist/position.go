/*
记录歌曲列表的行锚点，返回页面时以歌曲身份恢复，不保留旧歌曲切片或控件。
*/

package tracklist

import (
	"math"
	"slices"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/shell"
)

func (a *View) Position(c *ui.Context) shell.Position {
	s := a.State
	first, last := s.List.Visible()
	if first > 0 && first <= last {
		// Visible includes rows covered by the window toolbar. Restore the
		// reading row below it, rather than pulling hidden rows into view.
		top, _ := a.Insets(c)
		first = min(first+int(math.Ceil(float64(top+shell.PageGap)/rowHeight)), len(s.rows.keys))
	}
	pos := shell.Position{Row: max(0, first)}
	if first > 0 && first <= last && first <= len(s.rows.keys) {
		key := s.rows.keys[first-1]
		pos.Song, pos.Occurrence = key.song, key.occurrence
	}
	return pos
}

func (a *View) restorePosition(tracks []netease.Track) {
	// An error can render only a header and retry footer. Keep the saved
	// song until there are actual rows, rather than consuming it on failure.
	if len(tracks) == 0 {
		return
	}
	pos, ok := a.ScrollState.Pages.Take(true)
	if !ok {
		return
	}
	a.ScrollState.PageScroll.X = pos.X
	if pos.Row == 0 {
		// Preserve partial scrolling within a page header as well.
		a.ScrollState.PageScroll.Y = pos.Y
		return
	}
	row := min(pos.Row, len(tracks))
	if pos.Song != 0 {
		key := trackRowKey{pos.Song, pos.Occurrence}
		if at := slices.Index(a.State.rows.keys, key); at >= 0 {
			row = at + 1
		}
	}
	// A row, rather than the old pixel offset, survives a changed header,
	// window width or rows inserted above it. Missing songs clamp to a neighbor.
	a.State.List.ScrollTo(row, ui.Start)
}
