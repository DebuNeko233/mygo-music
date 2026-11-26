/*
验证已缓存歌曲的时间显示不误闪缓冲状态，并保留真实网络缓冲提示。
*/

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Switching to cached audio must keep the remaining time visible through its short asynchronous open.
func TestCachedTrackTimeDoesNotFlashBuffering(t *testing.T) {
	a := newTestApp()
	data, err := os.ReadFile("../player/testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	meta, err := json.Marshal(netease.Stream{Type: "mp3", Level: "exhigh", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2"} {
		for suffix, bytes := range map[string][]byte{".mp3": data, ".json": meta} {
			if err := os.WriteFile(filepath.Join(dir, id+"-exhigh"+suffix), bytes, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	posts := make(chan func(), 32)
	a.pl = player.New(func(fn func()) { posts <- fn })
	a.pl.Cache = player.NewCache(dir, 1<<20)
	a.pl.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	a.pl.SetVolume(0)
	t.Cleanup(func() { a.pl.SuspendCache() })
	player.Cue(a.pl, []netease.Track{{ID: 1, Duration: 20000}, {ID: 2, Duration: 10000}})
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			a.views.playback.NowPlayingTime(c)
			ui.Button(c, "下一首").OnClick(a.pl.Next)
		})
	}, 300, 70)
	if err := tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	if !a.pl.Loading() || a.pl.Current().ID != 2 {
		t.Fatal("the cached track did not enter its asynchronous open")
	}
	if a.pl.Buffering() {
		t.Fatal("the cached open was reported as remote buffering")
	}
	if texts := tt.Texts(); slices.Contains(texts, "正在缓冲…") || !slices.Contains(texts, "−0:10") {
		t.Fatalf("cached switch replaced the new remaining time: %q", texts)
	}
	tt.Advance(100 * time.Millisecond)
	for a.pl.Loading() || a.pl.MetadataLoading() {
		wait(t, posts, "cached audio and lyrics")()
	}
	if !a.pl.Ready() || a.pl.Err() != "" {
		t.Fatalf("cached audio did not become ready: %s", a.pl.Err())
	}
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if slices.Contains(tt.Texts(), "正在缓冲…") {
		t.Fatal("a completed cached open later displayed a buffering hint")
	}
}

// Waiting for unavailable audio still displays buffering; only complete cached opens skip the hint.
func TestNowPlayingTimeKeepsNonCachedBuffering(t *testing.T) {
	a := newTestApp()
	player.Cue(a.pl, []netease.Track{{ID: 1, Duration: 20000}, {ID: 2, Duration: 10000}})
	a.pl.SuspendCache() // Hold audio loading without a network request or wall-clock timer.
	tt := ui.NewTester(a.views.playback.NowPlayingTime, 300, 24)
	check := func() {
		t.Helper()
		if !slices.Contains(tt.Texts(), "正在缓冲…") {
			t.Fatal("unavailable audio lost its buffering hint")
		}
		if !a.pl.Loading() || !a.pl.Buffering() {
			t.Fatal("the buffering state was cleared while audio was unavailable")
		}
	}
	check()
	tt.Advance(2 * time.Second)
	tt.Frame()
	check()
	a.pl.Next()
	tt.Frame()
	check()
}
