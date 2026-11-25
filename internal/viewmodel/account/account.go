/*
管理账号身份、扫码登录和凭据保存状态，隔离过期验证与登录结果。
*/

package account

import (
	"context"
	"errors"
	"net/http"
	"time"

	"rsc.io/qr"

	"netease-music-mygo/internal/netease"
)

// LoginState is the persistent QR form data; rendering and images belong to views.
type LoginState struct {
	Open                 bool
	Generation           int
	Code                 *qr.Code
	Status               int
	Scanner, Avatar, Err string
	Finished             bool
	Keyring              bool // the system has no credential store
}
type QRLoginAttempt interface {
	Check(string) (netease.QRStatus, error)
	Commit()
}

// Account owns identity and login/verification attempts on one thread.
// Generation identifies an account lifetime, including re-login as the same user.
type Account struct {
	Profile       *netease.Profile
	Generation    int
	Login         LoginState
	Busy, Pending bool
	Failure       error
	loginCancel   context.CancelFunc
}

func (m *Account) Accept(prof *netease.Profile) {
	m.Profile = prof
	m.Generation++
	m.Busy, m.Pending, m.Failure = false, false, nil
}

// AccountServices supplies the existing session functions and app-wide effects.
// Change accepts identity and clears dependent pages; it must call Accept once.
type AccountServices struct {
	Offline                             func() bool
	Stopped                             func() bool
	Post                                func(func())
	Restore                             func() (*netease.Profile, bool)
	Ask                                 func() (*netease.Profile, error)
	Identity                            func() string
	Save, Clear                         func() error
	Cookies                             func() []*http.Cookie
	Logout                              func([]*http.Cookie) error
	NewQR                               func() (string, QRLoginAttempt, error)
	Change                              func(*netease.Profile)
	Discard, ClearIdentity, StopReports func()
	Verified                            func(*netease.Profile, string)
	PickUp                              func(int64)
	Notify                              func(string)
	After                               func(time.Duration, func())
}

func (m *Account) Boot(s AccountServices) {
	generation := m.Generation
	go func() {
		prof, restored := s.Restore()
		s.Post(func() {
			if m.Generation != generation {
				return
			}
			s.Change(prof)
			if restored {
				m.Pending = true
				m.Confirm(s)
			}
		})
	}()
}

// Confirm retains an offline identity on transport errors; only a definite
// signed-out response clears its private collections and persisted identity.
func (m *Account) Confirm(s AccountServices) {
	if s.Offline != nil && s.Offline() {
		m.Pending = true
		return
	}
	if m.Busy {
		return
	}
	m.Busy = true
	generation := m.Generation
	go func() {
		prof, err := s.Ask()
		if prof != nil && prof.UserID <= 0 {
			prof = nil
		}
		identity := s.Identity()
		s.Post(func() {
			if m.Generation != generation {
				return
			}
			m.Busy, m.Failure = false, err
			if err != nil && !errors.Is(err, netease.ErrNotLoggedIn) {
				m.Pending = true
				return
			}
			m.Pending = false
			if prof == nil {
				err := s.Clear()
				s.ClearIdentity()
				s.Discard()
				s.Change(nil)
				if err != nil {
					s.Notify("登录凭据清除失败")
				}
				return
			}
			if m.Profile == nil || m.Profile.UserID != prof.UserID {
				s.Discard()
				s.Change(prof)
			} else {
				m.Profile = prof
			}
			m.SaveLogin(nil, s)
			s.Verified(prof, identity)
		})
	}()
}
func (m *Account) SaveLogin(onSaved func(), s AccountServices) {
	generation, dialogGeneration := m.Generation, m.Login.Generation
	go func() {
		err := s.Save()
		if err == nil && onSaved == nil {
			return
		}
		s.Post(func() {
			dialog := onSaved != nil && m.Login.Open && m.Login.Generation == dialogGeneration
			// Profile resolution advances the account generation within the same QR login.
			// The dialog owns its save result; ambient notifications belong to the account.
			if !dialog && m.Generation != generation {
				return
			}
			if err == nil {
				if dialog {
					onSaved()
				}
				return
			}
			message := "登录凭据保存失败，本次登录仅在应用运行期间有效"
			if dialog {
				m.Login.Finished = false
				m.Login.Err = message
				m.Login.Keyring = netease.IsNoKeyring(err)
			} else {
				if netease.IsNoKeyring(err) {
					message += "；系统未启用钥匙串，请查看登录窗口中的设置帮助"
				}
				s.Notify(message)
			}
		})
	}()
}
func (m *Account) OpenLogin(s AccountServices) {
	m.CloseLogin()
	m.Login = LoginState{Open: true, Generation: m.Login.Generation + 1}
	generation := m.Login.Generation
	ctx, cancel := context.WithCancel(context.Background())
	m.loginCancel = cancel
	go func() {
		key, attempt, err := s.NewQR()
		var code *qr.Code
		if err == nil {
			code, err = qr.Encode(netease.LoginURL(key), qr.M)
		}
		s.Post(func() {
			if generation != m.Login.Generation || !m.Login.Open || ctx.Err() != nil {
				return
			}
			if err != nil {
				m.Login.Err = "无法获取二维码，请检查网络"
				return
			}
			m.Login.Code, m.Login.Status = code, netease.QRWaiting
			go m.pollLogin(ctx, generation, key, attempt, s)
		})
	}()
}
func (m *Account) CloseLogin() {
	m.Login.Open = false
	if m.loginCancel != nil {
		m.loginCancel()
		m.loginCancel = nil
	}
}
func (m *Account) pollLogin(ctx context.Context, generation int, key string, attempt QRLoginAttempt, s AccountServices) {
	for {
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if m.checkLogin(ctx, generation, key, attempt, s) {
			return
		}
	}
}
func (m *Account) CheckLogin(generation int, key string, attempt QRLoginAttempt, s AccountServices) bool {
	return m.checkLogin(context.Background(), generation, key, attempt, s)
}
func (m *Account) checkLogin(ctx context.Context, generation int, key string, attempt QRLoginAttempt, s AccountServices) bool {
	status, err := attempt.Check(key)
	done := make(chan bool, 1)
	s.Post(func() {
		l := &m.Login
		if ctx.Err() != nil || generation != l.Generation || !l.Open || s.Stopped != nil && s.Stopped() {
			done <- true
			return
		}
		switch {
		case err != nil:
		case status.Code == netease.QRConfirmed:
			s.StopReports()
			attempt.Commit()
			l.Status = status.Code
			s.Discard()
			s.Change(status.Profile)
			m.SaveLogin(func() { m.Login.Finished = true; m.CloseLoginSoon(generation, s) }, s)
			if status.Profile == nil {
				m.Pending = true
				m.Confirm(s)
			} else {
				s.PickUp(status.Profile.UserID)
			}
		case status.Code == netease.QRExpired || status.Code == netease.QRWaiting || status.Code == netease.QRScanned:
			l.Status = status.Code
			if status.Nickname != "" {
				l.Scanner, l.Avatar = status.Nickname, status.AvatarURL
			}
		default:
			l.Err = status.Message
			if l.Err == "" {
				l.Err = "登录失败，请稍后再试"
			}
		}
		done <- l.Status == netease.QRExpired || l.Status == netease.QRConfirmed || l.Err != ""
	})
	select {
	case stop := <-done:
		return stop
	case <-ctx.Done():
		return true
	}
}
func (m *Account) CloseLoginSoon(generation int, s AccountServices) {
	close := func() {
		s.Post(func() {
			if generation == m.Login.Generation {
				m.CloseLogin()
			}
		})
	}
	if s.After != nil {
		s.After(1200*time.Millisecond, close)
	} else {
		time.AfterFunc(1200*time.Millisecond, close)
	}
}
func (m *Account) SignOut(s AccountServices) {
	m.CloseLogin()
	m.Login.Generation++
	m.Pending = false
	s.ClearIdentity()
	s.Discard()
	cookies := s.Cookies()
	clearErr := s.Clear()
	s.Change(nil)
	if clearErr != nil {
		s.Notify("退出凭据清除失败，重启后可能恢复登录")
	}
	generation := m.Login.Generation
	go func() {
		err := s.Logout(cookies)
		s.Post(func() {
			if generation != m.Login.Generation || m.Profile != nil {
				return
			}
			switch {
			case clearErr != nil && err != nil:
				s.Notify("本地凭据清除失败且服务器未响应，重启后可能恢复登录")
			case clearErr != nil:
				s.Notify("已退出登录，本地凭据清除失败")
			case err != nil:
				s.Notify("已退出登录，服务器未响应")
			}
		})
	}()
}
