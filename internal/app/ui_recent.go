package app

import (
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// What the listener played lately, on any device signed in as them: NetEase
// keeps their last 300 songs, and the playlists, albums, podcasts and
// episodes they played along with them.

// recentLimit is how many of each kind the page asks NetEase for: 300 is
// where its count of songs stops.
const recentLimit = 300

// recentPlays, and the rest, ask NetEase what the listener played lately;
// tests replace them.
var (
	recentPlaylists = netease.RecentPlaylists
	recentAlbums    = netease.RecentAlbums
	recentPodcasts  = netease.RecentPodcasts
	recentVoices    = netease.RecentVoices
)

// recentData is what the app has of the listener's recent plays.
type recentData struct {
	loading bool
	shown   bool // whether it has loaded once
	err     string
	failure error
	at      time.Time // when what it holds was asked for
	songs   []netease.Play
	// The other kinds of play record, the ones NetEase keeps beside the
	// songs, the latest first as they are.
	playlists []netease.Played[netease.Playlist]
	albums    []netease.Played[netease.AlbumProfile]
	podcasts  []netease.Played[netease.Podcast]
	voices    []netease.Played[netease.Voice]
}

// keptRecent is what the page keeps of the records between runs.
type keptRecent struct {
	Songs     []netease.Play
	Playlists []netease.Played[netease.Playlist]
	Albums    []netease.Played[netease.AlbumProfile]
	Podcasts  []netease.Played[netease.Podcast]
	Voices    []netease.Played[netease.Voice]
	At        time.Time
}

// loadRecent asks NetEase what the listener played lately, unless that is
// already on its way: the songs, which are the page, and the playlists,
// albums, podcasts and episodes it keeps beside them.
func (a *app) loadRecent() {
	d := &a.recent
	if a.profile == nil || d.loading {
		return
	}
	uid, gen := a.profile.UserID, a.homeGen
	d.loading = true
	req := a.cacheRequest("recent")
	// The copy on disk shows first, so that a page open without a network
	// still says what was played last.
	if !d.shown {
		var kept keptRecent
		if at := req.read(&kept); !at.IsZero() {
			d.songs, d.playlists, d.albums = kept.Songs, kept.Playlists, kept.Albums
			d.podcasts, d.voices = kept.Podcasts, kept.Voices
			d.at, d.shown = kept.At, true
		}
	}
	go func() {
		var wg sync.WaitGroup
		var songs []netease.Play
		var playlists []netease.Played[netease.Playlist]
		var albums []netease.Played[netease.AlbumProfile]
		var podcasts []netease.Played[netease.Podcast]
		var voices []netease.Played[netease.Voice]
		var songsErr, playlistsErr, albumsErr, podcastsErr, voicesErr error
		wg.Add(5)
		go func() { defer wg.Done(); songs, songsErr = recentPlays(recentLimit) }()
		go func() { defer wg.Done(); playlists, playlistsErr = recentPlaylists(recentLimit) }()
		go func() { defer wg.Done(); albums, albumsErr = recentAlbums(recentLimit) }()
		go func() { defer wg.Done(); podcasts, podcastsErr = recentPodcasts(recentLimit) }()
		go func() { defer wg.Done(); voices, voicesErr = recentVoices(recentLimit) }()
		wg.Wait()
		a.post(func() {
			if a.profile == nil || a.profile.UserID != uid || a.homeGen != gen {
				return
			}
			d.loading = false
			d.failure = songsErr
			if songsErr != nil {
				// What came is kept, and an empty page says why it is empty.
				if !d.shown {
					d.err = "最近播放加载失败"
				}
				return
			}
			d.err = ""
			d.songs = songs
			if playlistsErr == nil {
				d.playlists = playlists
			}
			if albumsErr == nil {
				d.albums = albums
			}
			if podcastsErr == nil {
				d.podcasts = podcasts
			}
			if voicesErr == nil {
				d.voices = voices
			}
			d.at, d.shown = a.listsNow(), true
			req.keep(cacheBytes(keptRecent{
				Songs:     d.songs,
				Playlists: d.playlists,
				Albums:    d.albums,
				Podcasts:  d.podcasts,
				Voices:    d.voices,
				At:        d.at,
			}))
		})
	}()
}

// freshenRecent loads the records again once they are a while old: the
// listener may have played something on another device since.
func (a *app) freshenRecent() {
	if a.profile != nil && !a.recent.loading && listDue(a.recent.at, a.listsNow()) {
		a.loadRecent()
	}
}

// What the recent page lists, in the order its tabs are in.
const (
	recentSongsTab = iota
	recentPlaylistsTab
	recentAlbumsTab
	recentPodcastsTab
	recentVoicesTab
)

var recentTabTitles = [...]string{"歌曲", "歌单", "专辑", "播客", "声音"}

func (a *app) recentPage(c *ui.Context) {
	if a.profile == nil {
		a.centered(c, "登录后查看最近播放", nil)
		return
	}
	a.freshenRecent()
	d := &a.recent
	if !d.shown {
		if d.loading {
			a.trackSkeleton(c, a.pageShimmer(c), false, func(c *ui.Context) { a.recentSkeletonHeader(c) })
			return
		}
		a.centered(c, d.err, a.loadRecent)
		return
	}
	header := func(c *ui.Context) { a.recentHeader(c) }
	switch a.recentTab {
	case recentPlaylistsTab:
		a.scrollPage(c, func() {
			header(c)
			if len(d.playlists) == 0 {
				a.empty(c, "最近没有听过歌单")
				return
			}
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.playlists {
					a.playlistCard(c.Key(d.playlists[i].Item.ID), &d.playlists[i].Item)
				}
			})
		})
	case recentAlbumsTab:
		a.scrollPage(c, func() {
			header(c)
			if len(d.albums) == 0 {
				a.empty(c, "最近没有听过专辑")
				return
			}
			albums := make([]netease.AlbumProfile, len(d.albums))
			for i := range d.albums {
				albums[i] = d.albums[i].Item
			}
			a.albumGrid(c, albums)
		})
	case recentPodcastsTab:
		a.scrollPage(c, func() {
			header(c)
			if len(d.podcasts) == 0 {
				a.empty(c, "最近没有听过播客")
				return
			}
			ui.Grid(c).Columns(a.columns(c, 340)).GapX(8).GapY(4).Margin(0, -12).Children(func() {
				for i := range d.podcasts {
					a.podcastRow(c.Key(d.podcasts[i].Item.ID), &d.podcasts[i].Item)
				}
			})
		})
	case recentVoicesTab:
		a.scrollPage(c, func() {
			header(c)
			if len(d.voices) == 0 {
				a.empty(c, "最近没有听过声音")
				return
			}
			for i := range d.voices {
				a.voiceRow(c, &d.voices[i])
			}
		})
	default:
		songs := make([]netease.Track, len(d.songs))
		meta := make([]string, len(d.songs))
		now := c.Now()
		for i := range d.songs {
			songs[i] = d.songs[i].Track
			meta[i] = playedAt(d.songs[i].At, d.songs[i].Device, now)
		}
		if len(songs) == 0 {
			a.scrollPage(c, func() {
				header(c)
				a.empty(c, "最近没有听过歌曲")
			})
			return
		}
		// The songs come from albums of their own: their covers say which.
		a.trackRowsMeta(c, songs, true, meta, header)
	}
}

// recentHeader is the top of the recent page: what it lists, and the tabs of
// the kinds of play record NetEase keeps.
func (a *app) recentHeader(c *ui.Context) {
	a.heading(c, "最近播放", "手机、电脑和网页版上听过的")
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, recentTabTitles[a.recentTab]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		segmented(c.Key("tab"), &a.recentTab, recentTabTitles[:]...)
	})
}

func (a *app) recentSkeletonHeader(c *ui.Context) {
	a.heading(c, "最近播放", "手机、电脑和网页版上听过的")
	ui.Text(c, recentTabTitles[recentSongsTab]).FontSize(19).FontWeight(700).LetterSpacing(-0.3).Margin(34, 0, 14, 0)
}

// voiceRow is an episode the listener played under 声音, which plays when
// clicked, under the podcast it is on.
func (a *app) voiceRow(c *ui.Context, v *netease.Played[netease.Voice]) {
	p := a.pal
	tr := v.Item.Program.Track(&v.Item.Podcast)
	row := ui.ButtonBase(c.Key(v.Item.Program.ID)).Height(54).Padding(0, 12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		a.cover(c, tr.Album.PicURL, 96, 38, 6)
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, tr.Name).FontSize(14).FontWeight(500).SingleLine()
			ui.Text(c, v.Item.Podcast.Name).FontSize(12.5).TextColor(p.muted).SingleLine()
		})
		ui.Text(c, playedAt(v.At, v.Device, c.Now())).FontSize(12.5).TextColor(p.faint).MinWidth(88).TextAlign(ui.End)
	})
	if row.Clicked() {
		a.pl.PlayQueue([]netease.Track{tr}, 0)
	}
}

// playedAt says when and on what the listener played something, as
// "12分钟前 · 手机".
func playedAt(at time.Time, device string, now time.Time) string {
	when := agoLabel(now.Sub(at))
	if where := deviceName(device); where != "" {
		return when + " · " + where
	}
	return when
}
