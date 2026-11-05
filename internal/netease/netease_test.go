package netease

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// An HTTP error is not a successful API response even if its body says 200.
func TestHTTPErrorIsNotAnAPIResult(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader(`{"code":200}`)), Header: make(http.Header)}, nil
	})}
	var out any
	if err := do(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/api/test"}, Header: make(http.Header)}, &out); err == nil {
		t.Fatal("accepted an HTTP failure as a result")
	}
}

func TestPlayableFollowsPrivilege(t *testing.T) {
	tracks := []Track{{ID: 1, Fee: 1}, {ID: 2, Fee: 8}, {ID: 3, Fee: 8}, {ID: 4, Fee: 1}}
	if tracks[0].Playable() || !tracks[1].Playable() {
		t.Fatal("without privileges, VIP songs do not play and free ones do")
	}
	withPrivileges(tracks, []Privilege{
		{ID: 1, Pl: 320000},           // VIP song, the listener subscribes
		{ID: 2, Pl: 0},                // nothing to play
		{ID: 3, St: -200, Pl: 128000}, // taken down
	})
	want := []bool{true, false, false, false}
	for i, tr := range tracks {
		if tr.Playable() != want[i] {
			t.Errorf("track %d: playable %v", tr.ID, tr.Playable())
		}
	}
}
