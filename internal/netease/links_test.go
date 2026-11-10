package netease

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Share text, mobile/hash URLs and the website's Base64 JSON protocol
// must identify the same resource without mistaking ordinary queries for links.
func TestParseMusicLinks(t *testing.T) {
	encoded := func(s string) string { return "orpheus://" + base64.StdEncoding.EncodeToString([]byte(s)) }
	for _, tc := range []struct {
		raw, kind string
		id        int64
	}{
		{"https://music.163.com/song?id=1859245776&userid=1", "song", 1859245776},
		{"https://music.163.com/#/playlist?id=12", "playlist", 12},
		{"https://y.music.163.com/m/album?id=34&app_version=1", "album", 34},
		{"https://music.163.com/m/#/artist?id=56", "artist", 56},
		{"https://music.163.com/user/home?id=7", "user", 7},
		{"https://music.163.com/djradio?id=8", "djradio", 8},
		{"https://music.163.com/program?id=9", "program", 9},
		{"分享歌单：https://music.163.com/playlist?id=12（来自网易云音乐）", "playlist", 12},
		{"分享歌曲（https://music.163.com/song?id=12）", "song", 12},
		{"music.163.com/album?id=34", "album", 34},
		{"ORPHEUS://song/12/", "song", 12},
		{"orpheus://playlist?id=34", "playlist", 34},
		{"orpheus://artist/56/", "artist", 56},
		{encoded(`{"type":"song","id":1859245776,"cmd":"play","channel":"webset"}`), "song", 1859245776},
		{encoded(`{"type":"playlist","id":"34","cmd":"play"}`), "playlist", 34},
		{encoded(`{"type":"album","id":"56","cmd":"open"}`), "album", 56},
		{encoded(`{"type":"program","id":78,"cmd":"play"}`), "program", 78},
		{"https://163cn.tv/aBCd", "short", 0},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok, err := ParseMusicLink(tc.raw)
			if !ok || err != nil || got.Kind != tc.kind || got.ID != tc.id {
				t.Fatalf("got %+v, %v, %v", got, ok, err)
			}
		})
	}
	for _, raw := range []string{"春日影", "music theory", "https://example.com/song?id=1", "https://music.163.com.evil.test/song?id=1", "https://music.163.com@evil.test/song?id=1"} {
		if _, ok, _ := ParseMusicLink(raw); ok {
			t.Errorf("ordinary query recognized: %s", raw)
		}
	}
}

// Invalid IDs, unsupported commands and URLs with credentials or unusual
// ports cannot reach resource loading or be interpreted as download actions.
func TestMusicLinksRejectInvalidResources(t *testing.T) {
	for _, raw := range []string{
		"https://music.163.com/song?id=0", "https://music.163.com/song?id=-1",
		"https://music.163.com/song?id=1.5", "https://music.163.com/song?id=9223372036854775808",
		"https://music.163.com/song?id=1&id=2", "https://music.163.com/song?id=%GG",
		"https://user@music.163.com/song?id=1", "https://music.163.com:9999/song?id=1",
		"orpheus://", "orpheus://song/1?cmd=download", "orpheus://song/12?id=13", "orpheus://song/12/34", "orpheus://song/not-an-id",
		"orpheus://download/123", "orpheus://native", "https://music.163.com/mv?id=1",
		"orpheus://" + base64.StdEncoding.EncodeToString([]byte(`{"type":"song","id":1,"cmd":"download"}`)),
		"orpheus://" + base64.StdEncoding.EncodeToString([]byte(`{"type":"song","id":1e2,"cmd":"play"}`)),
		"https://music.163.com/song?id=" + strings.Repeat("1", 17000),
	} {
		if _, ok, err := ParseMusicLink(raw); !ok || err == nil {
			t.Errorf("accepted invalid resource %q", raw[:min(len(raw), 100)])
		}
	}
}

// Short links follow trusted redirects only, stop before fetching the
// final page, close response bodies, and never attach the account's cookies.
func TestResolveMusicShortLinks(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	for _, tc := range []struct {
		name, location string
		status         int
		wantErr        bool
	}{
		{"song", "https://music.163.com/#/song?id=12", 302, false},
		{"album", "https://y.music.163.com/m/album?id=34", 301, false},
		{"foreign", "https://example.com/song?id=12", 302, true},
		{"credentials", "https://name@music.163.com/song?id=12", 302, true},
		{"loop", "https://163cn.tv/loop", 302, true},
		{"no redirect", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			closed := 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "163cn.tv" || req.Header.Get("Cookie") != "" {
					t.Errorf("unexpected request: %s", req.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {tc.location}}, Body: &linkBody{Reader: strings.NewReader(""), closed: &closed}, Request: req}, nil
			})
			got, err := ResolveMusicLink(context.Background(), MusicLink{Kind: "short", URL: "https://163cn.tv/abc"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %+v, %v", got, err)
			}
			if calls != closed || calls > 5 {
				t.Fatalf("requests %d, closed %d", calls, closed)
			}
			if !tc.wantErr && got.ID <= 0 {
				t.Fatal("no resource after redirect")
			}
		})
	}
}

type linkBody struct {
	io.Reader
	closed *int
}

func (b *linkBody) Close() error { *b.closed++; return nil }

// A superseded short link must release its in-flight request promptly.
func TestMusicShortLinkCancellation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	ctx, cancel := context.WithCancel(context.Background())
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	_, err := ResolveMusicLink(ctx, MusicLink{Kind: "short", URL: "https://163cn.tv/abc"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

// A program link plays its audio ID with the program's title, cover and
// channel, rather than asking the song endpoint for the program ID.
func TestMusicLinkProgramMetadata(t *testing.T) {
	old := httpClient
	client := *old
	t.Cleanup(func() { httpClient = old })
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/dj/program/detail" || req.URL.Query().Get("id") != "78" {
			t.Errorf("wrong endpoint: %s", req.URL)
		}
		body := `{"code":200,"program":{"id":78,"name":"一期节目","duration":1000,"mainSong":{"id":123},"radio":{"id":9,"name":"播客","picUrl":"cover"}}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	httpClient = &client
	tr, err := MusicLinkTrack(context.Background(), MusicLink{Kind: "program", ID: 78})
	if err != nil || tr.ID != 123 || tr.ProgramID != 78 || tr.PodcastID != 9 || tr.Name != "一期节目" || tr.Album.PicURL != "cover" {
		t.Fatalf("wrong program metadata: %+v, %v", tr, err)
	}
}

// Cancelling a linked-song lookup must reach the eapi HTTP request too,
// instead of merely discarding its response after the timeout.
func TestMusicLinkSongMetadataCancellation(t *testing.T) {
	old := httpClient
	client := *old
	t.Cleanup(func() { httpClient = old })
	ctx, cancel := context.WithCancel(context.Background())
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/eapi/v3/song/detail" {
			t.Errorf("wrong endpoint: %s", req.URL)
		}
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	httpClient = &client
	_, err := MusicLinkTrack(ctx, MusicLink{Kind: "song", ID: 12})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost song cancellation: %v", err)
	}
}

// External URL events and pasted text must never panic on malformed input;
// successful parsing always yields a positive resource ID or a trusted short URL.
func FuzzParseMusicLink(f *testing.F) {
	for _, seed := range []string{"春日影", "orpheus://song/12", "https://music.163.com/#/album?id=34", "https://163cn.tv/abc", "orpheus://eyJ0eXBlIjoic29uZyIsImlkIjoxLCJjbWQiOiJwbGF5In0="} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		link, ok, err := ParseMusicLink(text)
		if !ok || err != nil {
			return
		}
		if link.Kind == "short" {
			if !strings.HasPrefix(link.URL, "http") {
				t.Fatal("short URL lost its scheme")
			}
		} else if link.ID <= 0 {
			t.Fatalf("nonpositive resource: %+v", link)
		}
	})
}
