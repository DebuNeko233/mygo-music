/*
跟踪歌曲列表可见区域，定位当前播放歌曲并协调浮动定位入口。
*/

package tracklist

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
)

// Header holds the text sources that can change the header's height.
// Keep values: a rename or refresh can mutate the existing playlist in place.
type Header struct {
	Name, Description, Creator, Frequency string
	Tracks                                int
	Plays                                 float64
}

// trackViewport keeps viewport/target values, never UI elements.
type trackViewport struct {
	y, width, height float32
	playlist, song   int64
	page             navigation.Page
	index            int
	header           Header
}

// Geometry queries read the committed frame. After a scroll or resize, build
// once more to read the newly laid-out rows, including a small movement that
// changes visibility behind a bar without changing ListState.Visible's range.
// A new target or header text also gets that pass: text wrapping can move
// rows behind a bar even while the scroll offset and list range stay the same.
// No frames are requested while these are unchanged.
func (a *View) SyncPlaylistViewport(c *ui.Context, pl *netease.Playlist, at int) {
	a.SyncViewport(c, Header{Name: pl.Name, Description: pl.Description, Creator: pl.Creator.Nickname, Frequency: pl.UpdateFreq, Tracks: len(pl.Tracks), Plays: pl.PlayCount}, at)
}

func (a *View) SyncViewport(c *ui.Context, header Header, at int) {
	w, h := c.Size()
	var song int64
	if current := a.Current(); current != nil {
		song = current.Key()
	}
	next := trackViewport{
		y: a.ScrollState.PageScroll.Y, width: w, height: h,
		playlist: a.Nav.Current.ID, page: a.Nav.Current.Page, song: song, index: at,
		header: header,
	}
	if next != a.State.viewport {
		a.State.viewport = next
		c.AnimationFrame()
	}
	// A virtual row may also be built late during layout, e.g. after a refresh.
	a.State.List.Visible()
}

// Resolve the song by ID in the displayed list, not by its queue position:
// shuffle, another queue or a refreshed playlist can all give it another index.
func (a *View) CurrentIndex(tracks []netease.Track) int {
	current := a.Current()
	if current == nil || current.Key() == 0 {
		return -1
	}
	for i := range tracks {
		if tracks[i].Key() == current.Key() {
			return i
		}
	}
	return -1
}

// Row zero is the scrolling source header in every supported music list.
func (a *View) Reveal(at int) { a.State.List.ScrollTo(at+1, ui.Center) }

// trackLocateButton stays above the player bar while the music list scrolls.
func (a *View) Locate(c *ui.Context, at int, visible bool) {
	if at < 0 {
		return
	}
	a.LocatePlaying(c, visible, func() { a.Reveal(at) })
}

func (a *View) LocatePlaying(c *ui.Context, visible bool, locate func()) {
	if visible {
		return
	}

	p := *a.Palette
	// The same glass plugin as the page/player bars: blur what lies beneath,
	// follow the theme and honor the system's Reduce Transparency setting.
	b := ui.ButtonBase(c.Key("locate-playing-track")).Absolute().Right(24).Bottom(shell.PlayerBarHeight+24).
		Size(48, 48).Radius(999).Center().Material(glass.Glass{Interactive: true}).Shadow(0, 4, 16, -2, p.CardShade).
		Label("定位正在播放的歌曲").Tooltip("定位正在播放的歌曲")
	color := p.Muted
	if b.Hovered() || b.Pressed() {
		color = p.Text
	}
	b.Children(func() { ui.Icon(c, components.IconLocate).Size(25, 25).TextColor(color) })
	if b.Clicked() {
		// The source callback can reveal a virtual row or fetch missing list
		// pages; the control itself never changes playback.
		locate()
	}
}
