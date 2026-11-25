package app

import (
	"github.com/egoist/mygo/ui"

	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/desktop/view/shell"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

// Shared geometry remains available to feature views during their gradual migration.
const (
	sidebarWidth    = shell.SidebarWidth
	playerBarHeight = shell.PlayerBarHeight
	pageGap         = shell.PageGap
	buttonsHeight   = shell.ButtonsHeight
	topBarHeight    = shell.TopBarHeight
)

func (a *app) view(c *ui.Context) {
	t := a.settings.uiTheme(c.Theme())
	a.pal = paletteFor(&t)
	p := a.pal
	t.Background, t.Text = p.Background, p.Text
	t.Inverse, t.InverseText = p.Text, p.Background
	p.ApplyAccent(&t)
	t.Selection = p.Accent.Alpha(0.3)
	c.SetTheme(&t)

	defer a.recoveryFrame(c.WindowActive())
	a.shortcuts(c)
	active := c.WindowActive()
	if active && a.wasBehind {
		a.freshenLibrary()
	}
	a.wasBehind = !active
	a.retryLibraryRefresh()

	npOpen, npSettled := a.views.playback.NPState(c)
	a.art.PageFrame(a.nav.Current, !npOpen)
	home := a.pictures(a.settings.BackdropHome)
	var backdrop func(*ui.Context)
	if pic := playbackview.BackdropShown(home, a.pl.Current()); pic != nil {
		backdrop = func(c *ui.Context) {
			a.views.playback.HomeBackdrop(c, pic, float32(a.settings.BackdropHomeOpacity))
		}
	}
	a.shell.Build(c, shell.Layout{
		Palette: p, HasBackdrop: len(home) > 0, Covered: npOpen && npSettled,
		Navigation: a.shellNavigation(), BindRoot: a.bindInApp, Backdrop: backdrop,
		Sidebar: a.sidebar, Page: a.content, Player: a.views.playback.PlayerBar,
		NowPlaying: func(c *ui.Context) { a.views.playback.NowPlayingLayer(c, npOpen, npSettled) },
	})
	a.views.account.Login(c)
	a.views.library.NameDialog(c)
	a.views.library.DeleteDialog(c)
	a.intro.Dialog(c)
	a.views.local.LyricMatchDialog(c)
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	// Keep network controls off the player, including its closing transition.
	// Recovery still runs independently through network events and recoveryFrame.
	if !npOpen && npSettled {
		a.views.settings.CacheNotice(c)
		a.offlineNotice(c)
	}
	a.syncWindows()
}

func (a *app) shortcuts(c *ui.Context) {
	// The side buttons of a mouse, the keys of keyboards that have them
	// and swipes between pages, and the keys of Music.
	c.OnShortcut(0, ui.KeyBack, a.back)
	c.OnShortcut(0, ui.KeyForward, a.goForward)
	c.OnShortcut(ui.Cmd, ui.KeyBracketLeft, a.back)
	c.OnShortcut(ui.Cmd, ui.KeyBracketRight, a.goForward)
	c.OnShortcut(ui.Cmd, ui.KeyF, func() {
		a.playbackState.Open = false
		a.shell.SearchBox.Focus(c)
	})
	if a.playbackState.Open {
		c.OnShortcut(0, ui.KeyEscape, func() { a.playbackState.Open = false })
		// ⌘ and a number choose Now Playing's tab of that place.
		if a.pl.Current() != nil {
			for i, t := range a.views.playback.NPTabs() {
				c.OnShortcut(ui.Cmd, ui.Key1+ui.Key(i), func() { a.views.playback.ChoosePane(t.Pane) })
			}
		}
	}
}

// go opens a page, remembering the one before for the back button, and
// closes Now Playing over it, though it is the page open.
func (a *app) open(r route) {
	a.cancelMusicLink()
	a.playbackState.Open = false
	a.leavingPage(r)
	if !a.nav.Open(r) {
		a.load(r)
		return
	}
	a.displayPage()
}

// show shows the page of r.
func (a *app) leavingPage(r route) {
	if a.nav.Current.Page == pageCloud && r.Page != pageCloud {
		a.cloud.Locate = 0
	}
}

func (a *app) show(r route) {
	a.leavingPage(r)
	a.nav.Current = r
	a.displayPage()
}

func (a *app) displayPage() {
	a.intro.Open = false
	r := a.nav.Current
	if r.Page == pageSearch {
		a.searches.Activate(r)
	}
	a.load(r)
	a.trimPlaylistCache()
}

// load loads what the page of r shows, unless it has.
func (a *app) load(r route) {
	switch r.Page {
	case pageHome:
		a.freshenHome()
	case pagePlaylist:
		a.loadPlaylist(r.ID, false)
		a.freshenLikes()
	case pageSearch:
		a.loadSearch(r, false)
	case pageArtist:
		a.loadArtist(r.ID)
	case pageAlbum:
		a.loadAlbum(r.ID)
	case pageUser:
		a.loadUser(r.ID)
	case pagePodcast:
		a.loadPodcast(r.ID)
	case pagePodcasts:
		a.loadPodcastTab()
	case pageCloud:
		if !a.cloud.Loading() && a.cloud.Failure == nil && (!a.cloud.Loaded || viewmodel.ListDue(a.cloud.At, a.listsNow())) {
			a.refreshCloud()
		}
	case pageLocal:
		a.refreshLocalSongs()
	}
}

// back goes to the page before, as the back button of a mouse does, or
// closes Now Playing, which covers the pages.
func (a *app) back() {
	a.cancelMusicLink()
	if a.playbackState.Open {
		a.playbackState.Open = false
		return
	}
	if r, ok := a.nav.Previous(); ok {
		a.show(r)
	}
}

// goForward goes to the page back left, as the forward button of a mouse
// does.
func (a *app) goForward() {
	a.cancelMusicLink()
	if r, ok := a.nav.Next(); ok {
		a.playbackState.Open = false
		a.show(r)
	}
}

func (a *app) shellNavigation() shell.Navigation {
	return shell.Navigation{Current: a.nav.Current, LastSearch: &a.searches.Last,
		Playing: a.playbackState.Open, CanBack: len(a.nav.History) > 0, CanForward: len(a.nav.Forward) > 0,
		Open: a.open, Back: a.back, Forward: a.goForward}
}
func (a *app) sidebar(c *ui.Context) {
	a.shell.Sidebar(c, a.pal, a.shellNavigation(), shell.Sidebar{
		Search: a.views.search.Field, Account: a.views.account.Row,
		Library: func(c *ui.Context) {
			a.views.library.Playlists(c, "我的歌单", a.library.Mine, true, &a.shell.MineCollapsed)
			a.views.library.Playlists(c, "收藏的歌单", a.library.Saved, false, &a.shell.SavedCollapsed)
			a.views.library.Albums(c, a.library.SubAlbums, &a.shell.AlbumsCollapsed)
		},
	})
}

func (a *app) content(c *ui.Context) {
	// A new page starts at its top. Not as it is opened: a control in a
	// list, as a page's tabs in its header, opens it as the list builds.
	if a.scrolled != a.nav.Current {
		a.tracks.List, a.shell.PageScroll, a.scrolled = ui.ListState{}, ui.ScrollState{}, a.nav.Current
		a.tracks.InvalidateRows()
	}
	// Pages mark where their content is, as they scroll.
	a.shell.UnderTop, a.shell.UnderBottom = false, false
	switch a.nav.Current.Page {
	case pageHome:
		a.homePage(c)
	case pageDaily:
		a.views.daily.Build(c)
	case pageRecent:
		a.views.recent.Build(c)
	case pageCharts:
		a.chartsPage(c)
	case pagePlaylist:
		a.views.library.Build(c)
	case pageSearch:
		a.views.search.Build(c)
	case pageFM:
		a.views.radio.Build(c)
	case pageArtist:
		a.views.artist.Build(c)
	case pageAlbum:
		a.views.album.Build(c)
	case pageUser:
		a.views.user.Build(c)
	case pagePodcast:
		a.views.podcast.Build(c)
	case pagePodcasts:
		a.views.podcast.Home(c)
	case pageCloud:
		a.views.cloud.Build(c)
	case pageLocal:
		a.views.local.Build(c)
	}
}

// Loading what the pages show.

// freshenHome loads the home page again once NetEase has picked the day's
// songs anew since it last did: the app may stay open overnight.
func (a *app) freshenHome() { a.home.Freshen(a.homeServices()) }

// playlistDetail is what a playlist is asked of NetEase with.
var playlistDetail func(id int64) (*netease.Playlist, error) = netease.PlaylistDetail
