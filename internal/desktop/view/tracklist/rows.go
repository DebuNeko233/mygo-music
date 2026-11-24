package tracklist

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
	libraryvm "netease-music-mygo/internal/viewmodel/library"
	localvm "netease-music-mygo/internal/viewmodel/local"
)

type State struct {
	List     ui.ListState
	rows     trackRowsCache
	viewport trackViewport
}

func (s *State) InvalidateRows() { s.rows = trackRowsCache{} }

type View struct {
	*page.Page
	State       *State
	Local       *localvm.LocalLibrary
	Library     *libraryvm.Library
	Current     func() *netease.Track
	LocalItems  func(*netease.Track, bool) func(*ui.Menu)
	SongItems   func(*netease.Track, int64) func(*ui.Menu)
	PlayingBars func(*ui.Context, ...ui.Color)
	Cover       func(*ui.Context, *netease.Track, int, float32, float32) ui.Element
	ArtistLinks func(*ui.Context, *netease.Track, ui.Color, ui.Color, bool) ui.Element
	Like        func(*ui.Context, *netease.Track, float32, ui.Color) ui.Element
	Relink      func(int64)
	Play        func([]netease.Track, int)
}

// trackRows lists tracks under a header, which scrolls with them; a click
// plays the list from the track clicked.
func (a *View) Rows(c *ui.Context, tracks []netease.Track, covers bool, header func(c *ui.Context), footer ...func(c *ui.Context)) (playingVisible bool) {
	return a.RowsMeta(c, tracks, covers, nil, header, footer...)
}

// trackRowsMeta lists tracks as trackRows does, saying meta(i) of the track
// at i where it would say how long it plays, as the recent plays say when and
// where the listener played it. meta is asked only for the rows in view.
func (a *View) RowsMeta(c *ui.Context, tracks []netease.Track, covers bool, meta func(i int) string, header func(c *ui.Context), footer ...func(c *ui.Context)) (playingVisible bool) {
	p := *a.Palette
	cur := a.Current()
	// A song's menu can take it out of the playlist it is listed in.
	var pid int64
	if a.Nav.Current.Page == navigation.Playlist {
		pid = a.Nav.Current.ID
	}
	w, _ := c.Size()
	// An album's page has no column of the album its songs are on.
	wide := w-shell.SidebarWidth > 820 && a.Nav.Current.Page != navigation.Album
	top, bottom := a.Insets(c)
	// Rows out of view are not built, their marks with them: a list
	// scrolled away from its start or end has content under the bars.
	a.ScrollState.UnderTop, a.ScrollState.UnderBottom = true, true
	rows := len(tracks) + 1 + len(footer)
	list := ui.List(c.Key(a.Nav.Current), &a.State.List, rows)
	if pid != 0 || a.Nav.Current.Page == navigation.Cloud || a.Nav.Current.Page == navigation.Local {
		list.ItemKey(a.RowKeys(tracks))
	}
	list.Rows(func(item ui.ListRow) {
		i := item.Index
		if i == 0 {
			ui.Column(c).Children(func() {
				a.ScrollState.StartMark(c)
				header(c)
			})
			return
		}
		if i == rows-1 {
			defer a.ScrollState.EndMark(c)
		}
		if i > len(tracks) {
			footer[i-len(tracks)-1](c)
			return
		}
		i--
		tr := &tracks[i]
		missing := tr.IsLocal() && a.Local.Missing[tr.LocalID]
		playable := tr.Playable()
		playing := cur != nil && cur.Key() == tr.Key()
		row := ui.ButtonBase(c.Key(tr.Key())).Height(54).PaddingX(12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		// Create the track-specific menu only when MyGo asks to build it.
		// Passing a returned closure through a callback would allocate on every visible row.
		localMenu := a.Nav.Current.Page == navigation.Local
		row.ContextMenu(func(m *ui.Menu) {
			if localMenu {
				a.LocalItems(tr, true)(m)
			} else {
				a.SongItems(tr, pid)(m)
			}
		})
		if playing {
			// The bars overlay the list. Even a partially visible row counts,
			// but rows wholly covered by either bar do not.
			playingVisible = playingVisible || row.InViewMargin(-top, 0, -bottom, 0)
		}
		hovered := row.Hovered()
		bg := ui.Transparent
		switch {
		case playing:
			bg = p.Accent.Alpha(0.09)
		case hovered:
			bg = p.Hover
		}
		rowBG := row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond})
		row.Background(rowBG)
		if !playable {
			row.Opacity(0.42)
		}
		row.Children(func() {
			ui.Box(c).Width(26).Center().Children(func() {
				switch {
				case playing && !missing:
					a.PlayingBars(c, rowBG.Over(p.Background))
				case hovered && playable && !missing:
					ui.Icon(c, components.IconPlay).Size(13, 13).TextColor(p.Text)
				default:
					ui.Textf(c, "%d", i+1).FontSize(13).FontFeatures("tnum").TextColor(p.Faint)
				}
			})
			if covers {
				a.Cover(c, tr, 96, 38, 6)
			}
			ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
				name := p.Text
				if playing {
					name = p.Accent
				}
				spans := []ui.Span{{Text: tr.Name, Weight: 500, Color: name}}
				if len(tr.Alias) > 0 {
					spans = append(spans, ui.Span{Text: "（" + tr.Alias[0] + "）", Color: p.Faint})
				}
				ui.RichText(c, spans...).FontSize(14).SingleLine()
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if missing {
						ui.Text(c, "文件已移动或不可访问").FontSize(12.5).TextColor(p.Muted).SingleLine()
						return
					}
					if !playable {
						ui.Text(c, "VIP").FontSize(9).FontWeight(800).TextColor(p.Accent).Padding(0, 4).Radius(3).Border(1, p.Accent.Alpha(0.6))
					}
					a.ArtistLinks(c, tr, p.Muted, p.Text, false).FontSize(12.5).SingleLine().Shrink(1).MinWidth(0)
					if tr.Reason != "" {
						ui.Text(c, tr.Reason).FontSize(10.5).FontWeight(600).TextColor(p.Accent).Padding(1, 6).Radius(999).Background(p.Accent.Alpha(0.1)).SingleLine()
					}
				})
			})
			if wide {
				album := ui.RichText(c).Grow(2).Basis(0).MinWidth(0).FontSize(12.5).TextColor(p.Muted).SingleLine()
				album.Children(func() {
					if tr.Album.ID == 0 {
						ui.Text(c, tr.Album.Name)
					} else {
						a.Link(c.Key("album"), tr.Album.Name, p.Muted, p.Text, navigation.Route{Page: navigation.Album, ID: tr.Album.ID})
					}
				})
			}
			// A liked song's heart shows; the others' as the pointer is on them.
			if missing {
				ui.Button(c.Key("relink"), "重新定位").Disabled(a.Local.Busy).OnClick(func() { a.Relink(tr.LocalID) })
			} else {
				ui.Box(c).Size(32, 32).Center().Children(func() {
					if !tr.IsLocal() && (a.Library.Liked[tr.ID] || hovered) {
						a.Like(c.Key("like"), tr, 15, p.Muted)
					}
				})
			}
			if meta != nil {
				ui.Text(c, meta(i)).FontSize(12.5).FontFeatures("tnum").TextColor(p.Faint).MinWidth(88).TextAlign(ui.End)
			} else {
				ui.Text(c, components.Clock(tr.Length())).FontSize(12.5).FontFeatures("tnum").TextColor(p.Faint).Width(44).TextAlign(ui.End)
			}
		})
		if row.Clicked() {
			if missing {
				a.Relink(tr.LocalID)
			} else if playable {
				a.Play(tracks, i)
			}
		}
	}).Grow(1).Padding(top+shell.PageGap, shell.PagePad-12, bottom+32, shell.PagePad-12).ScrollbarInsets(top, 0, bottom, 0)
	if a.Nav.Current.Page == navigation.Playlist || a.Nav.Current.Page == navigation.Cloud || a.Nav.Current.Page == navigation.Local {
		list.TrackScroll(&a.ScrollState.PageScroll)
	}
	return playingVisible
}
