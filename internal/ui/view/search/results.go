/*
呈现各类搜索结果、标题与空状态，并连接卡片及列表操作。
*/

package search

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/components"
	"netease-music-mygo/internal/ui/view/page"
	searchvm "netease-music-mygo/internal/ui/viewmodel/search"
)

type View struct {
	History               *[]string
	Dismiss, ClearHistory func()
	Suggest, Search       func(string)
	Choose                func(string, string)
	TrackSuggestions      func(string) []searchvm.TrackSuggestion

	*page.Page
	Model        *searchvm.Search
	More         func(navigation.Route, *searchvm.SearchPage)
	Show, Open   func(navigation.Route)
	LoadPlaylist func(int64, bool)
}

// searchKinds are the kinds of search, as their tabs show them.
var searchKinds = []struct {
	kind        netease.SearchType
	label, unit string
}{
	{netease.SearchSongs, "单曲", "首单曲"},
	{netease.SearchArtists, "歌手", "位歌手"},
	{netease.SearchAlbums, "专辑", "张专辑"},
	{netease.SearchPlaylists, "歌单", "个歌单"},
	{netease.SearchUsers, "用户", "位用户"},
	{netease.SearchPodcasts, "播客", "个播客"},
}

func (a *View) Build(c *ui.Context) {
	r := a.Nav.Current
	d := a.Model.Pages.Get(r)
	header := func(c *ui.Context) { a.Header(c, d) }
	more := func() { a.More(r, d) }
	if r.Kind == netease.SearchSongs {
		if !d.Page.Loaded && d.Page.Loading {
			a.Skeleton(c, true, header)
			return
		}
		a.TrackRows(c, d.Res.Songs, true, header, func(c *ui.Context) {
			a.Foot(c, &d.Page, more)
			a.EmptyResults(c, d)
		})
		return
	}
	a.Scroll(c, func() {
		header(c)
		res := &d.Res
		switch r.Kind {
		case netease.SearchArtists:
			ui.Grid(c).Columns(a.Columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range res.Artists {
					a.ArtistCard(c.Key(res.Artists[i].ID), &res.Artists[i])
				}
			})
		case netease.SearchAlbums:
			a.AlbumGrid(c, res.Albums)
		case netease.SearchPlaylists:
			ui.Grid(c).Columns(a.Columns(c, 156)).GapX(20).GapY(28).Children(func() {
				for i := range res.Playlists {
					a.FoundPlaylistCard(c.Key(res.Playlists[i].ID), &res.Playlists[i])
				}
			})
		case netease.SearchUsers:
			for i := range res.Users {
				a.UserRow(c.Key(res.Users[i].UserID), &res.Users[i])
			}
		case netease.SearchPodcasts:
			for i := range res.Podcasts {
				a.PodcastRow(c.Key(res.Podcasts[i].ID), &res.Podcasts[i], page.TrackWidth(c))
			}
		}
		if !d.Page.Loaded && d.Page.Loading {
			a.CardsSkeleton(c, a.Columns(c, 156), 156, false)
			return
		}
		a.Foot(c, &d.Page, more)
		a.EmptyResults(c, d)
	})
}

func (a *View) EmptyResults(c *ui.Context, d *searchvm.SearchPage) {
	if d.Page.Loaded && d.Res.Len() == 0 {
		a.Empty(c, "没有找到相关结果")
	}
}

// searchHeader is what was searched for, how many it found, and the tabs
// of what it may look for instead.
func (a *View) Header(c *ui.Context, d *searchvm.SearchPage) {
	r := a.Nav.Current
	sub := "搜索"
	at := 0
	for i, k := range searchKinds {
		if k.kind == r.Kind {
			at = i
			if d.Page.Loaded {
				sub = fmt.Sprintf("搜索 · 找到 %d %s", d.Res.Total, k.unit)
			}
		}
	}
	a.Heading(c, "“"+r.Query+"”", sub)
	ui.Row(c).Margin(0, 0, 18, 0).Children(func() {
		labels := make([]string, len(searchKinds))
		for i, k := range searchKinds {
			labels[i] = k.label
		}
		if components.Segmented(c.Key("kind"), &at, labels...) {
			r.Kind = searchKinds[at].kind
			a.Show(r)
		}
	})
}

// ArtistCard is an artist's round photo over their name, opening their page.
func (a *View) ArtistCard(c *ui.Context, ar *netease.ArtistProfile) {
	p := *a.Palette
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(ar.Name)
	hovered := card.Hovered()
	card.Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		art := ui.Box(c).AspectRatio(1).Radius(999)
		if hovered {
			art.Shadow(0, 12, 28, -6, p.CardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.CardShade)
		}
		art.Children(func() { a.CoverFill(c, photo, 400, 999) })
		ui.Text(c, ar.Name).FontSize(13).FontWeight(500).SingleLine().AlignSelf(ui.Center)
	})
	if card.Clicked() {
		a.Open(navigation.Route{Page: navigation.Artist, ID: ar.ID})
	}
}

// FoundPlaylistCard is a playlist a search found: its card, with how many
// songs it has and who made it under its name, as strangers' playlists
// are told apart by their makers.
func (a *View) FoundPlaylistCard(c *ui.Context, pl *netease.Playlist) {
	sub := fmt.Sprintf("%d 首", pl.TrackCount)
	if pl.Creator.Nickname != "" {
		sub += " · " + pl.Creator.Nickname
	}
	if a.CoverCard(c, pl.Name, pl.Cover(), pl.PlayCount, sub, func() { a.LoadPlaylist(pl.ID, true) }) {
		a.Open(navigation.Route{Page: navigation.Playlist, ID: pl.ID})
	}
}
