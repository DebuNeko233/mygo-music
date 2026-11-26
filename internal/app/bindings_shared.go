/*
为各视图连接共用的歌曲菜单、封面呈现与剪贴板回调。
*/

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	desktopview "netease-music-mygo/internal/desktop/view"
	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/desktop/view/page"
	"netease-music-mygo/internal/desktop/view/tracklist"
	"netease-music-mygo/internal/netease"
)

func (a *app) bindSharedViews() {
	p := &a.views.page
	*p = page.Page{Palette: &a.pal, Nav: &a.nav, ScrollState: &a.shell, Offline: func() bool { return a.network.offline }, Renderers: page.Renderers{
		Cover: a.views.artwork.Cover, CoverFill: a.views.artwork.Fill, Link: a.views.artist.Link, HeaderDescription: a.intro.Header, DescriptionSummary: a.intro.Summary,
		TrackRows: a.views.tracks.Rows, TrackRowsMeta: a.views.tracks.RowsMeta,
		Skeleton: func(c *ui.Context, covers bool, header func(*ui.Context)) {
			a.views.page.TrackSkeleton(c, components.PageShimmer(c, a.pal.Dark), covers, header)
		},
		HeaderSkeleton: func(c *ui.Context) { a.views.page.PlaylistHeaderSkeleton(c, components.PageShimmer(c, a.pal.Dark)) },
		CardsSkeleton: func(c *ui.Context, n int, min float32, freq bool) {
			a.views.page.PlaylistCardsSkeleton(c, components.PageShimmer(c, a.pal.Dark), n, min, freq)
		},
		PlaylistCard: a.views.cards.Playlist, AlbumGrid: a.views.album.Grid, PodcastGrid: a.views.podcast.Grid, ArtistCard: a.views.search.ArtistCard, UserRow: a.views.user.Row, PodcastRow: a.views.podcast.Row, CoverCard: a.views.cards.Cover,
	}}
	a.views.artwork = components.Artwork{Palette: &a.pal, PlayerOpen: func() bool { return a.playbackState.Open }, Load: func(url string, backdrop bool, el ui.Element) *ui.Bitmap {
		return components.Bitmap(a.art.GetInView(url, backdrop, el))
	}, LoadList: func(url string, el ui.Element) *ui.Bitmap {
		return components.Bitmap(a.art.GetListInView(a.nav.Current, url, el))
	}}
	a.views.cards = desktopview.Cards{Page: p, Open: a.open, LoadPlaylist: a.loadPlaylist}
	a.views.tracks = tracklist.View{Page: p, State: &a.tracks, Local: &a.local, Library: &a.library, Current: func() *netease.Track { return a.pl.Current() }, LocalItems: a.localSongItems, SongItems: a.views.songMenu.Items, PlayingBars: a.views.playback.NowPlayingBars, Cover: a.trackCover, ArtistLinks: a.views.artist.Links, Like: a.views.like.Button, Relink: a.chooseLocalFile, Play: a.playTrackQueue}

	a.views.like = components.Like{Palette: &a.pal, SignedIn: func() bool { return a.account.Profile != nil }, AskPrograms: a.askProgramLikes, IsLiked: a.library.IsLiked, Toggle: a.toggleLike}
	a.intro.Page = p
	p.Image = func(url string, blur bool) components.Image { return viewImage(a.art.Get(url, blur)) }
}

var copyText = func(text string) error { return mygo.Clipboard.Write(transfer.TextData(text)) }
