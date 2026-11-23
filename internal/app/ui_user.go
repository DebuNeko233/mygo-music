package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// userDetail and userPlaylists load a user and their playlists from
// NetEase, and followedArtists and subscribedPodcasts what the listener
// follows; tests replace them.
var (
	userDetail         = netease.UserDetail
	userPlaylists      = netease.UserPlaylists
	followedArtists    = netease.FollowedArtists
	subscribedPodcasts = netease.SubscribedPodcasts
)

func (a *app) userPage(c *ui.Context) {
	p := a.pal
	id := a.nav.Current.ID
	d := a.users.Pages.Get(id)
	if d.User == nil {
		if d.Loading == 0 {
			a.centered(c, d.Error, func() { a.loadUser(id) })
			return
		}
		s := a.pageShimmer(c)
		a.scrollPage(c, func() {
			a.playlistHeaderSkeleton(c, s)
			a.playlistCardsSkeleton(c, s, a.columns(c, 168), 168, false)
		})
		return
	}
	u := d.User
	own := a.account.Profile != nil && a.account.Profile.UserID == id
	a.scrollPage(c, func() {
		ui.Row(c).Gap(detailHeaderGap).AlignItems(ui.End).Children(func() {
			ui.Box(c).Size(artistPhotoSize, artistPhotoSize).Radius(artistPhotoSize/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
				a.coverFill(c, u.AvatarURL, 600, artistPhotoSize/2)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, "用户").FontSize(12).FontWeight(600).TextColor(p.accent)
				ui.Text(c, u.Nickname).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				ui.Text(c, fmt.Sprintf("%s 关注 · %s 粉丝", count(u.Follows), count(u.Followeds))).FontSize(13).TextColor(p.muted)
				a.headerDescription(c, u.Nickname+" · 个人签名", u.Signature, artistPhotoSize)
			})
		})
		a.section(c, "创建的歌单")
		switch {
		case d.Loading > 0:
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 168), 168, false)
		case len(d.Playlists) == 0:
			a.empty(c, "暂无公开歌单")
		default:
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.Playlists {
					a.playlistCard(c.Key(d.Playlists[i].ID), &d.Playlists[i])
				}
			})
		}
		if own && a.library.SubsShown {
			a.section(c, "收藏的专辑")
			if len(a.library.SubAlbums) == 0 {
				a.empty(c, "还没有收藏专辑")
			}
			a.albumGrid(c, a.library.SubAlbums)
		}
		if own && a.podcasts.Home.SubscriptionsShown {
			a.section(c, "订阅的播客")
			if len(a.podcasts.Home.Subs) == 0 {
				a.empty(c, "还没有订阅播客")
			}
			a.podcastGrid(c, len(a.podcasts.Home.Subs), func(i int) *netease.Podcast { return &a.podcasts.Home.Subs[i] })
		}
		if own && d.ArtistsShown {
			a.section(c, "关注的歌手")
			if len(d.Artists) == 0 {
				a.empty(c, "还没有关注歌手")
			}
			ui.Grid(c).Columns(a.columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range d.Artists {
					a.artistCard(c.Key(d.Artists[i].ID), &d.Artists[i])
				}
			})
		}
	})
}

// userRow is a user in a list, opening their page.
func (a *app) userRow(c *ui.Context, u *netease.User) {
	p := a.pal
	const pad, gap, cover = 12, 14, 48
	row := ui.ButtonBase(c).Padding(10, pad).Gap(gap).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(u.Nickname)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		a.cover(c, u.AvatarURL, 120, cover, cover/2)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, u.Nickname).FontSize(14).FontWeight(600).SingleLine()
			a.descriptionSummary(c, u.Nickname+" · 个人签名", u.Signature, trackPageWidth(c)-2*pad-gap-cover, 1)
		})
	})
	if row.Clicked() {
		a.open(route{Page: pageUser, ID: u.UserID})
	}
}
