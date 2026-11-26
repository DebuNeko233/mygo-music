/*
验证公开与个人数据分离、并行结果生命周期隔离及刷新期间保留旧内容。
*/

package user

import (
	"errors"
	"sync/atomic"
	"testing"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/viewmodel/internal/testutil"
)

func userFixture() (*Users, UserServices, chan func()) {
	m := new(Users)
	m.Reset()
	posts := make(chan func(), 8)
	s := UserServices{
		Post: func(update func()) { posts <- update }, Detail: func(id int64) (*netease.User, error) { return &netease.User{UserID: id}, nil },
		Playlists: func(id int64) ([]netease.Playlist, []netease.Playlist, error) {
			lists := []netease.Playlist{{ID: 1}, {ID: 2, Privacy: 10}, {ID: 3}}
			lists[0].Creator.UserID = id
			lists[1].Creator.UserID = id
			lists[2].Creator.UserID = id + 1
			return lists, nil, nil
		},
		Artists: func() ([]netease.ArtistProfile, error) {
			return []netease.ArtistProfile{{Artist: netease.Artist{ID: 10}}}, nil
		},
	}
	return m, s, posts
}

// Public playlists are filtered once on publication, and private subscriptions are fetched only for the listener's own page.
func TestUserDetailsSeparatePublicAndOwnerData(t *testing.T) {
	for _, own := range []bool{false, true} {
		t.Run(map[bool]string{false: "public", true: "owner"}[own], func(t *testing.T) {
			m, s, posts := userFixture()
			var calls atomic.Int32
			artists := s.Artists
			s.Artists = func() ([]netease.ArtistProfile, error) { calls.Add(1); return artists() }
			listener := int64(8)
			count := 2
			if own {
				listener = 7
				count = 3
			}
			m.Load(7, listener, s)
			m.Load(7, listener, s)
			completions := make([]func(), count)
			for i := range completions {
				completions[i] = testutil.Wait(t, posts, "user result")
			}
			d := m.Pages.Peek(7)
			if d.User != nil || d.Loading != 2 || d.ArtistsShown {
				t.Fatal("worker modified user state before dispatch")
			}
			for _, done := range completions {
				done()
			}
			if d.User == nil || d.Loading != 0 || len(d.Playlists) != 1 || d.Playlists[0].ID != 1 {
				t.Fatal("public page lost its profile or exposed another/private playlist")
			}
			if own && (calls.Load() != 1 || !d.ArtistsShown || d.SubsLoading != 0) || !own && calls.Load() != 0 {
				t.Fatal("subscriptions leaked into another user's page or failed to coalesce")
			}
		})
	}
}

// Reset rejects all three old requests even when a new page has reused the same user ID and pending counters.
func TestUserDetailsResetRejectsParallelCompletions(t *testing.T) {
	m, s, posts := userFixture()
	m.Load(7, 7, s)
	var completions []func()
	for range 3 {
		completions = append(completions, testutil.Wait(t, posts, "old user result"))
	}
	m.Reset()
	fresh := &UserPage{Loading: 2, SubsLoading: 2}
	m.Pages.Set(7, fresh)
	for _, done := range completions {
		done()
	}
	if fresh.Loading != 2 || fresh.SubsLoading != 2 || fresh.User != nil || fresh.ArtistsShown || len(fresh.Playlists) != 0 {
		t.Fatal("old user result changed the new lifetime")
	}
}

// Refreshing followed artists retains the previously published collection when its request fails.
func TestUserDetailsArtistRefreshKeepsOldData(t *testing.T) {
	m, s, posts := userFixture()
	m.Load(7, 7, s)
	for range 3 {
		testutil.Wait(t, posts, "initial user result")()
	}
	d := m.Pages.Peek(7)
	s.Artists = func() ([]netease.ArtistProfile, error) { return nil, errors.New("offline") }
	m.Load(7, 7, s)
	if len(d.Artists) != 1 {
		t.Fatal("refresh blanked displayed subscriptions")
	}
	testutil.Wait(t, posts, "subscription refresh")()
	if d.SubsLoading != 0 || len(d.Artists) != 1 || d.Artists[0].ID != 10 || !d.ArtistsShown {
		t.Fatal("partial failure erased successful or previously displayed data")
	}
}
