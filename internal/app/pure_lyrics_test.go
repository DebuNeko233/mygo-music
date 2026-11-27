/*
验证纯净歌词设置接入真实设置页并即时更新播放视图，截图检查明暗与最小窗口布局。
*/

package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	playbackview "netease-music-mygo/internal/ui/view/playback"
)

// Clicking the real settings switch updates lyrics immediately and persists both
// choices while preserving the original source, queue, playhead and seek revision.
func TestPureLyricsSettingsApplyAndReload(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	t.Cleanup(a.pl.Close)
	a.settings = defaultSettings()
	a.settings.HotComments = false
	a.settings.Path = filepath.Join(t.TempDir(), "settings.json")
	player.Cue(a.pl, []netease.Track{{ID: 7, Name: "夜航", Duration: 120000, Artists: []netease.Artist{{Name: "Alice"}}}})
	player.RefreshLyrics(a.pl, 7, "[00:00]夜航 - Alice\n[00:01]作词：甲\n[00:20]男：唱出明天", "[00:20]A new tomorrow", "")
	player.Hold(a.pl, 21*time.Second)
	settingsUI := ui.NewTester(a.views.settings.Build, 780, 640)
	showSettingsPage(t, settingsUI, "播放")
	lyricsUI := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.views.playback.LyricsPane(c, a.pl.Current(), 640, ui.Rect{}, 0) })
	}, 780, 640)
	desktop := &playbackview.DeskLyrics{A: &a.views.playback}
	desktopUI := ui.NewTester(desktop.View, 780, 240)
	seek, position := a.pl.SeekRevision(), a.pl.Position()
	for _, pure := range []bool{true, false} {
		clickSettingsSwitch(t, settingsUI, "纯净歌词")
		lyricsUI.Frame()
		desktopUI.Frame()
		if a.settings.PureLyrics != pure || readCacheSettings(t, a.settings.Path).PureLyrics != pure {
			t.Fatal("the pure-lyrics switch did not apply and save")
		}
		want := "男：唱出明天"
		if pure {
			want = "唱出明天"
		}
		if !lyricsUI.HasText(want) || lyricsUI.HasText("作词：甲") == pure || !lyricsUI.HasText("A new tomorrow") {
			t.Fatalf("switch did not immediately change the lyric view: %v", lyricsUI.Texts())
		}
		if !desktopUI.HasText(want) {
			t.Fatalf("desktop did not follow the shared preference: %v", desktopUI.Texts())
		}
		if a.pl.SeekRevision() != seek || a.pl.Position() != position || len(a.pl.Queue()) != 1 || len(a.pl.PresentLyrics(false).Display.Lines) != 3 {
			t.Fatal("a display preference changed playback or source lyrics")
		}
	}
}

// Optional software frames expose the setting at normal/minimum sizes in both
// appearances and show the same synthetic lyrics with the preference on and off.
func TestPureLyricsFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES for visual review")
	}
	isolateUserData(t)
	for _, size := range []struct{ w, h int }{{780, 640}, {640, 420}} {
		for _, dark := range []bool{false, true} {
			a := newTestApp()
			a.settings = defaultSettings()
			a.preferences.Open("playback")
			tt := ui.NewTester(a.views.settings.Build, size.w, size.h)
			tt.SetDark(dark)
			tt.Advance(time.Second)
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("pure-settings-%dx%d-dark-%v.png", size.w, size.h, dark)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			a.settings.HotComments = false
			player.Cue(a.pl, []netease.Track{{ID: 7, Name: "夜航", Artists: []netease.Artist{{Name: "Alice"}}}})
			player.RefreshLyrics(a.pl, 7, "[00:00]夜航 - Alice\n[00:01]作词：甲\n[00:20]男：唱出明天", "[00:20]A new tomorrow", "")
			player.Hold(a.pl, 21*time.Second)
			for _, pure := range []bool{false, true} {
				a.settings.PureLyrics = pure
				lyricUI := ui.NewTester(func(c *ui.Context) {
					// Playback lyrics use white text over the playing page's dark backdrop.
					c.Root().Background(ui.RGB(24, 24, 28))
					ui.Column(c).Fill().Children(func() { a.views.playback.LyricsPane(c, a.pl.Current(), float32(size.h), ui.Rect{}, 0) })
				}, size.w, size.h)
				lyricUI.SetDark(dark)
				lyricUI.Advance(time.Second)
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("pure-lyrics-%dx%d-dark-%v-pure-%v.png", size.w, size.h, dark, pure)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, lyricUI.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			a.pl.Close()
		}
	}
}
