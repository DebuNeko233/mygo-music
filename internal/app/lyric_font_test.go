package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// Ambiguous Han retains the region's Chinese or Japanese glyph preference.
func TestHanLyricLanguage(t *testing.T) {
	for locale, want := range map[string]string{
		"zh-CN":      "zh-Hans",
		"zh-Hans-HK": "zh-Hans",
		"zh_TW":      "zh-Hant",
		"zh-Hant":    "zh-Hant",
		"zh-Hant-HK": "zh-Hant-HK",
		"yue-HK":     "zh-Hant-HK",
		"ja-JP":      "ja",
		"ko-KR":      "",
		"en-US":      "zh-Hans",
	} {
		if got := hanLyricLanguage(locale); got != want {
			t.Errorf("Han lyrics in %q use %q, want %q", locale, got, want)
		}
	}
}

// The two font controls save independently and resetting one restores its own automatic choice.
func TestSettingsFontChoicesApplyIndependently(t *testing.T) {
	names := lyricFontNames()
	if len(names) < 3 {
		t.Skip("two installed fonts are needed")
	}
	isolateUserData(t)
	a := newTestApp()
	a.settings = loadSettings()
	a.settings.LyricFont = "Desktop font"
	wantedUI, wantedLyrics := names[1], names[2]
	automatic := a.playbackTypeface(lyrics.JapaneseScript, "zh-CN")

	var settingsFont, mainFont string
	tt := ui.NewTester(func(c *ui.Context) {
		a.settingsView(c)
		settingsFont = c.Theme().Font
	}, 780, 640)
	main := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		mainFont = c.Theme().Font
	}, 1000, 700)
	showSettingsPage(t, tt, "外观")
	choose := func(label, font string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(font)
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		main.Frame()
	}

	choose("选择UI 字体", wantedUI)
	if settingsFont != wantedUI || mainFont != wantedUI || a.settings.UIFont != wantedUI {
		t.Fatalf("settings font %q, main font %q, stored UI font %q", settingsFont, mainFont, a.settings.UIFont)
	}
	if got := a.playbackTypeface(lyrics.JapaneseScript, "zh-CN"); got != automatic {
		t.Errorf("changing the UI altered the Japanese lyric font: %v", got)
	}
	if got := a.playbackTypeface(lyrics.UnknownScript, "zh-CN"); got.family != "system-ui" {
		t.Errorf("script without a lyric font took the custom UI font: %v", got)
	}

	choose("选择歌词字体", wantedLyrics)
	for _, script := range []lyrics.Script{lyrics.JapaneseScript, lyrics.HanScript, lyrics.LatinScript} {
		custom := a.playbackTypeface(script, "zh-CN")
		auto := playbackLyricTypeface(script, "zh-CN")
		first, _, _ := strings.Cut(custom.family, ",")
		if first != wantedLyrics || custom.lang != auto.lang || !strings.HasSuffix(custom.family, auto.family) {
			t.Errorf("custom font did not keep the original fallback and language for %v: %v", script, custom)
		}
	}
	settingsWrites.wait()
	if saved := loadSettings(); saved.UIFont != wantedUI || saved.PlaybackLyricFont != wantedLyrics || saved.LyricFont != "Desktop font" {
		t.Fatal("font choices did not survive reload independently of desktop lyrics")
	}

	choose("选择歌词字体", lyricFontSystem)
	if a.settings.PlaybackLyricFont != "" || a.playbackTypeface(lyrics.JapaneseScript, "zh-CN") != automatic || mainFont != wantedUI {
		t.Fatal("resetting lyrics did not restore automatic typography while keeping the UI font")
	}
	choose("选择UI 字体", lyricFontSystem)
	settingsWrites.wait()
	if saved := loadSettings(); saved.UIFont != "" || saved.PlaybackLyricFont != "" || settingsFont == wantedUI || mainFont == wantedUI {
		t.Fatal("resetting the UI font did not restore the system choice")
	}
}
