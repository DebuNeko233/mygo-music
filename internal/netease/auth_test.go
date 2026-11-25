package netease

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// A confirmed QR attempt can read its new profile while normal requests still use the old session.
func TestQRLoginStagesSessionUntilCommitted(t *testing.T) {
	oldJar, oldTransport := httpClient.Jar, httpClient.Transport
	t.Cleanup(func() { httpClient.Jar, httpClient.Transport = oldJar, oldTransport })
	httpClient.Jar, _ = cookiejar.New(nil)
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "old"}})

	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		var body string
		switch req.URL.Path {
		case "/eapi/login/qrcode/unikey":
			header.Add("Set-Cookie", "NMTID=login-device; Path=/")
			body = `{"code":200,"unikey":"test-key"}`
		case "/eapi/login/qrcode/client/login":
			if c, err := req.Cookie("NMTID"); err != nil || c.Value != "login-device" {
				t.Error("the poll did not carry the attempt's device cookie")
			}
			header.Add("Set-Cookie", "MUSIC_U=new; Path=/")
			header.Add("Set-Cookie", "__csrf=new-csrf; Path=/")
			body = `{"code":803}`
		case "/api/nuser/account/get":
			switch c, err := req.Cookie("MUSIC_U"); {
			case err == nil && c.Value == "new":
				body = `{"code":200,"profile":{"userId":2}}`
			case err == nil && c.Value == "old":
				body = `{"code":200,"profile":{"userId":1}}`
			default:
				t.Errorf("account request has an unexpected session cookie: %v", err)
			}
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	attempt := NewQRLogin()
	key, err := attempt.Key()
	if err != nil {
		t.Fatal(err)
	}
	status, err := attempt.Check(key)
	if err != nil || status.Code != QRConfirmed || status.Profile == nil || status.Profile.UserID != 2 {
		t.Fatalf("confirmed profile: %+v, %v", status, err)
	}
	checkAccount := func(want int64) {
		t.Helper()
		profile, err := Account()
		if err != nil || profile == nil || profile.UserID != want {
			t.Fatalf("shared account = %+v, %v, want %d", profile, err, want)
		}
	}
	checkAccount(1)
	attempt.Commit()
	checkAccount(2)
	if token := csrf(); token != "new-csrf" {
		t.Errorf("the confirmed session did not publish its CSRF token")
	}
}

// Unsupported secret storage is a missing keyring, while a missing item or
// ordinary access failure must not advise replacing the credential service.
func TestMissingKeyringErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false}, {ErrNoKeyring, true}, {errors.ErrUnsupported, true},
		{fmt.Errorf("wrapped: %w", errors.ErrUnsupported), true},
		{mygo.ErrSecretNotFound, false},
		{errors.Join(errors.ErrUnsupported, mygo.ErrSecretNotFound), false},
		{errors.New("access denied"), false},
	} {
		if got := IsNoKeyring(tc.err); got != tc.want {
			t.Errorf("IsNoKeyring(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}
