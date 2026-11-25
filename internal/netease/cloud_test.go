/*
验证网盘身份、分页结束、登录失效和请求取消，并提供可选真实服务检查。
*/

package netease

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"
)

// Cloud pagination sends the authenticated web request and retains upload IDs even when catalogue matches differ.
func TestCloudSongsUsesUploadIdentity(t *testing.T) {
	oldClient, oldRandom := httpClient, weapiRandom
	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{Jar: jar}
	weapiRandom = bytes.NewReader(make([]byte, 16))
	t.Cleanup(func() { httpClient, weapiRandom = oldClient, oldRandom })
	jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "cloud-session"}, {Name: "__csrf", Value: "cloud-csrf"}})
	answer(t, func(req *http.Request) string {
		if req.Method != "POST" || req.URL.Path != "/weapi/v1/cloud/get" {
			t.Errorf("wrong cloud request: %s %s", req.Method, req.URL.Path)
		}
		if !strings.Contains(req.Header.Get("Cookie"), "MUSIC_U=cloud-session") {
			t.Error("cloud request lost login")
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(req.Form.Get("params"))
		if err != nil {
			t.Fatal(err)
		}
		b, err = weapiDecrypt(t, b, []byte("aaaaaaaaaaaaaaaa"))
		if err != nil {
			t.Fatal(err)
		}
		b, err = base64.StdEncoding.DecodeString(string(b))
		if err != nil {
			t.Fatal(err)
		}
		b, err = weapiDecrypt(t, b, []byte(weapiKey))
		if err != nil {
			t.Fatal(err)
		}
		var args struct {
			Limit, Offset int
			CSRF          string `json:"csrf_token"`
		}
		if err = json.Unmarshal(b, &args); err != nil {
			t.Fatal(err)
		}
		if args.Limit != 100 || args.Offset != 200 || args.CSRF != "cloud-csrf" {
			t.Errorf("cloud args: %+v", args)
		}
		return `{"code":200,"count":203,"hasMore":true,"data":[
   {"songId":5001,"songName":"私藏现场","artist":"上传者歌手","album":"演唱会","simpleSong":{"id":9,"dt":123456,"fee":1,"privilege":{"st":-1,"pl":0},"ar":[{"id":-1,"name":""}],"al":{"id":-1}}},
   {"songId":5002,"fileName":"未匹配歌曲.flac"},
   {"songId":0,"simpleSong":{"id":99}}
  ]}`
	})
	page, err := CloudSongs(1000, 200)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextOffset != 203 || !page.HasMore || page.Count != 203 || len(page.Tracks) != 2 {
		t.Fatalf("page %+v", page)
	}
	tr := page.Tracks[0]
	if tr.ID != 5001 || !tr.Cloud || !tr.Playable() || tr.Name != "私藏现场" || tr.ArtistNames() != "上传者歌手" || tr.Album.Name != "演唱会" || tr.Album.ID != 0 || tr.Duration != 123456 {
		t.Fatalf("wrong upload mapping: %+v", tr)
	}
	if page.Tracks[1].Name != "未匹配歌曲" {
		t.Fatalf("missing file fallback: %+v", page.Tracks[1])
	}
	b, _ := json.Marshal(tr)
	var restored Track
	if err := json.Unmarshal(b, &restored); err != nil || !restored.Cloud || !restored.Playable() {
		t.Fatal("restored private queue entry lost playback permission")
	}
	if (&Track{ID: 9, Fee: 1, Privilege: &Privilege{St: -1, Pl: 0}}).Playable() {
		t.Fatal("ordinary catalogue restriction was bypassed")
	}
}

// Empty pages stop pagination, and expired sessions remain login failures rather than empty libraries.
func TestCloudSongsEmptyAndExpired(t *testing.T) {
	body := `{"code":200,"count":0,"hasMore":true,"data":[]}`
	answer(t, func(*http.Request) string { return body })
	page, err := CloudSongs(100, 0)
	if err != nil || page.HasMore || page.NextOffset != 0 {
		t.Fatalf("empty page: %+v, %v", page, err)
	}
	body = `{"code":301,"message":"need login"}`
	page, err = CloudSongs(100, 0)
	if page != nil || !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("expired login: %+v, %v", page, err)
	}
}

// With LIVE and NM_SESSION set, the signed-in account can list uploads and resolve their actual playback URLs.
func TestCloudSongsLive(t *testing.T) {
	path := os.Getenv("NM_SESSION")
	if os.Getenv("LIVE") == "" || path == "" {
		t.Skip("set LIVE and NM_SESSION for authenticated cloud verification")
	}
	useSession(t, path)
	page, err := CloudSongs(100, 0)
	if err != nil {
		t.Fatalf("cloud listing: %v", err)
	}
	t.Logf("cloud listing: total=%d, first-page=%d, more=%v", page.Count, len(page.Tracks), page.HasMore)
	if len(page.Tracks) == 0 {
		return
	}
	for _, tr := range page.Tracks[:min(3, len(page.Tracks))] {
		if !tr.Cloud || !tr.Playable() {
			t.Fatal("upload did not retain cloud playback eligibility")
		}
		stream, err := SongURL(tr.ID, "exhigh")
		if err != nil {
			t.Fatalf("cloud stream lookup: %v", err)
		}
		if stream == nil || stream.URL == "" {
			t.Fatal("cloud stream returned no URL")
		}
		t.Logf("cloud stream resolved: type=%s, level=%s", stream.Type, stream.Level)
	}
}

// Cancelling a cloud scan reaches the HTTP transport instead of leaving the request running in the background.
func TestCloudSongsCancelsHTTPRequest(t *testing.T) {
	old := httpClient
	jar, _ := cookiejar.New(nil)
	entered := make(chan struct{})
	httpClient = &http.Client{Jar: jar, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	t.Cleanup(func() { httpClient = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := CloudSongsContext(ctx, 100, 0); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cloud request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cloud request ignored cancellation")
	}
}

// Generic service art is absent artwork across old queues and new cloud responses, without discarding custom covers.
func TestCoverURLRecognizesServicePlaceholder(t *testing.T) {
	for _, raw := range []string{"http://p1.music.126.net/x/109951171180358170.jpg", "https://p2.music.126.net/x/109951171180358170.jpg?param=600y600", "  "} {
		if CoverURL(raw) != "" || Sized(raw, 120) != "" {
			t.Fatalf("placeholder retained: %q", raw)
		}
	}
	for _, raw := range []string{"https://p1.music.126.net/x/custom.jpg", "https://example.com/109951171180358170.jpg"} {
		if CoverURL(raw) != raw {
			t.Fatalf("custom cover removed: %q", raw)
		}
	}
	tr := cloudSong{SongID: 1, SimpleSong: Track{Album: Album{PicURL: "http://p1.music.126.net/x/109951171180358170.jpg"}}}.track()
	if tr.Album.PicURL != "" {
		t.Fatal("new cloud entry kept default service art")
	}
}
