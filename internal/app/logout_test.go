/*
验证退出登录时报告凭据删除失败，并隔离属于旧登录的完成回调。
*/

package app

import (
	"errors"
	"net/http"
	"testing"

	"netease-music-mygo/internal/features/netease"
)

// Failed credential deletion is reported immediately and survives a failed remote logout.
func TestLogoutReportsCredentialDeletionFailure(t *testing.T) {
	a, posts := offlineFixture(t)
	fakeOfflineHome(t)
	clear, logout := clearLoginSession, logoutSession
	t.Cleanup(func() { clearLoginSession, logoutSession = clear, logout })
	clearLoginSession = func() error { return errors.New("keychain unavailable") }
	started, release := make(chan struct{}), make(chan struct{})
	logoutSession = func([]*http.Cookie) error {
		close(started)
		<-release
		return errors.New("server unavailable")
	}
	a.signOut()
	if a.account.Profile != nil || a.toast == "" {
		t.Fatal("sign-out hid the credential deletion failure")
	}
	initial := a.toast
	wait(t, started, "remote logout")
	for a.home.Loading {
		refreshPost(t, posts)
	}
	close(release)
	refreshPost(t, posts)
	if a.account.Profile != nil || a.toast == "" || a.toast == initial {
		t.Fatal("remote logout failure did not preserve and update the local failure report")
	}
}

// An old logout's completion cannot change feedback after another login starts.
func TestLogoutCompletionBelongsToItsLogin(t *testing.T) {
	a, posts := offlineFixture(t)
	fakeOfflineHome(t)
	clear, logout := clearLoginSession, logoutSession
	t.Cleanup(func() { clearLoginSession, logoutSession = clear, logout })
	clearLoginSession = func() error { return nil }
	started, release := make(chan struct{}), make(chan struct{})
	logoutSession = func([]*http.Cookie) error {
		close(started)
		<-release
		return errors.New("server unavailable")
	}
	a.signOut()
	wait(t, started, "remote logout")
	for a.home.Loading {
		refreshPost(t, posts)
	}
	a.account.Login.Generation++
	a.account.Profile = &netease.Profile{UserID: 8}
	a.toast = "new login feedback"
	close(release)
	refreshPost(t, posts)
	if a.account.Profile.UserID != 8 || a.toast != "new login feedback" {
		t.Fatal("old logout completion affected the new login")
	}
}
