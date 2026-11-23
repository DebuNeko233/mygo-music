package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

type pendingLibraryQR struct{}

func (*pendingLibraryQR) Check(string) (netease.QRStatus, error) {
	return netease.QRStatus{Code: netease.QRWaiting}, nil
}
func (*pendingLibraryQR) Commit() {}

// Escape retires the pending key, while reopening the same keyed dialog accepts only its new result.
func TestLoginDismissalRetiresQueuedKey(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	a.settings = defaultSettings()
	t.Cleanup(a.account.CloseLogin)
	s := a.accountServices()
	s.NewQR = func() (string, viewmodel.QRLoginAttempt, error) { return "test-key", &pendingLibraryQR{}, nil }
	a.account.OpenLogin(s)
	old := wait(t, posts, "queued key")
	tt := ui.NewTester(a.loginDialog, 800, 600)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.account.Login.Open {
		t.Fatal("Escape did not dismiss the dialog")
	}
	old()
	if a.account.Login.Code != nil {
		t.Fatal("closed dialog accepted a queued QR code")
	}
	a.account.OpenLogin(s)
	current := wait(t, posts, "reopened key")
	tt.Frame()
	current()
	tt.Frame()
	if !a.account.Login.Open || a.account.Login.Code == nil || a.account.Login.Status != netease.QRWaiting {
		t.Fatal("reopened dialog lost its current key")
	}
}

// Replies scoped to an old login cannot clear a new pending subscription for the same user and resource.
func TestLibraryRepliesRespectSameUserNewLogin(t *testing.T) {
	for _, album := range []bool{false, true} {
		t.Run(map[bool]string{false: "playlist", true: "album"}[album], func(t *testing.T) {
			a := newTestApp()
			posts := testPosts(a)
			a.account.Profile = &netease.Profile{UserID: 1}
			oldPlaylist, oldAlbum := savePlaylist, subscribeAlbum
			t.Cleanup(func() { savePlaylist, subscribeAlbum = oldPlaylist, oldAlbum })
			savePlaylist = func(int64, bool) error { return errors.New("old failure") }
			subscribeAlbum = savePlaylist
			if album {
				a.toggleAlbumSaved(netease.AlbumProfile{Album: netease.Album{ID: 7}})
			} else {
				a.toggleSaved(netease.Playlist{ID: 7})
			}
			apply := wait(t, posts, "old subscription")
			a.account.Accept(&netease.Profile{UserID: 1})
			a.library.SavingPlaylists = map[int64]bool{7: true}
			a.library.SavingAlbums = map[int64]bool{7: true}
			a.library.Refresh.Edits = 2
			a.library.Refresh.Revision = 20
			apply()
			if !a.library.SavingPlaylists[7] || !a.library.SavingAlbums[7] || a.library.Refresh.Edits != 2 || a.library.Refresh.Revision != 20 || a.toast != "" {
				t.Fatal("old login modified a new pending edit")
			}
		})
	}
}
