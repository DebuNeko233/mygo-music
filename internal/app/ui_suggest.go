package app

import (
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/components"
	"netease-music-mygo/internal/netease"
	searchvm "netease-music-mygo/internal/viewmodel/search"
)

// searchField is the search field atop the sidebar, which suggests what
// its query may be after as it is typed, and, empty, lists the searches
// before.
func (a *app) searchField(c *ui.Context) {
	p := a.pal
	field := ui.Row(c).Height(32).PaddingX(10).Gap(7).Radius(9).AlignItems(ui.Center).Margin(0, 0, 14, 0)
	if c.Focused(a.searchBox) {
		field.Background(p.Background).Shadow(0, 0, 0, 3, p.Accent.Alpha(0.25)).Border(1, p.Accent.Alpha(0.6))
	} else {
		field.Background(p.Hover).Border(1, p.Hairline)
	}
	field.Children(func() {
		ui.Icon(c, components.IconSearch).Size(14, 14).TextColor(p.Muted)
		cb := ui.ComboboxBase(c.Key("search"), &a.searches.Query)
		in := cb.Input.Bind(&a.searchBox).Grow(1).MinWidth(0).
			Placeholder("搜索/链接").FontSize(13).Label("搜索")
		if a.searches.Query != "" {
			clear := ui.ButtonBase(c.Key("clear-query")).Size(20, 20).Shrink(0).Margin(0, -4, 0, 0).
				Radius(999).Center().KeepFocus().Material(glass.Glass{Interactive: true}).
				Label("清空搜索").Tooltip("清空搜索")
			color := p.Muted
			if clear.Hovered() || clear.Pressed() {
				color = p.Text
			}
			clear.Children(func() { ui.Icon(c, components.IconX).Size(10, 10).TextColor(color) })
			if clear.Clicked() {
				a.searches.Query = ""
				a.dismissSuggestions()
				cb.SetOpen(false)
				a.searchBox.Focus(c)
			}
		}
		q := strings.TrimSpace(a.searches.Query)
		s := &a.searches.Suggestions
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
				Background(p.Background).Border(1, p.Hairline).
				Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2))
			ui.Scroll(c.Key("search-options")).Shrink(1).Children(func() {
				option := func(key, label string) ui.Element {
					row := cb.Item(key).MinHeight(36).Padding(6, 10).Gap(10).Radius(7).AlignItems(ui.Center).Label(label)
					if row.Highlighted() {
						row.Background(p.Hover)
					}
					return row
				}
				showSuggestion := func(sg searchvm.Suggestion, local, playable bool) {
					row := option(sg.Key, sg.Kind+"："+sg.Name)
					if !playable {
						row.Disabled(true).Opacity(0.42)
					}
					row.Children(func() {
						if local {
							ui.Icon(c, components.IconPlay).Size(13, 13).Shrink(0).TextColor(p.Muted)
						}
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, sg.Name).FontSize(13).SingleLine()
							if sg.Detail != "" {
								ui.Text(c, sg.Detail).FontSize(11.5).TextColor(p.Muted).SingleLine()
							}
						})
						tag := ui.Text(c, sg.Kind).FontSize(11.5).SingleLine().Shrink(0)
						if local {
							tag.TextColor(p.Accent).Padding(2, 6).Radius(999).Background(p.Accent.Alpha(0.1))
						} else {
							tag.TextColor(p.Faint)
						}
					})
				}
				if q == "" {
					panel.Label("搜索历史")
					ui.Row(c).Padding(2, 4, 2, 10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "搜索历史").FontSize(12).TextColor(p.Muted).Grow(1)
						components.IconButton(c.Key("clear"), components.IconTrash, 14, p.Muted, "清空搜索历史").KeepFocus().OnClick(a.clearSearchHistory)
					})
					for _, h := range a.settings.SearchHistory {
						option("history:"+h, h).Children(func() {
							ui.Icon(c, components.IconSearch).Size(13, 13).TextColor(p.Muted)
							ui.Text(c, h).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
						})
					}
					return
				}
				panel.Label("搜索建议")
				// Keep Enter searching as typed; local matches follow immediately
				// without waiting for online suggestions.
				option("query", "搜索“"+q+"”").Children(func() {
					ui.Icon(c, components.IconSearch).Size(13, 13).TextColor(p.Muted)
					ui.Text(c, "搜索“"+q+"”").FontSize(13).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
				})
				for _, sg := range a.trackSuggestions(q) {
					showSuggestion(sg.Suggestion, true, sg.Playable)
				}

				if s.Query == q {
					for _, sg := range s.Items {
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
		if s.Query != "" && (!cb.Open() || q == "" || link || !in.Focused()) {
			a.dismissSuggestions()
		}
	})
}

// choose goes where the option chosen in the search field's popup goes.
func (a *app) choose(key, q string) {
	if a.chooseTrackSuggestion(key, q) {
		return
	}
	if h, ok := strings.CutPrefix(key, "history:"); ok {
		a.searches.Query = h
		a.search(h)
		return
	}
	if key == "query" {
		a.search(q)
		return
	}
	for _, sg := range a.searches.Suggestions.Items {
		if sg.Key != key {
			continue
		}
		if sg.Route.Page == pageSearch {
			a.searches.Query = sg.Route.Query
			a.searchFor(sg.Route)
			return
		}
		a.rememberSearch(q)
		a.dismissSuggestions()
		a.open(sg.Route)
		return
	}
}
