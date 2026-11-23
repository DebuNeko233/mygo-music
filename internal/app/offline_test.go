package app

import (
	"errors"
	"image"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func offlineFixture(t *testing.T) (*app, chan func()) {
	t.Helper()
	a, posts, _ := refreshFixture(t)
	a.browseCache = diskcache.New(filepath.Join(t.TempDir(), "browse"), 1<<20)
	a.identityCache = diskcache.New(filepath.Join(t.TempDir(), "identity"), 64<<10)
	a.account.Profile = &netease.Profile{UserID: 7, Nickname: "Offline"}
	t.Cleanup(func() { netease.SetOffline(false); a.browseCache.Close(); a.identityCache.Close() })
	return a, posts
}

// AlbumProfile omits Songs from API JSON: a disk-reloaded offline album must
// preserve its playable track list rather than displaying an empty album.
func TestOfflineAlbumRetainsSongsAcrossRestart(t *testing.T) {
	a, posts := offlineFixture(t)
	a.browseCache.Close()
	dir := t.TempDir()
	a.browseCache = diskcache.New(dir, 1<<20)
	ask := albumDetail
	t.Cleanup(func() { albumDetail = ask })
	al := &netease.AlbumProfile{Album: netease.Album{ID: 101, Name: "Cached album"}, Size: 2, Songs: []netease.Track{{ID: 1}, {ID: 2}}}
	albumDetail = func(int64) (*netease.AlbumProfile, error) { return al, nil }
	a.loadAlbum(101)
	refreshPost(t, posts)
	a.browseCache.Close() // Accepted snapshots must reach disk before restart.
	a.browseCache = diskcache.New(dir, 1<<20)
	a.albums.Reset()
	albumDetail = func(int64) (*netease.AlbumProfile, error) { return nil, netease.ErrOffline }
	a.loadAlbum(101)
	for a.albums.Pages.Get(101).Loading {
		refreshPost(t, posts)
	}
	got := a.albums.Pages.Get(101).Album
	if got == nil || got == al || len(got.Songs) != 2 || got.Songs[0].ID != 1 || got.Songs[1].ID != 2 || got.Size != 2 {
		t.Fatalf("offline album lost tracks: %+v", got)
	}
}

// A playlist appears from disk before a stalled network request completes,
// survives its failure, then refreshes in place without touching playback.
func TestOfflinePlaylistRecoversWithoutRestart(t *testing.T) {
	a, posts := offlineFixture(t)
	a.browseCache.Put("7", "playlist/9", cacheBytes(&netease.Playlist{ID: 9, Name: "Cached", Tracks: []netease.Track{{ID: 1}}}), 0)
	release := make(chan struct{})
	playlistDetail = func(id int64) (*netease.Playlist, error) { <-release; return nil, netease.ErrOffline }
	a.nav.Current = route{Page: pagePlaylist, ID: 9}
	a.loadPlaylist(9, false)
	refreshPost(t, posts)
	if a.library.Playlists[9] == nil || a.library.Playlists[9].Name != "Cached" {
		t.Fatal("network blocked disk content")
	}
	close(release)
	refreshPost(t, posts)
	if !a.playlistRequest(9).Failed || a.library.Playlists[9].Name != "Cached" {
		t.Fatal("failure erased cached page")
	}
	playlistDetail = func(id int64) (*netease.Playlist, error) { return &netease.Playlist{ID: id, Name: "Online"}, nil }
	a.recoverLoads()
	refreshPost(t, posts)
	if a.library.Playlists[9].Name != "Online" || a.playlistRequest(9).Failed || a.pl.Current() != nil {
		t.Fatal("did not recover page independently of playback")
	}
}

// A snapshot of another user and replies from a previous generation cannot
// become the current user's playlist.
func TestOfflinePlaylistAccountIsolation(t *testing.T) {
	a, posts := offlineFixture(t)
	a.browseCache.Put("8", "playlist/9", cacheBytes(&netease.Playlist{ID: 9, Name: "Private"}), 0)
	playlistDetail = func(id int64) (*netease.Playlist, error) { return &netease.Playlist{ID: id, Name: "Late"}, nil }
	a.loadPlaylist(9, false)
	a.account.Generation++
	a.account.Profile = &netease.Profile{UserID: 8}
	refreshPost(t, posts)
	if a.library.Playlists[9] != nil {
		t.Fatal("previous account leaked into new one")
	}
}

// Independent home responses retain failed modules while accepting a genuine empty result.
func TestOfflineHomePartialFailure(t *testing.T) {
	a, posts := offlineFixture(t)
	oldFeatured, oldCharts, oldDaily, oldRec := featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists
	t.Cleanup(func() {
		featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists = oldFeatured, oldCharts, oldDaily, oldRec
	})
	a.home.Featured = []netease.Playlist{{ID: 1, Name: "Keep"}}
	a.home.Daily = []netease.Track{{ID: 2}}
	chartsCalls, recCalls := 0, 0
	featuredPlaylists = func() ([]netease.Playlist, error) { return nil, netease.ErrOffline }
	dailySongs = func() ([]netease.Track, error) { return nil, netease.ErrOffline }
	homeCharts = func() ([]netease.Playlist, error) { chartsCalls++; return []netease.Playlist{{ID: 3}}, nil }
	recommendedPlaylists = func() ([]netease.Playlist, error) { recCalls++; return []netease.Playlist{}, nil }
	a.home.Recommended = []netease.Playlist{{ID: 4}}
	a.loadHome()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if len(a.home.Featured) != 1 || len(a.home.Daily) != 1 || a.home.Charts[0].ID != 3 || len(a.home.Recommended) != 0 || len(a.home.Failures) == 0 {
		t.Fatal("partial response cleared old content or hid a successful empty result")
	}
	featuredPlaylists = func() ([]netease.Playlist, error) { return []netease.Playlist{{ID: 5}}, nil }
	dailySongs = func() ([]netease.Track, error) { return []netease.Track{{ID: 6}}, nil }
	a.recoverLoads()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if len(a.home.Failures) > 0 || chartsCalls != 1 || recCalls != 1 || a.home.Daily[0].ID != 6 {
		t.Fatal("recovery retried healthy modules or failed to restore missing ones")
	}
}

// Network failure preserves the offline account and a later confirmation
// of the same account keeps page pointers and scroll positions unchanged.
func TestOfflineAccountConfirmationRecoversInPlace(t *testing.T) {
	a, posts := offlineFixture(t)
	ask, save, identity := accountAsk, saveLoginSession, loginIdentity
	t.Cleanup(func() { accountAsk, saveLoginSession, loginIdentity = ask, save, identity })
	saveLoginSession = func() error { return nil }
	loginIdentity = func() string { return "session-digest" }
	original := &netease.Playlist{ID: 9, Name: "Keep"}
	a.library.Playlists[9] = original
	a.library.Refresh.Mine.At, a.library.Refresh.Likes.At = a.listsNow(), a.listsNow()
	a.account.Pending = true
	accountAsk = func() (*netease.Profile, error) { return nil, errors.New("DNS failed") }
	a.confirmAccount()
	refreshPost(t, posts)
	if a.account.Profile == nil || a.account.Profile.UserID != 7 || !a.account.Pending {
		t.Fatal("transport failure signed out offline account")
	}
	accountAsk = func() (*netease.Profile, error) { return &netease.Profile{UserID: 7, Nickname: "Confirmed"}, nil }
	gen := a.account.Generation
	a.confirmAccount()
	refreshPost(t, posts)
	if a.account.Pending || a.account.Generation != gen || a.library.Playlists[9] != original {
		t.Fatal("confirmation reset the current pages")
	}
}

// Failed account confirmations keep cached pages until scheduled retries succeed without restarting the app.
func TestOfflineAccountScheduledRetriesRecover(t *testing.T) {
	a, posts := offlineFixture(t)
	ask, save, identity, after := accountAsk, saveLoginSession, loginIdentity, recoveryAfter
	var callbacks []func()
	var timers []*time.Timer
	t.Cleanup(func() {
		accountAsk, saveLoginSession, loginIdentity, recoveryAfter = ask, save, identity, after
		for _, timer := range timers {
			timer.Stop()
		}
	})
	saveLoginSession = func() error { return nil }
	loginIdentity = func() string { return "" }
	recoveryAfter = func(_ time.Duration, fn func()) *time.Timer {
		callbacks = append(callbacks, fn)
		timer := time.NewTimer(time.Hour)
		timers = append(timers, timer)
		return timer
	}
	calls := 0
	accountAsk = func() (*netease.Profile, error) {
		calls++
		if calls < 3 {
			return nil, &net.DNSError{Err: "temporary failure", IsTemporary: true}
		}
		return &netease.Profile{UserID: 7, Nickname: "Confirmed"}, nil
	}
	original := &netease.Playlist{ID: 9, Name: "Cached"}
	a.library.Playlists[9] = original
	a.library.Refresh.Mine.At, a.library.Refresh.Likes.At = a.listsNow(), a.listsNow()
	a.recovery.active, a.account.Pending = true, true
	gen := a.account.Generation
	a.confirmAccount()
	refreshPost(t, posts)
	for attempt := 1; attempt < 3; attempt++ {
		if calls != attempt || a.account.Profile == nil || a.account.Profile.UserID != 7 || !a.account.Pending || a.account.Busy || a.library.Playlists[9] != original {
			t.Fatal("failed confirmation discarded the account or cached page")
		}
		a.recoveryFrame(true)
		if a.recovery.timer == nil || len(callbacks) != attempt {
			t.Fatal("failed account confirmation did not schedule one retry")
		}
		callbacks[attempt-1]()
		refreshPost(t, posts) // The timer starts confirmation on the UI queue.
		refreshPost(t, posts) // The request publishes its answer on that queue.
	}
	a.recoveryFrame(true)
	if calls != 3 || a.account.Pending || a.account.Busy || a.recovery.timer != nil || a.account.Generation != gen || a.account.Profile == nil || a.account.Profile.Nickname != "Confirmed" || a.library.Playlists[9] != original {
		t.Fatal("scheduled confirmation failed to recover in place or kept retrying")
	}
}

// Failed visible artwork can be retried, but navigating away does not start
// downloads for every old failure ever seen.
func TestOfflineArtworkRetryTracksVisibleRows(t *testing.T) {
	c := newArtCache(func(func()) {})
	c.add("shown", &artwork{failed: true, retryable: true})
	c.add("hidden", &artwork{failed: true, retryable: true})
	c.beginFrame()
	c.get("shown", false)
	if !c.hasFailed(false) {
		t.Fatal("visible failure was forgotten")
	}
	c.retryFailed(false)
	if c.art["shown"] != nil || c.art["hidden"] == nil {
		t.Fatal("retried hidden covers or retained visible failure")
	}
	c.beginFrame()
	if c.hasFailed(false) {
		t.Fatal("offscreen failures keep recovery alive")
	}
}

// A healthy offline-enabled page schedules neither retry timers nor idle frames.
func TestOfflineIdleFrameBudget(t *testing.T) {
	a, _ := offlineFixture(t)
	a.nav.Current = route{Page: pagePlaylist, ID: 9}
	a.library.Playlists[9] = &netease.Playlist{ID: 9, Name: "Ready"}
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	if tt.FramePending() || a.recovery.timer != nil {
		t.Fatal("healthy idle page polls or repaints")
	}
}

// Cached lyrics are available without any successful request to the lyrics service.
func TestOfflineLyricsFromDisk(t *testing.T) {
	a, _ := offlineFixture(t)
	old := lyricsRequest
	t.Cleanup(func() { lyricsRequest = old })
	lyricsRequest = func(int64) (string, string, string, error) {
		t.Error("requested cached lyrics")
		return "", "", "", netease.ErrOffline
	}
	a.browseCache.Put("7", "lyrics/1", cacheBytes(keptLyrics{Original: "[00:01]Cached", Translation: "translation", Words: "words"}), 0)
	l, tr, w, err := a.cachedLyricsFor("7")(1, nil)
	if err != nil || l != "[00:01]Cached" || tr != "translation" || w != "words" {
		t.Fatal("did not restore all lyric formats")
	}
}

func fakeOfflineHome(t *testing.T) {
	t.Helper()
	f, c, d, r := featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists
	t.Cleanup(func() { featuredPlaylists, homeCharts, dailySongs, recommendedPlaylists = f, c, d, r })
	fail := func() ([]netease.Playlist, error) { return nil, netease.ErrOffline }
	featuredPlaylists, homeCharts, recommendedPlaylists = fail, fail, fail
	dailySongs = func() ([]netease.Track, error) { return nil, netease.ErrOffline }
}

// Offline boot restores only the account bound to the actual saved session,
// and renders its sidebar without waiting for the account request to finish.
func TestOfflineBootUsesBoundIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, identity     string
		matched, malformed bool
	}{
		{name: "matching", identity: "bound", matched: true},
		{name: "other-session", identity: "other"},
		{name: "malformed", identity: "bound", malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, posts := offlineFixture(t)
			fakeOfflineHome(t)
			restore, identity, ask := restoreLoginSession, loginIdentity, accountAsk
			t.Cleanup(func() { restoreLoginSession, loginIdentity, accountAsk = restore, identity, ask })
			restoreLoginSession = func() bool { return true }
			loginIdentity = func() string { return tc.identity }
			release := make(chan struct{})
			accountAsk = func() (*netease.Profile, error) { <-release; return nil, netease.ErrOffline }
			userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) { return nil, nil, netease.ErrOffline }
			likedIDs = func(int64) ([]int64, error) { return nil, netease.ErrOffline }
			profile := cacheBytes(a.account.Profile)
			if tc.malformed {
				profile = []byte(`{"userId":7,"nickname":123}`)
			}
			a.identityCache.Put("bound", "profile", profile, 0)
			a.browseCache.Put("7", "mine", cacheBytes(library.Lists{Mine: []netease.Playlist{{ID: 9, Name: "Offline list"}}}), 0)
			a.account.Profile = nil
			a.boot()
			refreshPost(t, posts)
			if tc.matched {
				for a.library.Mine == nil {
					refreshPost(t, posts)
				}
				if a.account.Profile == nil || a.account.Profile.UserID != 7 || a.library.Mine[0].ID != 9 || !a.account.Busy {
					t.Fatal("account confirmation blocked cached sidebar")
				}
			} else if a.account.Profile != nil || a.library.Mine != nil {
				t.Fatal("restored an unrelated or malformed offline profile")
			}
			close(release)
			for a.home.Loading || a.account.Busy || a.library.Refresh.Mine.Busy || a.library.Refresh.Likes.Busy {
				refreshPost(t, posts)
			}
			if !a.account.Pending {
				t.Fatal("network error was accepted as an account answer")
			}
		})
	}
}

// An explicit login expiry clears personal page state instead of retaining an
// unverified account indefinitely; guest home requests are allowed to finish.
func TestOfflineExpiredAccountRequiresLogin(t *testing.T) {
	a, posts := offlineFixture(t)
	fakeOfflineHome(t)
	ask, clear := accountAsk, clearLoginSession
	t.Cleanup(func() { accountAsk, clearLoginSession = ask, clear })
	cleared := false
	clearLoginSession = func() error {
		cleared = true
		return nil
	}
	accountAsk = func() (*netease.Profile, error) { return nil, netease.ErrNotLoggedIn }
	a.library.Mine = []netease.Playlist{{ID: 9}}
	a.library.Playlists[9] = &netease.Playlist{ID: 9}
	a.account.Pending = true
	a.confirmAccount()
	refreshPost(t, posts)
	if !cleared || a.account.Profile != nil || a.library.Mine != nil || len(a.library.Playlists) != 0 || a.account.Pending {
		t.Fatal("expired identity remained visible")
	}
	for a.home.Loading {
		refreshPost(t, posts)
	}
}

// Recovery backs off across failed requests, sleeps during each request and
// stops when the window hides; a cancelled callback cannot restart a load.
func TestOfflineRecoveryBackoffAndHiddenWindow(t *testing.T) {
	a, posts := offlineFixture(t)
	a.nav.Current = route{Page: pagePlaylist, ID: 9}
	a.library.Playlists[9] = &netease.Playlist{ID: 9}
	a.playlistRequest(9).Failed = true
	a.playlistRequest(9).Failure = netease.ErrOffline
	a.recovery.active = true
	after := recoveryAfter
	var callbacks []func()
	var gaps []time.Duration
	var timers []*time.Timer
	recoveryAfter = func(gap time.Duration, fn func()) *time.Timer {
		callbacks = append(callbacks, fn)
		gaps = append(gaps, gap)
		timer := time.NewTimer(time.Hour)
		timers = append(timers, timer)
		return timer
	}
	t.Cleanup(func() {
		recoveryAfter = after
		for _, timer := range timers {
			timer.Stop()
		}
	})
	playlistDetail = func(id int64) (*netease.Playlist, error) { return nil, netease.ErrOffline }
	a.recoveryFrame(true)
	if len(gaps) != 1 || gaps[0] != 2*time.Second {
		t.Fatal("wrong initial retry")
	}
	callbacks[0]()
	refreshPost(t, posts)
	a.recoveryFrame(true)
	if a.recovery.timer != nil || a.recovery.attempts != 1 {
		t.Fatal("request in flight reset backoff or created a timer")
	}
	refreshPost(t, posts)
	a.recoveryFrame(true)
	if len(gaps) != 2 || gaps[1] != 5*time.Second {
		t.Fatal("retry did not back off")
	}
	a.recoveryFrame(false)
	callbacks[1]()
	refreshPost(t, posts)
	if a.recovery.timer != nil || a.playlistRequest(9).Busy {
		t.Fatal("hidden window resumed a cancelled retry")
	}
}

// All previously visited album pages remain browsable if validating page zero fails.
func TestOfflineArtistRestoresCachedPages(t *testing.T) {
	a, posts := offlineFixture(t)
	old := albumsBy
	t.Cleanup(func() { albumsBy = old })
	a.browseCache.Put("7", "artist-albums/1/0", cacheBytes(viewmodel.KeptArtistAlbums{Albums: []netease.AlbumProfile{{ID: 10}}, More: true}), 0)
	a.browseCache.Put("7", "artist-albums/1/1", cacheBytes(viewmodel.KeptArtistAlbums{Albums: []netease.AlbumProfile{{ID: 11}}, More: false}), 0)
	albumsBy = func(int64, int, int) ([]netease.AlbumProfile, bool, error) { return nil, false, netease.ErrOffline }
	a.loadAlbums(1)
	refreshPost(t, posts)
	refreshPost(t, posts)
	d := a.artist(1)
	if len(d.Albums) != 2 || d.Albums[1].ID != 11 || !d.AlbumsFailed || d.AlbumsPage.More {
		t.Fatal("cached pages beyond failed first page became inaccessible")
	}
}

// A stale cover load finishing after invalidation cannot undo a newer load.
func TestOfflineArtworkInvalidationRejectsOldLoad(t *testing.T) {
	loader := loadArt
	t.Cleanup(func() { loadArt = loader })
	posts := make(chan func(), 4)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
		// Downloads are started in a controlled order below.
		n := calls.Add(1)
		entered <- struct{}{}
		if n == 1 {
			<-release
		}
		return nil, nil, 0, errors.New("bad cover")
	}
	c := newArtCache(func(f func()) { posts <- f })
	c.get("cover", false)
	<-entered
	c.invalidate("cover", false)
	c.get("cover", false)
	<-entered
	refreshPost(t, posts)
	fresh := c.art["cover"]
	close(release)
	refreshPost(t, posts)
	if c.art["cover"] != fresh {
		t.Fatal("invalidated load replaced its successor")
	}
}
