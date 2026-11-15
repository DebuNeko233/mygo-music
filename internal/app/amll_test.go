package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/player"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

type amllRoundTrip func(*http.Request) (*http.Response, error)

func (fn amllRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

// A mirror's invalid response is skipped and a valid fallback is parsed as TTML.
func TestAMLLMirrorFallback(t *testing.T) {
	old := amllClient
	defer func() { amllClient = old }()
	var hosts []string
	amllClient = &http.Client{Transport: amllRoundTrip(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		body := "<html>bad gateway</html>"
		if req.URL.Host == "amlldb.bikonoo.com" {
			body = `<tt><body><p begin="1" end="2"><span begin="1" end="2">Singing</span></p></body></tt>`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	_, d, err := fetchAMLL(context.Background(), "", 42)
	if err != nil || len(hosts) != 2 || hosts[0] != "amll-ttml-db.stevexmh.net" || !d.Lines[0].Timed {
		t.Fatalf("did not fail over to the backup mirror: %v, %+v, %v", hosts, d, err)
	}
}

// Automatic selection uses NetEase on an AMLL miss; a pinned source never switches silently.
func TestAMLLSourceSelection(t *testing.T) {
	originalAMLL, originalNetEase := amllRequest, lyricsRequest
	defer func() { amllRequest, lyricsRequest = originalAMLL, originalNetEase }()
	amllCalls, neteaseCalls := 0, 0
	amllRequest = func(context.Context, string, int64) ([]byte, lyrics.Display, error) {
		amllCalls++
		return nil, lyrics.Display{}, errAMLLMissing
	}
	lyricsRequest = func(int64) (string, string, string, error) {
		neteaseCalls++
		return "[00:01]Fallback", "", "", nil
	}
	a := newTestApp()
	track := netease.Track{ID: 42}
	a.bindOfflineLyrics()
	d, err := a.pl.FetchLyricDisplay(context.Background(), track, nil)
	if err != nil || d.Source != lyrics.NetEase || amllCalls != 1 || neteaseCalls != 1 {
		t.Fatalf("automatic mode did not use the NetEase fallback: %+v, %v", d, err)
	}
	a.settings.SongLyricSources = map[int64]string{42: "amll"}
	a.bindOfflineLyrics()
	_, err = a.pl.FetchLyricDisplay(context.Background(), track, nil)
	if err == nil || amllCalls != 2 || neteaseCalls != 1 {
		t.Fatal("a pinned AMLL source switched to NetEase")
	}
	a.settings.SongLyricSources[42] = "netease"
	a.bindOfflineLyrics()
	_, err = a.pl.FetchLyricDisplay(context.Background(), track, nil)
	if err != nil || amllCalls != 2 || neteaseCalls != 2 {
		t.Fatal("a pinned NetEase source contacted an AMLL mirror")
	}
}

// The shared offline gate stops mirror requests before they leave the app.
func TestAMLLOfflineGate(t *testing.T) {
	netease.SetOffline(true)
	defer netease.SetOffline(false)
	if _, _, err := fetchAMLL(context.Background(), "", 42); !errors.Is(err, netease.ErrOffline) {
		t.Fatalf("offline fetch returned %v", err)
	}
}

// Choosing a source in Now Playing replaces the lyrics and keeps playback at its position.
func TestNowPlayingSwitchesLyricSource(t *testing.T) {
	amll, neteaseLyrics := amllRequest, lyricsRequest
	defer func() { amllRequest, lyricsRequest = amll, neteaseLyrics }()
	const native = `<tt><body><p begin="1" end="3">Mirror line</p></body></tt>`
	amllRequest = func(_ context.Context, _ string, id int64) ([]byte, lyrics.Display, error) {
		d, err := lyrics.ParseTTML([]byte(native), id)
		return []byte(native), d, err
	}
	lyricsRequest = func(int64) (string, string, string, error) {
		return "[00:01]NetEase line", "", "", nil
	}
	a, posts := offlineFixture(t)
	a.pl = player.New(a.post)
	a.pl.Cue([]netease.Track{{ID: 42, Name: "Sample", Duration: 180000}})
	a.pl.Hold(2 * time.Second)
	a.bindOfflineLyrics()
	a.pl.ReloadLyrics()
	refreshPost(t, posts)
	a.nowPlaying, a.npPane = true, paneLyrics
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	for _, choice := range []struct {
		menu   string
		source lyrics.Source
		text   string
	}{
		{"网易云", lyrics.NetEase, "NetEase line"},
		{"自动", lyrics.AMLL, "Mirror line"},
	} {
		if err := tt.Click("歌词来源"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(choice.menu); err != nil {
			t.Fatalf("%v; menu=%v", err, tt.Menu())
		}
		for a.pl.MetadataLoading() {
			refreshPost(t, posts)
		}
		tt.Frame()
		display, loaded := a.pl.LyricsDisplay()
		if !loaded || display.Source != choice.source || display.Lines[0].Text != choice.text ||
			a.pl.Current().ID != 42 || a.pl.Position() != 2*time.Second || !a.pl.Playing() {
			t.Fatalf("source change affected playback or kept stale lyrics: %+v", display)
		}
	}
}
