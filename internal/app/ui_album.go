package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// albumDetail loads an album from NetEase, and subscribeAlbum saves one for
// the listener or unsaves it; tests replace them.
var (
	albumDetail    = netease.AlbumDetail
	subscribeAlbum = netease.SubscribeAlbum
)

// albumData is what the app has of an album.
type albumData struct {
	album   *netease.AlbumProfile
	loading bool
	err     string
	failure error
}

// AlbumProfile omits Songs from JSON because API responses carry them beside
// the album. A persistent snapshot must explicitly keep both parts together.
type keptAlbum struct {
	Album *netease.AlbumProfile
	Songs []netease.Track
}

func (a *app) loadAlbum(id int64) {
	d := a.albums.get(id)
	if d == nil {
		d = &albumData{}
		a.albums.set(id, d)
	}
	if d.album != nil && d.err == "" || d.loading {
		return
	}
	d.loading = true
	req, gen := a.cacheRequest(fmt.Sprintf("album/%d", id)), a.homeGen
	fetch := albumDetail
	go func() {
		var kept keptAlbum
		if at := req.read(&kept); !at.IsZero() && kept.Album != nil && kept.Album.ID == id {
			kept.Album.Songs = kept.Songs
			a.post(func() {
				if a.homeGen == gen && a.albums.peek(id) == d && d.album == nil {
					d.album = kept.Album
				}
			})
		}
		al, err := fetch(id)
		var bytes []byte
		if err == nil && al != nil {
			bytes = cacheBytes(keptAlbum{al, al.Songs})
		}
		a.post(func() {
			if a.homeGen != gen || a.albums.peek(id) != d {
				return
			}
			d.loading = false
			d.failure = err
			if err != nil {
				d.err = "专辑加载失败"
				return
			}
			d.album = al
			d.err = ""
			req.keep(bytes)
		})
	}()
}

func (a *app) albumPage(c *ui.Context) {
	id := a.route.id
	// Whether the listener saved this album is whether the albums they saved
	// list it: load that once, as the page may be the first thing opened.
	a.freshenAlbums()
	d := a.albums.get(id)
	if d.album == nil {
		if !d.loading || d.err != "" {
			a.centered(c, d.err, func() { a.loadAlbum(id) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, false, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	a.trackRows(c, d.album.Songs, false, func(c *ui.Context) { a.albumHeader(c, d.album) })
}

func (a *app) albumHeader(c *ui.Context, al *netease.AlbumProfile) {
	p := a.pal
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		ui.Box(c).Size(212, 212).Radius(14).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
			a.coverFill(c, al.PicURL, 600, 14)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "专辑").FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, al.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			ui.RichText(c).FontSize(13).TextColor(p.muted).Children(func() {
				for i, ar := range al.Artists {
					if i > 0 {
						ui.Text(c, ", ")
					}
					a.link(c.Key(i), ar.Name, p.muted, p.text, route{page: pageArtist, id: ar.ID})
				}
				meta := fmt.Sprintf(" · %d 首", len(al.Songs))
				if al.PublishTime > 0 {
					meta = " · " + time.UnixMilli(al.PublishTime).Format("2006-01-02") + meta
				}
				ui.Text(c, meta)
			})
			if d := strings.TrimSpace(al.Description); d != "" {
				ui.Text(c, strings.ReplaceAll(d, "\n", " ")).FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45).Margin(2, 0, 0, 0)
			}
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueue(al.Songs, firstPlayable(al.Songs))
				}
				icon, label := iconPlus, "收藏"
				if a.albumSaved(al.ID) {
					icon, label = iconCheck, "已收藏"
				}
				if pillButton(c.Key("save"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
					a.toggleAlbumSaved(*al)
				}
			})
		})
	})
}

// albumSaved reports whether the listener saved album id, as its page says.
// NetEase does not say of an album whether it is saved, so this is what the
// list of the albums they saved, loaded once, says.
func (a *app) albumSaved(id int64) bool {
	return slices.ContainsFunc(a.subAlbums, func(al netease.AlbumProfile) bool { return al.ID == id })
}

// toggleAlbumSaved saves album al for the listener, or unsaves it, at once in
// the app and then with NetEase, going back if NetEase refuses: the button
// reads how it went, so saving says nothing else. Signed out, it asks the
// listener to sign in.
func (a *app) toggleAlbumSaved(al netease.AlbumProfile) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	id := al.ID
	if a.savingAlb[id] {
		return
	}
	if a.savingAlb == nil {
		a.savingAlb = map[int64]bool{}
	}
	a.savingAlb[id] = true
	save := !a.albumSaved(id)
	at := a.setAlbumSaved(al, save, 0)
	uid := a.profile.UserID
	finishEdit := a.beginLibraryEdit()
	go func() {
		err := subscribeAlbum(id, save)
		a.post(func() {
			finishEdit()
			defer a.persistLibrary()
			delete(a.savingAlb, id)
			if err == nil || a.profile == nil || a.profile.UserID != uid {
				// Saved, or signed out or in as someone else meanwhile, whose
				// albums are what load now.
				return
			}
			a.setAlbumSaved(al, !save, max(at, 0))
			a.toast = "操作失败，请稍后再试"
		})
	}()
}

// setAlbumSaved marks album al saved or not, where a saved one goes at index
// at, and returns where it was, or -1.
func (a *app) setAlbumSaved(al netease.AlbumProfile, save bool, at int) int {
	// A new slice, not shifting the one the listener's page may be listing.
	saved := slices.Clone(a.subAlbums)
	was := slices.IndexFunc(saved, func(s netease.AlbumProfile) bool { return s.ID == al.ID })
	if was >= 0 {
		saved = slices.Delete(saved, was, was+1)
	}
	if save {
		al.Songs = nil
		saved = slices.Insert(saved, min(at, len(saved)), al)
	}
	a.subAlbums = saved
	return was
}

// albumGrid shows albums as cards, each opening its page.
func (a *app) albumGrid(c *ui.Context, albums []netease.AlbumProfile) {
	ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
		for i := range albums {
			a.albumCard(c.Key(albums[i].ID), &albums[i])
		}
	})
}

// albumCard is an album's cover, lifting under the pointer, over its name
// and the year it came out.
func (a *app) albumCard(c *ui.Context, al *netease.AlbumProfile) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(al.Name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(10)
		lift := ui.Pose{}
		if hovered {
			lift = ui.Pose{Y: -3}
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.AnimatePose(lift, ui.Snappy)
		art.Children(func() { a.coverFill(c, al.PicURL, 400, 10) })
		ui.Text(c, al.Name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		var meta []string
		if a.route.page != pageArtist {
			meta = append(meta, al.ArtistNames())
		}
		if al.PublishTime > 0 {
			meta = append(meta, time.UnixMilli(al.PublishTime).Format("2006"))
		}
		ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(p.muted).SingleLine().Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		a.open(route{page: pageAlbum, id: al.ID})
	}
}
