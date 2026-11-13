package netease

import (
	"net/http"
	"net/url"
	"testing"
)

// Following an artist asks with the session's csrf token, in the query and in
// the form: without it NetEase answered 200 and followed nobody.
func TestFollowArtistSendsTheCSRFToken(t *testing.T) {
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Value: "session-value", Path: "/"},
		{Name: "__csrf", Value: "csrf-value", Path: "/"},
	})
	var query, form url.Values
	answer(t, func(req *http.Request) string {
		query = req.URL.Query()
		if err := req.ParseForm(); err != nil {
			t.Error(err)
		}
		form = req.PostForm
		return `{"code":200}`
	})
	if err := FollowArtist(42, true); err != nil {
		t.Fatal(err)
	}
	if got := query.Get("csrf_token"); got != "csrf-value" {
		t.Errorf("csrf_token in the query = %q", got)
	}
	if got := form.Get("csrf_token"); got != "csrf-value" {
		t.Errorf("csrf_token in the form = %q", got)
	}
	if got := form.Get("artistId"); got != "42" {
		t.Errorf("artistId = %q", got)
	}
}
