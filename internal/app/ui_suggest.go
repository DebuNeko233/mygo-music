package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// suggestDelay is how long typing pauses before suggestions are asked for.
const suggestDelay = 220 * time.Millisecond

// suggestState is what a search may be after, as its query is typed.
type suggestState struct {
	query    string // what they are for
	items    []suggestion
	loading  bool
	failed   bool
	playlist playlistSuggestState
	timer    *time.Timer // waiting for typing to pause
}

// suggestion is a page a query may be after: a song's search, or an
// artist's, album's or playlist's page.
type suggestion struct {
	key          string // its option in the popup
	name, detail string
	kind         string
	route        route
}

// suggest asks for suggestions for q, once typing pauses.
func (a *app) suggest(q string) {
	s := &a.suggestions
	if q == s.query {
		return
	}
	a.dismissSuggestions()
	s.query, s.loading = q, true
	fetch := searchSuggest
	s.timer = time.AfterFunc(suggestDelay, func() {
		res, err := fetch(q)
		a.post(func() {
			// Typed on, or searched since.
			if q != s.query {
				return
			}
			s.timer, s.loading, s.failed = nil, false, err != nil
			s.items = suggestionList(res)
		})
	})
}

func (a *app) dismissSuggestions() {
	s := &a.suggestions
	if s.timer != nil {
		s.timer.Stop()
	}
	*s = suggestState{}
}

// suggestionList is the first ten suggestions, the kinds in the order
// NetEase ranks them.
func suggestionList(res netease.Suggestions) []suggestion {
	byKind := map[string][]suggestion{}
	for _, t := range res.Songs {
		byKind["songs"] = append(byKind["songs"], suggestion{fmt.Sprint("song:", t.ID), t.Name, t.ArtistNames(), "单曲",
			route{page: pageSearch, query: t.Name, kind: netease.SearchSongs}})
	}
	for _, ar := range res.Artists {
		byKind["artists"] = append(byKind["artists"], suggestion{fmt.Sprint("artist:", ar.ID), ar.Name, "", "歌手",
			route{page: pageArtist, id: ar.ID}})
	}
	for _, al := range res.Albums {
		byKind["albums"] = append(byKind["albums"], suggestion{fmt.Sprint("album:", al.ID), al.Name, al.ArtistNames(), "专辑",
			route{page: pageAlbum, id: al.ID}})
	}
	for _, pl := range res.Playlists {
		byKind["playlists"] = append(byKind["playlists"], suggestion{fmt.Sprint("playlist:", pl.ID), pl.Name, pl.Creator.Nickname, "歌单",
			route{page: pagePlaylist, id: pl.ID}})
	}
	var out []suggestion
	for _, kind := range append(res.Order, "songs", "artists", "albums", "playlists") {
		out = append(out, byKind[kind]...)
		delete(byKind, kind) // listed once, if also in the order
	}
	return out[:min(len(out), 10)]
}

// searchField is the search field atop the sidebar, which suggests what
// its query may be after as it is typed, and, empty, lists the searches
// before.
func (a *app) searchField(c *ui.Context) {
	p := a.pal
	field := ui.Row(c).Height(32).PaddingX(10).Gap(7).Radius(9).AlignItems(ui.Center).Margin(0, 0, 14, 0)
	if c.Focused(a.searchBox) {
		field.Background(p.bg).Shadow(0, 0, 0, 3, p.accent.Alpha(0.25)).Border(1, p.accent.Alpha(0.6))
	} else {
		field.Background(p.hover).Border(1, p.hairline)
	}
	field.Children(func() {
		ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.muted)
		cb := ui.ComboboxBase(c.Key("search"), &a.query)
		in := cb.Input.Bind(&a.searchBox).Grow(1).MinWidth(0).
			Placeholder("搜索/链接").FontSize(13).Label("搜索")
		if a.query != "" {
			clear := ui.ButtonBase(c.Key("clear-query")).Size(20, 20).Shrink(0).Margin(0, -4, 0, 0).
				Radius(999).Center().KeepFocus().Material(glass.Glass{Interactive: true}).
				Label("清空搜索").Tooltip("清空搜索")
			color := p.muted
			if clear.Hovered() || clear.Pressed() {
				color = p.text
			}
			clear.Children(func() { ui.Icon(c, iconX).Size(10, 10).TextColor(color) })
			if clear.Clicked() {
				a.query = ""
				a.dismissSuggestions()
				cb.SetOpen(false)
				a.searchBox.Focus(c)
			}
		}
		q := strings.TrimSpace(a.query)
		s := &a.suggestions
		_, link, _ := netease.ParseMusicLink(q)
		if link {
			cb.SetOpen(false)
		}
		if !link && in.Focused() && cb.Open() && q != "" {
			a.suggest(q)
		}
		// Empty, it lists the searches before, if any.
		if q == "" && len(a.settings.SearchHistory) == 0 {
			cb.SetOpen(false)
		}
		cb.Popup(func(panel ui.Element) {
			panel.AttachTo(field, ui.AnchorBottomStart, ui.AnchorTopStart).
				Width(360).Padding(6).Margin(8, 0, 0, 0).Radius(12).
				Background(p.bg).Border(1, p.hairline).
				Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2))
			// Keep every option reachable when local and online matches fill
			// a small window. Leave room for the panel's padding and margin.
			_, height := c.Size()
			bounds := field.Bounds()
			ui.Scroll(c.Key("search-options")).MaxHeight(max(80, height-bounds.Y-bounds.H-32)).Children(func() {
				option := func(key, label string) ui.Element {
					row := cb.Item(key).MinHeight(36).Padding(6, 10).Gap(10).Radius(7).AlignItems(ui.Center).Label(label)
					if row.Highlighted() {
						row.Background(p.hover)
					}
					return row
				}
				showSuggestion := func(sg suggestion, local, playable bool) {
					row := option(sg.key, sg.kind+"："+sg.name)
					if !playable {
						row.Disabled(true).Opacity(0.42)
					}
					row.Children(func() {
						if local {
							ui.Icon(c, iconPlay).Size(13, 13).Shrink(0).TextColor(p.muted)
						}
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, sg.name).FontSize(13).SingleLine()
							if sg.detail != "" {
								ui.Text(c, sg.detail).FontSize(11.5).TextColor(p.muted).SingleLine()
							}
						})
						tag := ui.Text(c, sg.kind).FontSize(11.5).SingleLine().Shrink(0)
						if local {
							tag.TextColor(p.accent).Padding(2, 6).Radius(999).Background(p.accent.Alpha(0.1))
						} else {
							tag.TextColor(p.faint)
						}
					})
				}
				if q == "" {
					panel.Label("搜索历史")
					ui.Row(c).Padding(2, 4, 2, 10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "搜索历史").FontSize(12).TextColor(p.muted).Grow(1)
						iconButton(c.Key("clear"), iconTrash, 14, p.muted, "清空搜索历史").KeepFocus().OnClick(a.clearSearchHistory)
					})
					for _, h := range a.settings.SearchHistory {
						option("history:"+h, h).Children(func() {
							ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.muted)
							ui.Text(c, h).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
						})
					}
					return
				}
				panel.Label("搜索建议")
				// Keep Enter searching as typed; local matches follow immediately
				// without waiting for online suggestions.
				option("query", "搜索“"+q+"”").Children(func() {
					ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.muted)
					ui.Text(c, "搜索“"+q+"”").FontSize(13).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
				})
				for _, sg := range a.playlistSuggestions(q) {
					showSuggestion(sg.suggestion, true, sg.playable)
				}
				if s.query == q {
					for _, sg := range s.items {
						showSuggestion(sg, false, true)
					}
				}
			})
		})
		if key, ok := cb.Chosen(); ok {
			cb.SetOpen(false)
			a.choose(key, q)
		} else if in.Submitted() && q != "" {
			cb.SetOpen(false)
			a.search(q)
		}
		// Keyboard selection closes the popup before Chosen is handled.
		// Keep its data until then, just as for a pointer selection.
		if s.query != "" && (!cb.Open() || q == "" || link || !in.Focused()) {
			a.dismissSuggestions()
		}
	})
}

// choose goes where the option chosen in the search field's popup goes.
func (a *app) choose(key, q string) {
	if a.choosePlaylistSuggestion(key, q) {
		return
	}
	if h, ok := strings.CutPrefix(key, "history:"); ok {
		a.query = h
		a.search(h)
		return
	}
	if key == "query" {
		a.search(q)
		return
	}
	for _, sg := range a.suggestions.items {
		if sg.key != key {
			continue
		}
		if sg.route.page == pageSearch {
			a.query = sg.route.query
			a.searchFor(sg.route)
			return
		}
		a.rememberSearch(q)
		a.dismissSuggestions()
		a.open(sg.route)
		return
	}
}
