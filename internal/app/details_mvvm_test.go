/*
验证详情页请求的账号代次隔离、独立加载和共享收藏状态。
*/

package app

import (
	"testing"

	"netease-music-mygo/internal/features/netease"
	albumvm "netease-music-mygo/internal/ui/viewmodel/album"
)

// Old owner-profile requests cannot publish subscriptions after leaving and re-entering the same account session.
func TestDetailUserRepliesRespectAccountGeneration(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 7}
	a.library.Refresh.Albums.At = a.library.Now()
	oldUser, oldLists, oldArtists, oldPodcasts := userDetail, userPlaylists, followedArtists, subscribedPodcasts
	t.Cleanup(func() {
		userDetail, userPlaylists, followedArtists, subscribedPodcasts = oldUser, oldLists, oldArtists, oldPodcasts
	})
	userDetail = func(id int64) (*netease.User, error) { return &netease.User{UserID: id}, nil }
	userPlaylists = func(id int64) ([]netease.Playlist, []netease.Playlist, error) {
		pl := netease.Playlist{ID: 8}
		pl.Creator.UserID = id
		return []netease.Playlist{pl}, nil, nil
	}
	followedArtists = func() ([]netease.ArtistProfile, error) {
		return []netease.ArtistProfile{{Artist: netease.Artist{ID: 9}}}, nil
	}
	subscribedPodcasts = func() ([]netease.Podcast, error) { return []netease.Podcast{{ID: 10}}, nil }
	a.loadUser(7)
	var replies []func()
	for range 4 {
		replies = append(replies, wait(t, posts, "old account result"))
	}
	a.account.Generation += 2
	for _, reply := range replies {
		reply()
	}
	d := a.users.Pages.Peek(7)
	if d.Loading != 2 || d.SubsLoading != 1 || d.User != nil || d.ArtistsShown || a.podcasts.Home.SubscriptionsShown || len(d.Playlists) != 0 {
		t.Fatal("previous login's private data reached the new session")
	}
}

// A follow completion from an old login must not roll back or show a toast in the current account session.
func TestDetailFollowReplyRespectsAccountGeneration(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 7}
	d := a.artists.Page(12)
	d.Profile = &netease.ArtistProfile{Artist: netease.Artist{ID: 12}}
	old := followArtist
	t.Cleanup(func() { followArtist = old })
	followArtist = func(int64, bool) error { return nil }
	a.toggleFollow(12)
	reply := wait(t, posts, "old follow result")
	a.account.Generation += 2
	a.toast = "current"
	reply()
	if !d.Following || !d.Profile.Followed || a.toast != "current" {
		t.Fatal("old follow completion changed the new account's presentation")
	}
}

// Starting nine detail requests must retain the ninth page until its response can publish beside the older pending pages.
func TestDetailRequestsKeepNewPageWhileOthersLoad(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	old := albumDetail
	t.Cleanup(func() { albumDetail = old })
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		return &netease.AlbumProfile{Album: netease.Album{ID: id}}, nil
	}
	for id := int64(1); id <= 9; id++ {
		a.loadAlbum(id)
	}
	for range 9 {
		refreshPost(t, posts)
	}
	if d := a.albums.Pages.Peek(9); d == nil || d.Album == nil || d.Album.ID != 9 || d.Loading {
		t.Fatal("newest detail evicted itself while older requests were pending")
	}
}

// Album collection actions from a detail still update the shared Library consumed by the user's own page.
func TestDetailAlbumSaveUsesSharedLibrary(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	a.account.Profile = &netease.Profile{UserID: 7}
	al := &netease.AlbumProfile{Album: netease.Album{ID: 8, Name: "album"}}
	a.albums.Pages.Set(8, &albumvm.AlbumPage{Album: al})
	old := subscribeAlbum
	t.Cleanup(func() { subscribeAlbum = old })
	subscribeAlbum = func(int64, bool) error { return nil }
	a.toggleAlbumSaved(*al)
	if !a.library.AlbumSaved(8) || len(a.library.SubAlbums) != 1 || a.library.SubAlbums[0].ID != 8 {
		t.Fatal("detail action forked album collection state")
	}
	refreshPost(t, posts)
	if !a.library.AlbumSaved(8) || a.library.SavingAlbums[8] {
		t.Fatal("save completion lost shared collection state")
	}
}
