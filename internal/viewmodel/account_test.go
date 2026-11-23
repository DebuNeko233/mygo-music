package viewmodel

import (
	"errors"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

// A transport failure preserves the offline account; a later confirmation refreshes it without resetting collections.
func TestAccountConfirmationPreservesOfflineIdentity(t *testing.T) {
	profile := &netease.Profile{UserID: 1, Nickname: "cached"}
	m := Account{Profile: profile, Generation: 4}
	posts := make(chan func(), 4)
	changes, verified := 0, 0
	failure := netease.ErrOffline
	s := AccountServices{Post: func(fn func()) { posts <- fn }, Identity: func() string { return "session" }, Ask: func() (*netease.Profile, error) { return nil, failure }, Change: func(p *netease.Profile) { changes++; m.Accept(p) }, Save: func() error { return nil }, Verified: func(*netease.Profile, string) { verified++ }}
	m.Confirm(s)
	apply := wait(t, posts, "offline confirmation")
	if !m.Busy || m.Pending || m.Profile != profile {
		t.Fatal("worker changed account state before publication")
	}
	apply()
	if m.Busy || !m.Pending || m.Profile != profile || m.Generation != 4 || changes != 0 {
		t.Fatal("transport failure cleared the offline identity")
	}
	s.Ask = func() (*netease.Profile, error) { return &netease.Profile{UserID: 1, Nickname: "online"}, nil }
	m.Confirm(s)
	wait(t, posts, "recovered confirmation")()
	if m.Profile.Nickname != "online" || m.Pending || m.Failure != nil || changes != 0 || verified != 1 {
		t.Fatal("same-account verification reset collections or failed to recover")
	}
}

// An account response posted before logout/re-login cannot clear the new lifetime, even when the UID is unchanged.
func TestAccountRejectsRetiredConfirmation(t *testing.T) {
	m := Account{Profile: &netease.Profile{UserID: 1}, Generation: 1}
	posts := make(chan func(), 1)
	s := AccountServices{Post: func(fn func()) { posts <- fn }, Ask: func() (*netease.Profile, error) { return nil, netease.ErrNotLoggedIn }, Identity: func() string { return "" }}
	m.Confirm(s)
	apply := wait(t, posts, "retired confirmation")
	next := &netease.Profile{UserID: 1, Nickname: "new login"}
	m.Accept(next)
	m.Busy = true
	apply()
	if m.Profile != next || !m.Busy || m.Generation != 2 {
		t.Fatal("retired response changed the new account lifetime")
	}
}

// A key or save result from a dismissed login cannot change a later dialog or report another account's persistence failure.
func TestAccountRejectsRetiredLoginResults(t *testing.T) {
	var m Account
	posts := make(chan func(), 4)
	s := AccountServices{Post: func(fn func()) { posts <- fn }, NewQR: func() (string, QRLoginAttempt, error) { return "old-key", nil, errors.New("old failure") }}
	m.OpenLogin(s)
	old := wait(t, posts, "old QR key")
	m.CloseLogin()
	m.OpenLogin(s)
	current := wait(t, posts, "new QR key")
	old()
	if !m.Login.Open || m.Login.Err != "" {
		t.Fatal("old key failure overwrote the new dialog")
	}
	current()
	if m.Login.Err == "" {
		t.Fatal("current key error was not published")
	}
	m.CloseLogin()
	m.Accept(&netease.Profile{UserID: 1})
	s.Save = func() error { return errors.New("keychain unavailable") }
	s.Notify = func(string) { t.Fatal("retired save reported a failure for the next login") }
	m.SaveLogin(nil, s)
	oldSave := wait(t, posts, "old save")
	m.Accept(&netease.Profile{UserID: 1})
	oldSave()
}

type accountConfirmation struct {
	profile   *netease.Profile
	committed bool
}

func (q *accountConfirmation) Check(string) (netease.QRStatus, error) {
	return netease.QRStatus{Code: netease.QRConfirmed, Profile: q.profile}, nil
}
func (q *accountConfirmation) Commit() { q.committed = true }

// A confirmed QR session becomes live immediately but only a successful save enables the delayed dialog close.
func TestAccountQRConfirmationSavesBeforeClosing(t *testing.T) {
	m := Account{Profile: &netease.Profile{UserID: 1}, Login: LoginState{Open: true, Generation: 1}}
	posts := make(chan func(), 4)
	save := make(chan struct{})
	delayed := make(chan func(), 1)
	picked, reports := int64(0), 0
	attempt := &accountConfirmation{profile: &netease.Profile{UserID: 2}}
	s := AccountServices{Post: func(fn func()) { posts <- fn }, Save: func() error { <-save; return nil }, Change: m.Accept, Discard: func() {}, StopReports: func() { reports++ }, PickUp: func(uid int64) { picked = uid }, After: func(_ time.Duration, fn func()) { delayed <- fn }}
	done := make(chan bool, 1)
	go func() { done <- m.CheckLogin(1, "key", attempt, s) }()
	wait(t, posts, "QR confirmation")()
	if !wait(t, done, "poll to stop") || !attempt.committed || m.Profile != attempt.profile || m.Login.Finished || !m.Login.Open || picked != 2 || reports != 1 {
		t.Fatal("confirmation committed the wrong session or closed before saving")
	}
	close(save)
	wait(t, posts, "session persistence")()
	if !m.Login.Finished || !m.Login.Open {
		t.Fatal("saved login did not show its welcome before closing")
	}
	wait(t, delayed, "delayed close")()
	wait(t, posts, "dialog close")()
	if m.Login.Open || m.Profile != attempt.profile {
		t.Fatal("delayed close did not retain the active account")
	}
}

// Resolving a profile after QR confirmation must not retire the same dialog's pending credential save.
func TestAccountQRSaveSurvivesProfileResolution(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "saved"
		if failure {
			name = "save-failed"
		}
		t.Run(name, func(t *testing.T) {
			m := Account{Login: LoginState{Open: true, Generation: 1}}
			posts := make(chan func(), 4)
			saved := make(chan struct{})
			release := sync.OnceFunc(func() { close(saved) })
			t.Cleanup(release)
			delayed := make(chan func(), 1)
			profile := &netease.Profile{UserID: 7}
			s := AccountServices{
				Post: func(fn func()) { posts <- fn }, Change: m.Accept,
				Discard: func() {}, StopReports: func() {}, Notify: func(string) {},
				Ask: func() (*netease.Profile, error) { return profile, nil }, Identity: func() string { return "" },
				Verified: func(*netease.Profile, string) {},
				Save: func() error {
					<-saved
					if failure {
						return errors.New("keychain unavailable")
					}
					return nil
				},
				After: func(_ time.Duration, fn func()) { delayed <- fn },
			}
			done := make(chan bool, 1)
			go func() { done <- m.CheckLogin(1, "key", &accountConfirmation{}, s) }()
			wait(t, posts, "QR confirmation without profile")()
			if !wait(t, done, "poll to stop") {
				t.Fatal("confirmed QR kept polling")
			}
			wait(t, posts, "profile resolution")()
			if m.Profile != profile || m.Generation != 2 || !m.Login.Open {
				t.Fatal("profile resolution lost the active login")
			}
			release()
			wait(t, posts, "QR credential save")()
			if failure {
				// Account verification also persists the resolved session; consume its result as well.
				wait(t, posts, "verification credential save")()
				if m.Login.Err == "" || m.Login.Finished || !m.Login.Open {
					t.Fatal("profile resolution hid the matching dialog's save failure")
				}
			} else {
				if !m.Login.Finished || !m.Login.Open {
					t.Fatal("profile resolution discarded the matching dialog's save completion")
				}
				wait(t, delayed, "delayed close")()
				wait(t, posts, "dialog close")()
				if m.Login.Open {
					t.Fatal("saved login did not close")
				}
			}
		})
	}
}
