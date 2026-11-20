package app

import (
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
	tabs            [recentTabCount]recentTabState
	snapshotChecked bool
	readingSnapshot bool
	snapshotDirty   bool
	songs           []netease.Play
	// The rows use tracks drawn from the record when it arrives.
	tracks    []netease.Track
	playlists []netease.Played[netease.Playlist]
	albums    []netease.Played[netease.AlbumProfile]
	podcasts  []netease.Played[netease.Podcast]
	voices    []netease.Played[netease.Voice]
}

type recentTabState struct {
	loading bool
	shown   bool
	failure error
	at      time.Time // when the last request finished
}

func (d *recentData) loading() bool {
	if d.readingSnapshot {
		return true
	}
	for _, tab := range d.tabs {
		if tab.loading {
			return true
		}
	}
	return false
}

// setSongs takes the songs of the record, and the tracks its rows list.
func (d *recentData) setSongs(songs []netease.Play) {
	d.songs = songs
	d.tracks = make([]netease.Track, len(songs))
	for i := range songs {
		d.tracks[i] = songs[i].Track
	}
}

// keptRecent is what the page keeps of the records between runs.
type keptRecent struct {
	Songs     []netease.Play
	Playlists []netease.Played[netease.Playlist]
	Albums    []netease.Played[netease.AlbumProfile]
	Podcasts  []netease.Played[netease.Podcast]
	Voices    []netease.Played[netease.Voice]
	At        time.Time
	Tabs      uint8 // one bit per available tab; legacy snapshots have no mask
}

// loadRecent refreshes the five records concurrently. Each tab can display
// its own result as soon as it arrives.
func (a *app) loadRecent() {
	d := &a.recent
	if a.profile == nil || d.readingSnapshot {
		return
	}
	if !d.snapshotChecked {
		d.snapshotChecked = true
		if req := a.cacheRequest("recent"); req.store != nil {
			d.readingSnapshot = true
			gen := a.homeGen
			go func() {
				var kept keptRecent
				at := req.read(&kept)
				a.post(func() {
					if a.homeGen != gen {
						return
					}
					d.readingSnapshot = false
					if !at.IsZero() && a.browseCache == req.store && req.store.Epoch() == req.epoch {
						d.setSongs(kept.Songs)
						d.playlists, d.albums = kept.Playlists, kept.Albums
						d.podcasts, d.voices = kept.Podcasts, kept.Voices
						for i := range d.tabs {
							d.tabs[i].shown = kept.Tabs == 0 || kept.Tabs&(1<<i) != 0
						}
					}
					a.loadRecent()
				})
			}()
			return
		}
	}
	now := a.listsNow()
	for i, tab := range d.tabs {
		if !tab.loading && listDue(tab.at, now) {
			a.loadRecentTab(i)
		}
	}
}

func (a *app) loadRecentTab(tab int) {
	d := &a.recent
	if a.profile == nil || d.readingSnapshot || d.tabs[tab].loading {
		return
	}
	switch tab {
	case recentSongsTab:
		fetchRecent(a, tab, recentPlays, d.setSongs)
	case recentPlaylistsTab:
		fetchRecent(a, tab, recentPlaylists, func(v []netease.Played[netease.Playlist]) { d.playlists = v })
	case recentAlbumsTab:
		fetchRecent(a, tab, recentAlbums, func(v []netease.Played[netease.AlbumProfile]) { d.albums = v })
	case recentPodcastsTab:
		fetchRecent(a, tab, recentPodcasts, func(v []netease.Played[netease.Podcast]) { d.podcasts = v })
	case recentVoicesTab:
		fetchRecent(a, tab, recentVoices, func(v []netease.Played[netease.Voice]) { d.voices = v })
	}
}

func fetchRecent[T any](a *app, tab int, fetch func(int) ([]T, error), apply func([]T)) {
	d := &a.recent
	d.tabs[tab].loading = true
	gen, req := a.homeGen, a.cacheRequest("recent")
	go func() {
		items, err := fetch(recentLimit)
		a.post(func() {
			if a.homeGen != gen {
				return
			}
			state := &d.tabs[tab]
			state.loading, state.failure, state.at = false, err, a.listsNow()
			if err == nil {
				apply(items)
				state.shown = true
				d.snapshotDirty = true
			}
			// Coalesce the concurrent replies into one snapshot. A failure
			// preserves any previously available records for that tab.
			if d.snapshotDirty && !d.loading() {
				d.snapshotDirty = false
				if req.store != nil {
					d.keep(req, state.at)
				}
			}
		})
	}()
}

func (d *recentData) keep(req cacheRequest, at time.Time) {
	var tabs uint8
	for i, tab := range d.tabs {
		if tab.shown {
			tabs |= 1 << i
		}
	}
	req.keep(cacheBytes(keptRecent{
		Songs:     d.songs,
		Playlists: d.playlists,
		Albums:    d.albums,
		Podcasts:  d.podcasts,
		Voices:    d.voices,
		At:        at,
		Tabs:      tabs,
	}))
}

// What the recent page lists, in the order its tabs are in.
const (
	recentSongsTab = iota
	recentPlaylistsTab
	recentAlbumsTab
	recentPodcastsTab
	recentVoicesTab
	recentTabCount
)

var recentTabTitles = [...]string{"歌曲", "歌单", "专辑", "播客", "声音"}

func (a *app) recentPage(c *ui.Context) {
	if a.profile == nil {
		a.centered(c, "登录后查看最近播放", nil)
		return
	}
	a.loadRecent()
	d := &a.recent
	tab := a.recentTab
	state := &d.tabs[tab]
	header := func(c *ui.Context) { a.recentHeader(c) }
	if !state.shown {
		if d.readingSnapshot || state.loading {
			a.trackSkeleton(c, a.pageShimmer(c), false, header)
			return
		}
		a.scrollPage(c, func() {
			header(c)
			a.centered(c, "最近播放加载失败", func() { a.loadRecentTab(tab) })
		})
		return
	}
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
			a.podcastGrid(c, len(d.podcasts), func(i int) *netease.Podcast { return &d.podcasts[i].Item })
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
		if len(d.tracks) == 0 {
			a.scrollPage(c, func() {
				header(c)
				a.empty(c, "最近没有听过歌曲")
			})
			return
		}
		// When and where each song was played, as its row is built: a
		// frame builds the rows in view, not the hundreds of the record.
		now := c.Now()
		played := func(i int) string { return playedAt(d.songs[i].At, d.songs[i].Device, now) }
		// The songs come from albums of their own: their covers say which.
		a.trackRowsMeta(c, d.tracks, true, played, header)
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
