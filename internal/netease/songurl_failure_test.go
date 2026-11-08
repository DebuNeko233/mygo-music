package netease

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// NetEase's risk check is a retryable refusal, not a song restriction.
func TestSongURLMarksRiskControl(t *testing.T) {
	var requests int
	answer(t, func(*http.Request) string {
		requests++
		return `{"code":-460,"message":"risk"}`
	})
	_, err := SongURL(1, "exhigh")
	if !errors.Is(err, ErrRiskControl) {
		t.Fatalf("error %v does not identify risk control", err)
	}
	if requests != 2 {
		t.Fatalf("both gateways should be asked once, got %d requests", requests)
	}
}

// A desktop gateway under risk control falls back to the web gateway once.
func TestSongURLFallsBackToWeb(t *testing.T) {
	var requests int
	answer(t, func(req *http.Request) string {
		requests++
		if req.URL.Host != "music.163.com" {
			return `{"code":-460,"message":"risk"}`
		}
		return `{"code":200,"data":[{"url":"http://fixture.invalid/song","type":"mp3","level":"exhigh","freeTrialInfo":null}]}`
	})
	st, err := SongURL(1, "exhigh")
	if err != nil || st == nil || st.URL != "https://fixture.invalid/song" {
		t.Fatalf("stream=%+v error=%v", st, err)
	}
	if requests != 2 {
		t.Fatalf("one extra request is the fallback, got %d", requests)
	}
}

// A preview from the fallback does not turn a risk refusal into a full song.
func TestSongURLDoesNotAcceptFallbackPreview(t *testing.T) {
	answer(t, func(req *http.Request) string {
		if req.URL.Host == "music.163.com" {
			return `{"code":200,"data":[{"url":"https://fixture.invalid/song","freeTrialInfo":{"end":30000}}]}`
		}
		return `{"code":-460,"message":"risk"}`
	})
	_, err := SongURL(1, "exhigh")
	if !errors.Is(err, ErrRiskControl) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v should preserve the risk refusal", err)
	}
}

// A missing playback URL is not proof of a song restriction, even in a successful API response.
func TestSongURLDistinguishesMissingURLAndPreview(t *testing.T) {
	deviceOnce.Do(func() { deviceID = strings.Repeat("0", 52) })
	for _, tc := range []struct {
		name, body          string
		restricted, success bool
	}{
		{"empty", `{"code":200,"data":[]}`, false, false},
		{"missing URL", `{"code":200,"data":[{"url":null}]}`, false, false},
		{"API failure", `{"code":500}`, false, false},
		{"account refused", `{"code":-460}`, false, false},
		{"expired session", `{"code":301}`, false, false},
		{"preview", `{"code":200,"data":[{"url":"https://fixture.invalid/song","freeTrialInfo":{"start":0,"end":30000}}]}`, true, false},
		{"full song", `{"code":200,"data":[{"url":"https://fixture.invalid/song","type":"mp3","freeTrialInfo":null}]}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer(t, func(*http.Request) string { return tc.body })
			st, err := SongURL(1, "exhigh")
			if errors.Is(err, ErrUnavailable) != tc.restricted || (err == nil) != tc.success || (st != nil) != tc.success {
				t.Fatalf("stream=%+v error=%v", st, err)
			}
		})
	}
}
