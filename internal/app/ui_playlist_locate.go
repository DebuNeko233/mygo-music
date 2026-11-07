package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// playlistViewport keeps viewport/target values, never UI elements.
type playlistViewport struct {
	y, width, height float32
	playlist, song   int64
	index            int
}

// Geometry queries read the committed frame. After a scroll or resize, build
// once more to read the newly laid-out rows, including a small movement that
// changes visibility behind a bar without changing ListState.Visible's range.
// A new playlist/song/index also gets that pass, so refreshed rows cannot
// reuse stale geometry. No frames are requested while these are unchanged.
func (a *app) syncPlaylistViewport(c *ui.Context, at int) {
	w, h := c.Size()
	var song int64
	if current := a.pl.Current(); current != nil {
		song = current.ID
	}
	next := playlistViewport{a.pageScroll.Y, w, h, a.route.id, song, at}
	if next != a.locateViewport {
		a.locateViewport = next
		c.AnimationFrame()
	}
	// A virtual row may also be built late during layout, e.g. after a refresh.
	a.trackList.Visible()
}

// Resolve the song by ID in the displayed list, not by its queue position:
// shuffle, another queue or a refreshed playlist can all give it another index.
func (a *app) playlistCurrentIndex(tracks []netease.Track) int {
	current := a.pl.Current()
	if current == nil || current.ID <= 0 {
		return -1
	}
	for i := range tracks {
		if tracks[i].ID == current.ID {
			return i
		}
	}
	return -1
}

// playlistLocateButton stays above the player bar while the playlist scrolls.
func (a *app) playlistLocateButton(c *ui.Context, at int, visible bool) {
	if visible || at < 0 {
		return
	}

	p := a.pal
	// The same glass plugin as the page/player bars: blur what lies beneath,
	// follow the theme and honor the system's Reduce Transparency setting.
	b := ui.ButtonBase(c.Key("locate-playing-track")).Absolute().Right(24).Bottom(playerBarHeight+24).
		Size(48, 48).Radius(999).Center().Material(glass.Glass{Interactive: true}).Shadow(0, 4, 16, -2, p.cardShade).
		Label("定位正在播放的歌曲").Tooltip("定位正在播放的歌曲")
	color := p.muted
	if b.Hovered() || b.Pressed() {
		color = p.text
	}
	b.Children(func() { ui.Icon(c, iconLocate).Size(25, 25).TextColor(color) })
	if b.Clicked() {
		// Row zero is the scrolling playlist header. ScrollTo can locate an
		// unbuilt virtual row, without loading songs or changing playback.
		a.trackList.ScrollTo(at+1, ui.Center)
	}
}
