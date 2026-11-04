package netease

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// answer lets a test answer the requests NetEase would, whatever the
// client asks.
func answer(t *testing.T, replier func(*http.Request) string) {
	t.Helper()
	old := httpClient.Transport
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(replier(req))),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { httpClient.Transport = old })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestLyricsAskForAnyVersion checks what the lyric request asks for: asked
// for lyric version 1 alone, NetEase served nothing for 25 of the 300 songs
// in two charts, which the player took for an instrumental.
func TestLyricsAskForAnyVersion(t *testing.T) {
	var asked url.Values
	answer(t, func(req *http.Request) string {
		if req.Method == http.MethodPost {
			return `{"code":200,"yrc":{"lyric":""}}`
		}
		asked = req.URL.Query()
		return `{"code":200,"lrc":{"lyric":"[00:01.50]one\n[00:03.00]two"},"tlyric":{"lyric":"[00:01.50]一"}}`
	})

	lrc, translation, _, err := Lyrics(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"lv", "kv", "tv"} {
		if got := asked.Get(p); got != "-1" {
			t.Errorf("%s %q, and songs kept under another version come back without lyrics", p, got)
		}
	}
	if got := asked.Get("id"); got != "1" {
		t.Errorf("id %q, for the song asked for", got)
	}
	if want := "[00:01.50]one\n[00:03.00]two"; lrc != want {
		t.Errorf("lyrics %q, not %q", lrc, want)
	}
	if want := "[00:01.50]一"; translation != want {
		t.Errorf("translation %q, not %q", translation, want)
	}
}
