package playback

import (
	"testing"
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
		if got := HanLyricLanguage(locale); got != want {
			t.Errorf("Han lyrics in %q use %q, want %q", locale, got, want)
		}
	}
}
