package netease

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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
