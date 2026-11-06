package netease

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

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
