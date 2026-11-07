package app

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"sync/atomic"
	"testing"
	"time"
)

func refreshFixture(t *testing.T) (*app, chan func(), *time.Time) {
	t.Helper()
	isolateUserData(t)
	posts := make(chan func(), 32)
	oldPost, oldDetail, oldMine, oldLikes := testPost, playlistDetail, userPlaylists, likedIDs
	testPost = func(f func()) { posts <- f }
	t.Cleanup(func() { testPost, playlistDetail, userPlaylists, likedIDs = oldPost, oldDetail, oldMine, oldLikes })
	a := newTestApp()
	a.settings = defaultSettings()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.listRefresh.now = func() time.Time { return now }
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

// Expiry is checked on navigation/focus, requests coalesce, and a failure
// retains old content and observes the same retry interval instead of looping.
func TestLibraryRefreshExpiryAndFailure(t *testing.T) {
	a, posts, now := refreshFixture(t)
	old := &netease.Playlist{ID: 7, Name: "Old"}
	a.playlists[7] = old
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
	if calls.Load() != 1 || a.playlists[7] != old || a.plErr[7] != "" {
		t.Fatal("failed refresh lost content or duplicated requests")
	}
	a.loadPlaylist(7, false)
	*now = now.Add(listsFresh - time.Second)
	a.loadPlaylist(7, false)
	if calls.Load() != 1 {
		t.Fatal("retried before the freshness interval")
	}
	*now = now.Add(time.Second)
	a.loadPlaylist(7, false)
	a.loadPlaylist(7, false)
	refreshPost(t, posts)
	if calls.Load() != 2 || a.playlists[7].Name != "New" {
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
			a.playlists[7] = original
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
				a.playlists[7] = &netease.Playlist{ID: 7, Name: "Local"}
			case "forget":
				a.forgetPlaylist(7)
			}
			refreshPost(t, posts)
			if got := a.playlists[7]; got != nil && got.Name == "Server" {
				t.Fatal("stale response overwrote a local edit")
			}
			if edit == "forget" && a.playlists[7] != nil {
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
	if !a.loadingPl[7] || !a.playlistRequest(7).busy || a.playlists[7] != nil {
		t.Fatal("old completion finished the new load")
	}
	close(newDone)
	refreshPost(t, posts)
	if a.loadingPl[7] || a.playlists[7].Name != "New" {
		t.Fatal("replacement did not finish")
	}
}

// Returning to the foreground refreshes the sidebar, liked IDs and visible
// list once, retaining all old values until their answers arrive.
func TestLibraryRefreshOnWindowActivation(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.profile = &netease.Profile{UserID: 1}
	a.mine = []netease.Playlist{{ID: 7, Name: "Old"}}
	a.liked = map[int64]bool{1: true}
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "Old"}
	a.route = route{page: pagePlaylist, id: 7}
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
	if a.mine[0].Name != "Old" || !a.liked[1] || a.playlists[7].Name != "Old" {
		t.Fatal("refresh blanked existing content")
	}
	for range 3 {
		refreshPost(t, posts)
	}
	if a.mine[0].Name != "New" || !a.liked[2] || a.liked[1] || a.playlists[7].Name != "New" {
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
	a.profile = &netease.Profile{UserID: 1}
	a.liked = map[int64]bool{1: true}
	a.playlists[7] = &netease.Playlist{ID: 7, SpecialType: 5, Tracks: []netease.Track{{ID: 1}}}
	playlistDetail = func(id int64) (*netease.Playlist, error) {
		return &netease.Playlist{ID: id, SpecialType: 5, Tracks: []netease.Track{{ID: 2}}}, nil
	}
	likedIDs = func(int64) ([]int64, error) { return []int64{2}, nil }
	a.open(route{page: pagePlaylist, id: 7})
	refreshPost(t, posts)
	refreshPost(t, posts)
	if !a.liked[2] || a.liked[1] || a.playlists[7].Tracks[0].ID != 2 {
		t.Fatal("liked page and hearts stayed stale")
	}
}

// Personal list refreshes must not overwrite optimistic edits made while
// the request is in flight, including changes to a playlist's name in place.
func TestLibraryRefreshPersonalEditsWin(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.profile = &netease.Profile{UserID: 1}
	a.mine = []netease.Playlist{{ID: 7, Name: "Before"}}
	a.liked = map[int64]bool{1: true}
	userPlaylists = func(int64) ([]netease.Playlist, []netease.Playlist, error) {
		return []netease.Playlist{{ID: 7, Name: "Server"}}, nil, nil
	}
	likedIDs = func(int64) ([]int64, error) { return []int64{3}, nil }
	a.loadMine(1)
	a.loadLiked(1)
	a.mine[0].Name = "Local"
	a.setLiked(2, true)
	refreshPost(t, posts)
	refreshPost(t, posts)
	if a.mine[0].Name != "Local" || !a.liked[2] || a.liked[3] {
		t.Fatal("refresh overwrote local personal edits")
	}
}

// A response from an earlier account epoch must not publish or clear the
// current request even after signing back in as the original UID (A-B-A).
func TestLibraryRefreshAccountEpoch(t *testing.T) {
	a, posts, _ := refreshFixture(t)
	a.profile = &netease.Profile{UserID: 1}
	a.mine = []netease.Playlist{{ID: 7, Name: "Current"}}
	a.liked = map[int64]bool{9: true}
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
	a.profile = &netease.Profile{UserID: 2}
	a.homeGen++
	a.resetLibraryFreshness()
	a.profile = &netease.Profile{UserID: 1}
	a.homeGen++
	a.resetLibraryFreshness()
	for range 3 {
		refreshPost(t, posts)
	}
	if a.mine[0].Name != "Current" || !a.liked[9] || a.liked[3] || a.playlists[7] != nil {
		t.Fatal("previous account's data returned")
	}
}

// Starting a refresh during an optimistic edit must not reintroduce an item
// being deleted, even if the edit finishes before the refresh's reply lands.
func TestLibraryRefreshPendingEditWins(t *testing.T) {
	for _, finishBeforeReply := range []bool{false, true} {
		t.Run(map[bool]string{false: "during", true: "after"}[finishBeforeReply], func(t *testing.T) {
			a, posts, _ := refreshFixture(t)
			a.profile = &netease.Profile{UserID: 1}
			a.mine = []netease.Playlist{{ID: 7, Name: "Local"}}
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
			if len(a.mine) != 1 || a.mine[0].ID != 7 {
				t.Fatal("response reintroduced an item edited on the server")
			}
			if a.listRefresh.edits != 0 {
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
				a.profile = &netease.Profile{UserID: 1}
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
					a.route = route{page: pagePlaylist, id: 7}
					a.playlists[7] = &netease.Playlist{ID: 7, Name: "Local"}
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
					if !a.liked[42] {
						t.Fatal("initial likes never recovered")
					}
				case "mine":
					if len(a.mine) != 1 || a.mine[0].ID != 42 {
						t.Fatal("initial sidebar never recovered")
					}
				case "playlist":
					if a.playlists[7].Name != "Fresh" {
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
	a.profile = &netease.Profile{UserID: 1}
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
	if calls.Load() != 2 || a.listRefresh.likes.retry || listDue(a.listRefresh.likes.at, a.listsNow()) {
		t.Fatal("failed follow-up kept retrying")
	}
}
