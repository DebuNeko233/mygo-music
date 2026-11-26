/*
验证汉字区域偏好，以及字体设置对应的字体族。
*/

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

// A font setting uses the selected system family, or the default family
// when no family is selected.
func TestTheFamiliesAFontSettingStandsFor(t *testing.T) {
	if got := LyricFamily(LyricFontSystem); got != "" {
		t.Errorf("the system's own stands for %q", got)
	}
	if got := LyricFamily(""); got != "" {
		t.Errorf("no choice stands for %q", got)
	}
	names := LyricFontNames()
	if len(names) > 1 {
		if got := LyricFamily(names[1]); got != names[1] {
			t.Errorf("the system's font %q stands for %q", names[1], got)
		}
	}
}
