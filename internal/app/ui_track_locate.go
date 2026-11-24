package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
)

// trackHeaderKey holds the text sources that can change the header's height.
// Keep values: a rename or refresh can mutate the existing playlist in place.
type trackHeaderKey struct {
	name, description, creator, frequency string
	tracks                                int
	plays                                 float64
}

// trackViewport keeps viewport/target values, never UI elements.
type trackViewport struct {
	y, width, height float32
	playlist, song   int64
	page             pageKind
	index            int
	header           trackHeaderKey
}

// Geometry queries read the committed frame. After a scroll or resize, build
// once more to read the newly laid-out rows, including a small movement that
// changes visibility behind a bar without changing ListState.Visible's range.
// A new target or header text also gets that pass: text wrapping can move
// rows behind a bar even while the scroll offset and list range stay the same.
// No frames are requested while these are unchanged.
func (a *app) syncPlaylistViewport(c *ui.Context, pl *netease.Playlist, at int) {
	a.syncTrackViewport(c, trackHeaderKey{name: pl.Name, description: pl.Description, creator: pl.Creator.Nickname, frequency: pl.UpdateFreq, tracks: len(pl.Tracks), plays: pl.PlayCount}, at)
}

func (a *app) syncTrackViewport(c *ui.Context, header trackHeaderKey, at int) {
	w, h := c.Size()
	var song int64
	if current := a.pl.Current(); current != nil {
		song = current.Key()
	}
	next := trackViewport{
		y: a.shell.PageScroll.Y, width: w, height: h,
		playlist: a.nav.Current.ID, page: a.nav.Current.Page, song: song, index: at,
		header: header,
	}
	if next != a.locateViewport {
		a.locateViewport = next
		c.AnimationFrame()
	}
	// A virtual row may also be built late during layout, e.g. after a refresh.
	a.trackList.Visible()
}

// Resolve the song by ID in the displayed list, not by its queue position:
// shuffle, another queue or a refreshed playlist can all give it another index.
func (a *app) currentTrackIndex(tracks []netease.Track) int {
	current := a.pl.Current()
	if current == nil || current.Key() == 0 {
		return -1
	}
	return trackIndex(tracks, current.Key())
}

// Row zero is the scrolling source header in every supported music list.
func (a *app) revealTrack(at int) { a.trackList.ScrollTo(at+1, ui.Center) }

// trackLocateButton stays above the player bar while the music list scrolls.
func (a *app) trackLocateButton(c *ui.Context, at int, visible bool) {
	if at < 0 {
		return
	}
	a.locatePlayingButton(c, visible, func() { a.revealTrack(at) })
}

func (a *app) locatePlayingButton(c *ui.Context, visible bool, locate func()) {
	if visible {
		return
	}

	p := a.pal
	// The same glass plugin as the page/player bars: blur what lies beneath,
	// follow the theme and honor the system's Reduce Transparency setting.
	b := ui.ButtonBase(c.Key("locate-playing-track")).Absolute().Right(24).Bottom(playerBarHeight+24).
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
