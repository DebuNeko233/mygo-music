/*
验证下载权限、实际音质、网盘地址和请求取消的处理。
*/

package netease

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// Download rights use the download endpoint and object-shaped response, not
// the player's array response or playback permission, even for cached songs.
func TestDownloadURLRightsAndQuality(t *testing.T) {
	for _, test := range []struct {
		name, response string
		wantErr        bool
	}{
		{"allowed", `{"code":200,"data":{"code":200,"url":"https://audio.example/song","type":"FLAC","level":"lossless","size":125}}`, false},
		{"no URL", `{"code":200,"data":{"code":200,"url":null}}`, true},
		{"forbidden", `{"code":200,"data":{"code":403,"url":"https://audio.example/preview"}}`, true},
		{"trial", `{"code":200,"data":{"code":200,"url":"https://audio.example/preview","freeTrialInfo":{"start":0,"end":30}}}`, true},
		{"expired", `{"code":301}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			answer(t, func(r *http.Request) string {
				calls++
				if r.URL.Path != "/eapi/song/enhance/download/url/v1" {
					t.Fatalf("used playback rights: %s", r.URL.Path)
				}
				r.ParseForm()
				var data struct {
					ID                 int64
					Level, ImmerseType string
				}
				if err := json.Unmarshal([]byte(unseal(t, r.Form.Get("params"))), &data); err != nil {
					t.Fatal(err)
				}
				if data.ID != 123 || data.Level != "hires" || data.ImmerseType != "c51" {
					t.Fatalf("wrong download arguments: %+v", data)
				}
				return test.response
			})
			st, err := DownloadURL(context.Background(), Track{ID: 123}, "hires")
			if (err != nil) != test.wantErr || calls != 1 {
				t.Fatalf("stream=%+v err=%v calls=%d", st, err, calls)
			}
			if !test.wantErr && (st.Level != "lossless" || st.Type != "flac" || st.Size != 125) {
				t.Fatalf("lost actual quality: %+v", st)
			}
		})
	}
}

// Private uploads retain their existing stream route; cancellation prevents
// either route from issuing a request after an account/window has retired.
func TestDownloadURLCloudAndCancellation(t *testing.T) {
	calls := 0
	answer(t, func(r *http.Request) string {
		calls++
		if r.URL.Path != "/eapi/song/enhance/player/url/v1" {
			t.Fatal(r.URL.Path)
		}
		return `{"code":200,"data":[{"code":200,"url":"https://audio.example/upload","type":"wav","level":"exhigh"}]}`
	})
	if st, err := DownloadURL(context.Background(), Track{ID: 5001, Cloud: true}, "exhigh"); err != nil || st.Type != "wav" {
		t.Fatal(st, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DownloadURL(ctx, Track{ID: 123}, "exhigh")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled request ran: calls=%d err=%v", calls, err)
	}
}

// The shared response parser normalizes formats and URLs consistently while
// download quality stays unknown when the server only reports the bitrate.
func TestDownloadURLDoesNotBorrowRequestedQuality(t *testing.T) {
	answer(t, func(r *http.Request) string {
		return `{"code":200,"data":{"code":200,"url":"http://audio.example/upload","type":"3gp","br":192000}}`
	})
	st, err := DownloadURL(context.Background(), Track{ID: 71}, "hires")
	if err != nil {
		t.Fatal(err)
	}
	if st.Level != "" || st.Type != "m4a" || st.URL != "https://audio.example/upload" || st.Bitrate != 192000 {
		t.Fatal("lost actual response metadata", st)
	}
}
