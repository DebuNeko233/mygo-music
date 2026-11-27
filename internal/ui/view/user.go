/*
呈现用户资料页与用户列表行，提供个人内容的浏览入口。
*/

package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/view/page"
	accountvm "netease-music-mygo/internal/ui/viewmodel/account"
	libraryvm "netease-music-mygo/internal/ui/viewmodel/library"
	podcastvm "netease-music-mygo/internal/ui/viewmodel/podcast"
	uservm "netease-music-mygo/internal/ui/viewmodel/user"
)

type User struct {
	*page.Page
	Model    *uservm.Users
	Account  *accountvm.Account
	Library  *libraryvm.Library
	Podcasts *podcastvm.Podcasts
	Count    func(int) string
	Load     func(int64)
	Open     func(navigation.Route)
}

func (a *User) Build(c *ui.Context) {
	p := *a.Palette
	id := a.Nav.Current.ID
	d := a.Model.Pages.Get(id)
	if d.User == nil {
		if d.Loading == 0 {
			a.Centered(c, d.Error, func() { a.Load(id) })
			return
		}
		a.ScrollReady(c, false, func() {
			a.HeaderSkeleton(c)
			a.CardsSkeleton(c, a.Columns(c, 168), 168, false)
		})
		return
	}
	u := d.User
	own := a.Account.Profile != nil && a.Account.Profile.UserID == id
	a.ScrollReady(c, d.Loading == 0, func() {
		ui.Row(c).Gap(page.DetailHeaderGap).AlignItems(ui.End).Children(func() {
			ui.Box(c).Size(page.ArtistPhotoSize, page.ArtistPhotoSize).Radius(page.ArtistPhotoSize/2).Shadow(0, 18, 40, -10, p.CardShade).Children(func() {
				a.CoverFill(c, u.AvatarURL, 600, page.ArtistPhotoSize/2)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, "用户").FontSize(12).FontWeight(600).TextColor(p.Accent)
				ui.Text(c, u.Nickname).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				ui.Text(c, fmt.Sprintf("%s 关注 · %s 粉丝", a.Count(u.Follows), a.Count(u.Followeds))).FontSize(13).TextColor(p.Muted)
				a.HeaderDescription(c, u.Nickname+" · 个人签名", u.Signature, page.ArtistPhotoSize)
			})
		})
		a.Section(c, "创建的歌单")
		switch {
		case d.Loading > 0:
			a.CardsSkeleton(c, a.Columns(c, 168), 168, false)
		case len(d.Playlists) == 0:
			a.Empty(c, "暂无公开歌单")
		default:
			ui.Grid(c).Columns(a.Columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.Playlists {
					a.PlaylistCard(c.Key(d.Playlists[i].ID), &d.Playlists[i])
				}
			})
		}
		if own && a.Library.SubsShown {
			a.Section(c, "收藏的专辑")
			if len(a.Library.SubAlbums) == 0 {
				a.Empty(c, "还没有收藏专辑")
			}
			a.AlbumGrid(c, a.Library.SubAlbums)
		}
		if own && a.Podcasts.Home.SubscriptionsShown {
			a.Section(c, "订阅的播客")
			if len(a.Podcasts.Home.Subs) == 0 {
				a.Empty(c, "还没有订阅播客")
			}
			a.PodcastGrid(c, len(a.Podcasts.Home.Subs), func(i int) *netease.Podcast { return &a.Podcasts.Home.Subs[i] })
		}
		if own && d.ArtistsShown {
			a.Section(c, "关注的歌手")
			if len(d.Artists) == 0 {
				a.Empty(c, "还没有关注歌手")
			}
			ui.Grid(c).Columns(a.Columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range d.Artists {
					a.ArtistCard(c.Key(d.Artists[i].ID), &d.Artists[i])
				}
			})
		}
	})
}

// userRow is a user in a list, opening their page.
func (a *User) Row(c *ui.Context, u *netease.User) {
	p := *a.Palette
	const pad, gap, cover = 12, 14, 48
	row := ui.ButtonBase(c).Padding(10, pad).Gap(gap).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(u.Nickname)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		a.Cover(c, u.AvatarURL, 120, cover, cover/2)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, u.Nickname).FontSize(14).FontWeight(600).SingleLine()
			a.DescriptionSummary(c, u.Nickname+" · 个人签名", u.Signature, page.TrackWidth(c)-2*pad-gap-cover, 1)
		})
	})
	if row.Clicked() {
		a.Open(navigation.Route{Page: navigation.User, ID: u.UserID})
	}
}
