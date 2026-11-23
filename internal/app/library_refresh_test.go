package app

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func refreshFixture(t *testing.T) (*app, chan func(), *time.Time) {
	t.Helper()
	isolateUserData(t)
	oldDetail, oldMine, oldLikes := playlistDetail, userPlaylists, likedIDs
	t.Cleanup(func() { playlistDetail, userPlaylists, likedIDs = oldDetail, oldMine, oldLikes })
	a := newTestApp()
	posts := testPosts(a)
	a.settings = defaultSettings()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.library.Refresh.Now = func() time.Time { return now }
	return a, posts, &now
}
func refreshPost(t *testing.T, posts chan func()) {
	t.Helper()
	select {
	case f := <-posts:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("refresh never posted")
	}
}

// A matching refresh keeps the displayed playlist and its disk snapshot,
// while an absent snapshot can still be restored for offline use.
func TestPlaylistRefreshKeepsUnchangedSnapshot(t *testing.T) {
	a, posts, now := refreshFixture(t)
	a.browseCache = diskcache.New(t.TempDir(), 64<<20)
	t.Cleanup(a.browseCache.Close)
	pl := &netease.Playlist{ID: 7, Name: "歌单", Tracks: []netease.Track{{ID: 1, Name: "歌曲"}}}
	a.library.Playlists[pl.ID] = pl
	req := a.cacheRequest("playlist/7")
	playlistDetail = func(int64) (*netease.Playlist, error) {
		result := copyPlaylist(*pl)
		return &result, nil
	}

	a.loadPlaylist(pl.ID, false)
	refreshPost(t, posts)
	a.waitForPlaylistSnapshot(req)
	var stored netease.Playlist
	written := req.Read(&stored)
	if written.IsZero() || !reflect.DeepEqual(*pl, stored) {
		t.Fatal("matching refresh did not fill a missing offline snapshot")
	}
	*now = now.Add(viewmodel.ListsFresh)
	a.loadPlaylist(pl.ID, false)
	refreshPost(t, posts)
	a.waitForPlaylistSnapshot(req)
	if again := req.Read(&stored); !again.Equal(written) {
		t.Fatal("matching refresh rewrote the offline snapshot")
	}
	if a.library.Playlists[pl.ID] != pl || a.playlistRequest(pl.ID).Busy {
		t.Fatal("matching refresh replaced the displayed list or stayed busy")
	}
}

// Header-only refreshes keep the track data, while new song privileges still
// reach the list even when its IDs and order have not changed.
func TestPlaylistRefreshAppliesHeaderAndPrivileges(t *testing.T) {
	a, posts, now := refreshFixture(t)
	pl := &netease.Playlist{ID: 7, Name: "歌单", Tracks: []netease.Track{{ID: 1, Name: "歌曲"}}}
	a.library.Playlists[pl.ID] = pl
	header := copyPlaylist(*pl)
	header.PlayCount = 10
	withPrivilege := copyPlaylist(header)
	withPrivilege.Tracks[0].Privilege = &netease.Privilege{ID: 1, St: -1}

	playlistDetail = func(int64) (*netease.Playlist, error) { return &header, nil }
	a.loadPlaylist(pl.ID, false)
	refreshPost(t, posts)
	got := a.library.Playlists[pl.ID]
	if got.PlayCount != 10 || &got.Tracks[0] != &pl.Tracks[0] {
		t.Fatal("header refresh lost the new count or replaced unchanged tracks")
	}
	*now = now.Add(viewmodel.ListsFresh)
	playlistDetail = func(int64) (*netease.Playlist, error) { return &withPrivilege, nil }
	a.loadPlaylist(pl.ID, false)
	refreshPost(t, posts)
	if a.library.Playlists[pl.ID].Tracks[0].Playable() || !pl.Tracks[0].Playable() {
		t.Fatal("privilege refresh was lost or changed the old track")
	}
}

// A refresh keeps the top song and its inset when it survives a nearby edit,
// falls back to a surviving song after deletion, and leaves playback alone.
func TestPlaylistRefreshKeepsScrollAndQueue(t *testing.T) {
	for _, edit := range []string{"insert above", "remove above", "reorder", "long move", "remove top", "duplicate"} {
		t.Run(edit, func(t *testing.T) {
			a, posts, _ := refreshFixture(t)
			const id = 7
			pl := &netease.Playlist{ID: id, Name: "歌单", Tracks: make([]netease.Track, 1600)}
			for i := range pl.Tracks {
				pl.Tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("歌曲 %04d", i+1), Duration: 200000}
			}
			// Distinct rows can name the same recording. Test following the
			// second occurrence, with the first above the visible region.
			if edit == "duplicate" {
				for i := 240; i < 260; i++ {
					pl.Tracks[i].ID = pl.Tracks[i-200].ID
				}
			}
			a.nav.Current = route{Page: pagePlaylist, ID: id}
			a.library.Playlists[id] = pl
			a.pl.Cue(pl.Tracks)
			a.pl.Hold(43 * time.Second)
			queue := slices.Clone(a.pl.Queue())
			tt := ui.NewTester(a.view, 1100, 760)
			tt.Frame()
			a.trackList.ScrollTo(249, ui.Start)
			tt.Frame()
			tt.Scroll(750, 400, 0, 57)
			tt.Advance(time.Second)
			tt.Frame()
			first, _ := a.trackList.Visible()
			if first < 241 || first > 260 {
				t.Fatalf("test did not scroll into the target range: first row %d", first)
			}
			before, ok := tt.AccessByRole(ui.RoleButton, pl.Tracks[first-1].Name)
			if !ok {
				t.Fatal("test top song was not built")
			}
			fresh := copyPlaylist(*pl)
			want := pl.Tracks[first-1].Name
			switch edit {
			case "insert above", "duplicate":
				fresh.Tracks = slices.Insert(fresh.Tracks, 1, netease.Track{ID: 2000, Name: "新增歌曲"})
			case "remove above":
				fresh.Tracks = slices.Delete(fresh.Tracks, 1, 2)
			case "reorder":
				fresh.Tracks[first-1], fresh.Tracks[first+18] = fresh.Tracks[first+18], fresh.Tracks[first-1]
			case "long move":
				fresh.Tracks[first-1], fresh.Tracks[1400] = fresh.Tracks[1400], fresh.Tracks[first-1]
			case "remove top":
				want = pl.Tracks[first].Name
				fresh.Tracks = slices.Delete(fresh.Tracks, first-1, first)
			}
			playlistDetail = func(int64) (*netease.Playlist, error) { return &fresh, nil }
			a.loadPlaylist(id, false)
			refreshPost(t, posts)
			tt.Frame()
			tt.Frame()
			first, _ = a.trackList.Visible()
			if edit == "long move" || edit == "remove top" {
				locatedRow(t, tt, want, 760)
			} else {
				if first < 1 || fresh.Tracks[first-1].Name != want {
					t.Fatalf("refresh did not keep %q at the top: first row %d", want, first)
				}
				after, ok := tt.AccessByRole(ui.RoleButton, want)
				if !ok || after.Bounds.Y != before.Bounds.Y {
					t.Fatalf("refresh changed the top song's inset: %g -> %g", before.Bounds.Y, after.Bounds.Y)
				}
			}
			if !reflect.DeepEqual(queue, a.pl.Queue()) || a.pl.Current().ID != queue[0].ID ||
				!a.pl.Playing() || a.pl.Position() != 43*time.Second {
				t.Fatal("refresh changed the active queue or playback")
			}
		})
	}
}

// Expiry is checked on navigation/focus, requests coalesce, and a failure
// retains old content and observes the same retry interval instead of looping.
func TestLibraryRefreshExpiryAndFailure(t *testing.T) {
	a, posts, now := refreshFixture(t)
	old := &netease.Playlist{ID: 7, Name: "Old"}
	a.library.Playlists[7] = old
	var calls atomic.Int32
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("offline")
		}
		return &netease.Playlist{ID: id, Name: "New"}, nil
	}
	a.loadPlaylist(7, false)
	a.loadPlaylist(7, false)
	refreshPost(t, posts)
	if calls.Load() != 1 || a.library.Playlists[7] != old || a.library.Errors[7] != "" {
		t.Fatal("failed refresh lost content or duplicated requests")
	}
	a.loadPlaylist(7, false)
	*now = now.Add(viewmodel.ListsFresh - time.Second)
	a.loadPlaylist(7, false)
	if calls.Load() != 1 {
		t.Fatal("retried before the freshness interval")
	}
	*now = now.Add(time.Second)
	a.loadPlaylist(7, false)
	a.loadPlaylist(7, false)
	refreshPost(t, posts)
	if calls.Load() != 2 || a.library.Playlists[7].Name != "New" {
		t.Fatal("expired playlist did not refresh exactly once")
	}
}

// Renaming, editing tracks in place, replacing or forgetting a cached list
// during revalidation must take priority over the older server response.
func TestLibraryRefreshLocalPlaylistEditWins(t *testing.T) {
	for _, edit := range []string{"rename", "tracks", "subscription", "replace", "forget"} {
		t.Run(edit, func(t *testing.T) {
			a, posts, _ := refreshFixture(t)
			original := &netease.Playlist{ID: 7, Name: "Before", Tracks: []netease.Track{{ID: 1}}}
			a.library.Playlists[7] = original
			playlistDetail = func(id int64) (*netease.Playlist, error) { return &netease.Playlist{ID: id, Name: "Server"}, nil }
			a.loadPlaylist(7, false)
			switch edit {
			case "rename":
				original.Name = "Local"
			case "tracks":
				original.Tracks[0].ID = 2
			case "subscription":
				original.Subscribed = true
			case "replace":
				a.library.Playlists[7] = &netease.Playlist{ID: 7, Name: "Local"}
			case "forget":
				a.forgetPlaylist(7)
			}
			refreshPost(t, posts)
			if got := a.library.Playlists[7]; got != nil && got.Name == "Server" {
				t.Fatal("stale response overwrote a local edit")
			}
			if edit == "forget" && a.library.Playlists[7] != nil {
				t.Fatal("forgotten playlist was restored")
			}
		})
	}
}

// Cancelling an initial load and starting a replacement cannot let the old
// completion clear the replacement's busy flag or publish its stale content.
func TestLibraryRefreshForgottenLoadCannotFinishNewLoad(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	entered := make(chan int32, 2)
	oldDone, newDone := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		n := calls.Add(1)
		entered <- n
		if n == 1 {
			<-oldDone
		} else {
			<-newDone
		}
		return &netease.Playlist{ID: id, Name: map[bool]string{true: "Old", false: "New"}[n == 1]}, nil
	}
	a.loadPlaylist(7, false)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initial load missing")
	}
	a.forgetPlaylist(7)
	a.loadPlaylist(7, false)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("replacement load missing")
	}
	close(oldDone)
	refreshPost(t, posts)
	if !a.library.Loading[7] || !a.playlistRequest(7).Busy || a.library.Playlists[7] != nil {
		t.Fatal("old completion finished the new load")
	}
	close(newDone)
	refreshPost(t, posts)
	if a.library.Loading[7] || a.library.Playlists[7].Name != "New" {
		t.Fatal("replacement did not finish")
	}
}

// Returning to the foreground refreshes the sidebar, liked IDs and visible
// list once, retaining all old values until their answers arrive.
func TestLibraryRefreshOnWindowActivation(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.library.Mine = []netease.Playlist{{ID: 7, Name: "Old"}}
	a.library.Liked = map[int64]bool{1: true}
	a.library.Playlists[7] = &netease.Playlist{ID: 7, Name: "Old"}
	a.nav.Current = route{Page: pagePlaylist, ID: 7}
	var mine, likes, detail atomic.Int32
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
		mine.Add(1)
		return []netease.Playlist{{ID: 7, Name: "New"}}, nil, nil
	}
	likedIDs = func(int64) ([]int64, error) { likes.Add(1); return []int64{2}, nil }
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		detail.Add(1)
		return &netease.Playlist{ID: id, Name: "New"}, nil
	}
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetWindowActive(false)
	tt.Frame()
	tt.SetWindowActive(true)
	tt.Frame()
	tt.Frame()
	if a.library.Mine[0].Name != "Old" || !a.library.Liked[1] || a.library.Playlists[7].Name != "Old" {
		t.Fatal("refresh blanked existing content")
	}
	for range 3 {
		refreshPost(t, posts)
	}
	if a.library.Mine[0].Name != "New" || !a.library.Liked[2] || a.library.Liked[1] || a.library.Playlists[7].Name != "New" {
		t.Fatal("foreground refresh did not update personal content")
	}
	tt.SetWindowActive(false)
	tt.Frame()
	tt.SetWindowActive(true)
	tt.Frame()
	if mine.Load() != 1 || likes.Load() != 1 || detail.Load() != 1 {
		t.Fatal("focus changes bypassed the freshness interval")
	}
}

// A visible liked playlist refreshes both its songs and the hearts even
// when the listener navigates back without switching to another application.
func TestLibraryRefreshLikedPlaylistOnReopen(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.library.Liked = map[int64]bool{1: true}
	a.library.Playlists[7] = &netease.Playlist{ID: 7, SpecialType: 5, Tracks: []netease.Track{{ID: 1}}}
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		return &netease.Playlist{ID: id, SpecialType: 5, Tracks: []netease.Track{{ID: 2}}}, nil
	}
	likedIDs = func(int64) ([]int64, error) { return []int64{2}, nil }
	a.open(route{Page: pagePlaylist, ID: 7})
	refreshPost(t, posts)
	refreshPost(t, posts)
	if !a.library.Liked[2] || a.library.Liked[1] || a.library.Playlists[7].Tracks[0].ID != 2 {
		t.Fatal("liked page and hearts stayed stale")
	}
}

// Personal list refreshes must not overwrite optimistic edits made while
// the request is in flight, including changes to a playlist's name in place.
func TestLibraryRefreshPersonalEditsWin(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.library.Mine = []netease.Playlist{{ID: 7, Name: "Before"}}
	a.library.Liked = map[int64]bool{1: true}
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
		return []netease.Playlist{{ID: 7, Name: "Server"}}, nil, nil
	}
	likedIDs = func(int64) ([]int64, error) { return []int64{3}, nil }
	a.loadMine(1)
	a.loadLiked(1)
	a.library.Mine[0].Name = "Local"
	a.library.SetLiked(2, true)
	refreshPost(t, posts)
	refreshPost(t, posts)
	if a.library.Mine[0].Name != "Local" || !a.library.Liked[2] || a.library.Liked[3] {
		t.Fatal("refresh overwrote local personal edits")
	}
}

// A response from an earlier account epoch must not publish or clear the
// current request even after signing back in as the original UID (A-B-A).
func TestLibraryRefreshAccountEpoch(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	a.library.Mine = []netease.Playlist{{ID: 7, Name: "Current"}}
	a.library.Liked = map[int64]bool{9: true}
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
		return []netease.Playlist{{ID: 7, Name: "Stale"}}, nil, nil
	}
	likedIDs = func(int64) ([]int64, error) { return []int64{3}, nil }
	playlistDetail = func(id int64) (*netease.Playlist, error) { return &netease.Playlist{ID: id, Name: "Stale"}, nil }
	a.loadMine(1)
	a.loadLiked(1)
	a.loadPlaylist(7, false)
	// This is the invalidation performed by signedIn, without launching its
	// unrelated home/account requests in this isolated regression.
	a.account.Profile = &netease.Profile{UserID: 2}
	a.account.Generation++
	a.resetLibraryFreshness()
	a.account.Profile = &netease.Profile{UserID: 1}
	a.account.Generation++
	a.resetLibraryFreshness()
	for range 3 {
		refreshPost(t, posts)
	}
	if a.library.Mine[0].Name != "Current" || !a.library.Liked[9] || a.library.Liked[3] || a.library.Playlists[7] != nil {
		t.Fatal("previous account's data returned")
	}
}

// Starting a refresh during an optimistic edit must not reintroduce an item
// being deleted, even if the edit finishes before the refresh's reply lands.
func TestLibraryRefreshPendingEditWins(t *testing.T) {
	for _, finishBeforeReply := range []bool{false, true} {
		t.Run(map[bool]string{false: "during", true: "after"}[finishBeforeReply], func(t *testing.T) {
			a, posts, _ := refreshFixture(t)
			a.account.Profile = &netease.Profile{UserID: 1}
			a.library.Mine = []netease.Playlist{{ID: 7, Name: "Local"}}
			userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
				return []netease.Playlist{{ID: 99, Name: "Deleted"}}, nil, nil
			}
			finish := a.beginLibraryEdit()
			a.loadMine(1)
			if finishBeforeReply {
				finish()
			}
			refreshPost(t, posts)
			if !finishBeforeReply {
				finish()
			}
			if len(a.library.Mine) != 1 || a.library.Mine[0].ID != 7 {
				t.Fatal("response reintroduced an item edited on the server")
			}
			if a.library.Refresh.Edits != 0 {
				t.Fatal("edit gate never closed")
			}
		})
	}
}

// An unrelated edit used to discard a successful refresh and leave it marked
// fresh; the next view must retry once, after any pending edit has finished.
func TestLibraryRefreshDiscardedSuccessRetries(t *testing.T) {
	for _, resource := range []string{"likes", "mine", "playlist"} {
		for _, pending := range []bool{false, true} {
			t.Run(resource+map[bool]string{false: "/finished", true: "/pending"}[pending], func(t *testing.T) {
				a, posts, _ := refreshFixture(t)
				a.account.Profile = &netease.Profile{UserID: 1}
				var calls atomic.Int32
				likedIDs = func(int64) ([]int64, error) { calls.Add(1); return []int64{42}, nil }
				userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
					calls.Add(1)
					return []netease.Playlist{{ID: 42, Name: "Fresh"}}, nil, nil
				}
				playlistDetail = func(id int64) (*netease.Playlist, error) {
					calls.Add(1)
					return &netease.Playlist{ID: id, Name: "Fresh"}, nil
				}
				switch resource {
				case "likes":
					a.loadLiked(1)
				case "mine":
					a.loadMine(1)
				case "playlist":
					a.nav.Current = route{Page: pagePlaylist, ID: 7}
					a.library.Playlists[7] = &netease.Playlist{ID: 7, Name: "Local"}
					a.loadPlaylist(7, false)
				}
				finish := a.beginLibraryEdit()
				if !pending {
					finish()
				}
				refreshPost(t, posts)
				tt := ui.NewTester(a.view, 1000, 700)
				if pending {
					tt.Frame()
					if calls.Load() != 1 {
						t.Fatal("retried while an edit was pending")
					}
					finish()
				}
				tt.Frame()
				refreshPost(t, posts)
				switch resource {
				case "likes":
					if !a.library.Liked[42] {
						t.Fatal("initial likes never recovered")
					}
				case "mine":
					if len(a.library.Mine) != 1 || a.library.Mine[0].ID != 42 {
						t.Fatal("initial sidebar never recovered")
					}
				case "playlist":
					if a.library.Playlists[7].Name != "Fresh" {
						t.Fatal("discarded playlist never recovered")
					}
				}
				tt.Frame()
				tt.Frame()
				if calls.Load() != 2 {
					t.Fatalf("retry did not coalesce: %d calls", calls.Load())
				}
			})
		}
	}
}

// A failed replacement request must retain the retry interval, not turn a
// stale response's follow-up into an offline network loop.
func TestLibraryRefreshRetryFailureStops(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.account.Profile = &netease.Profile{UserID: 1}
	var calls atomic.Int32
	likedIDs = func(int64) ([]int64, error) {
		if calls.Add(1) == 1 {
			return []int64{42}, nil
		}
		return nil, errors.New("offline")
	}
	a.loadLiked(1)
	finish := a.beginLibraryEdit()
	finish()
	refreshPost(t, posts)
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	refreshPost(t, posts)
	tt.Frame()
	tt.Frame()
	a.freshenLikes()
	if calls.Load() != 2 || a.library.Refresh.Likes.Retry || viewmodel.ListDue(a.library.Refresh.Likes.At, a.listsNow()) {
		t.Fatal("failed follow-up kept retrying")
	}
}

// Reset only refresh bookkeeping to isolate the app account-generation guard.
func (a *app) resetLibraryFreshness() {
	a.library.Refresh = viewmodel.LibraryRefresh{Now: a.library.Refresh.Now}
	a.library.Loading = map[int64]bool{}
	a.library.PlayWhenLoaded = map[int64]bool{}
}

func (a *app) playlistRequest(id int64) *viewmodel.ListRequest {
	if a.library.Refresh.Playlists == nil {
		a.library.Refresh.Playlists = map[int64]*viewmodel.ListRequest{}
	}
	if a.library.Refresh.Playlists[id] == nil {
		a.library.Refresh.Playlists[id] = &viewmodel.ListRequest{}
	}
	return a.library.Refresh.Playlists[id]
}
func (a *app) usePlaylist(id int64) { a.library.UsePlaylist(id) }

const playlistCachePages = viewmodel.PlaylistCachePages

func (a *app) forgetPlaylist(id int64) {
	a.library.ForgetPlaylist(id)
	a.forgetPlaylistSnapshot(id)
}

func (a *app) touchPlaylist(id int64) { a.library.TouchPlaylist(id) }

// beginLibraryEdit makes refreshes respect edits that are already pending,
// not only edits made after their snapshot. The ending revision rejects a
// server reply fetched before a rename/delete/like had finished committing.
func (a *app) beginLibraryEdit() func() {
	generation := a.account.Generation
	finish := a.library.BeginEdit()
	return func() {
		if a.account.Generation == generation {
			finish()
		}
	}
}
