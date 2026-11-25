/*
验证退出请求携带原会话凭据，且不会清除之后建立的新会话。
*/

package netease

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// Signing out ends the session with the cookies it was kept by, token and all,
// for an app that has already cleared them here.
func TestLogoutSendsTheSessionCookies(t *testing.T) {
	// Nothing left in the jar: only what Logout is handed may travel.
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Path: "/", MaxAge: -1},
		{Name: "__csrf", Path: "/", MaxAge: -1},
	})
	cookies := []*http.Cookie{
		{Name: "MUSIC_U", Value: "ended-session"},
		{Name: "__csrf", Value: "ended-csrf"},
	}
	var header http.Header
	var query url.Values
	answer(t, func(req *http.Request) string {
		header, query = req.Header, req.URL.Query()
		return `{"code":200}`
	})
	if err := Logout(cookies); err != nil {
		t.Fatal(err)
	}
	if c := header.Get("Cookie"); !strings.Contains(c, "MUSIC_U=ended-session") {
		t.Errorf("logout request cookie = %q", c)
	}
	if got := query.Get("csrf_token"); got != "ended-csrf" {
		t.Errorf("csrf_token in the query = %q", got)
	}
}

// A remote logout carries only its old cookies and cannot expire or delete a new login.
func TestLogoutPreservesNewSession(t *testing.T) {
	t.Setenv("MYGO_ENV", "development")
	dir := t.TempDir()
	oldDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatal(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, oldDir) })
	path := filepath.Join(dir, "session")
	if err := os.WriteFile(path, []byte("MUSIC_U=new-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldJar, oldTransport := httpClient.Jar, httpClient.Transport
	httpClient.Jar, _ = cookiejar.New(nil)
	t.Cleanup(func() { httpClient.Jar, httpClient.Transport = oldJar, oldTransport })
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "new-session"}})
	identity := SessionIdentity()
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Cookie"); got != "MUSIC_U=old-session" {
			t.Errorf("logout used cookies %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": {"MUSIC_U=; Max-Age=0; Path=/"}},
			Body:   io.NopCloser(strings.NewReader(`{"code":200}`))}, nil
	})
	if err := Logout([]*http.Cookie{{Name: "MUSIC_U", Value: "old-session"}}); err != nil {
		t.Fatal(err)
	}
	if SessionIdentity() != identity {
		t.Fatal("the old logout expired the new login's cookies")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "MUSIC_U=new-session" {
		t.Fatalf("the old logout changed the new saved session: %q, %v", b, err)
	}
}
