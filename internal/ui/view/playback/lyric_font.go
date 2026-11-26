/*
解析歌词字体与汉字区域偏好，提供主歌词和翻译使用的字体选择。
*/

package playback

import (
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
)

const LyricFontSystem = "跟随系统"

// Font pickers share the system's installed family names.
var LyricFontNames = sync.OnceValue(func() []string {
	names := []string{LyricFontSystem}
	for _, f := range ui.FontFamilies() {
		if f.Name != "" && f.Name != LyricFontSystem {
			names = append(names, f.Name)
		}
	}
	return names
})

type LyricTypeface struct {
	Family string
	Lang   string
}

type lyricFontSelection struct {
	name     string
	families map[string]string
}

func (a *View) PlaybackTypeface(script lyrics.Script, locale string) LyricTypeface {
	font := PlaybackLyricTypeface(script, locale)
	chosen := a.Config.PlaybackLyricFont
	if font.Family == "" && (chosen != "" || a.Config.UIFont != "") {
		font.Family = "system-ui"
	}
	if chosen == "" {
		return font
	}
	selection := a.PlaybackFont
	if selection.name != chosen {
		selection.name = chosen
		selection.families = make(map[string]string)
	}
	family, ok := selection.families[font.Family]
	if !ok {
		family = font.Family
		if first, _, _ := strings.Cut(family, ","); first != chosen {
			family = chosen + ", " + family
		}
		selection.families[font.Family] = family
	}
	font.Family = family
	return font
}

func PlaybackLyricTypeface(script lyrics.Script, locale string) LyricTypeface {
	switch script {
	case lyrics.JapaneseScript:
		return LyricTypeface{lyricDocumentFont("ja"), "ja"}
	case lyrics.HanScript:
		language := HanLyricLanguage(locale)
		return LyricTypeface{lyricDocumentFont(language), language}
	case lyrics.LatinScript:
		return LyricTypeface{Family: lyricDocumentFont("en-US")}
	default:
		return LyricTypeface{}
	}
}

func HanLyricLanguage(locale string) string {
	base, variant := lyricLocalePart(locale)
	switch {
	case strings.EqualFold(base, "ja"):
		return "ja"
	case strings.EqualFold(base, "ko"):
		return "" // Keep the Korean system fallback for ambiguous Han.
	case strings.EqualFold(base, "zh") || strings.EqualFold(base, "yue"):
		script, _ := lyricLocalePart(variant)
		if strings.EqualFold(script, "Hans") {
			return "zh-Hans"
		}
		traditional := strings.EqualFold(base, "yue") || strings.EqualFold(script, "Hant")
		for variant != "" {
			var part string
			part, variant = lyricLocalePart(variant)
			if strings.EqualFold(part, "HK") || strings.EqualFold(part, "MO") {
				return "zh-Hant-HK"
			}
			traditional = traditional || strings.EqualFold(part, "TW")
		}
		if traditional {
			return "zh-Hant"
		}
	}
	return "zh-Hans"
}

func lyricLocalePart(tag string) (string, string) {
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		return tag[:i], tag[i+1:]
	}
	return tag, ""
}

func (font LyricTypeface) apply(e ui.Element) ui.Element {
	e.Lang(font.Lang)
	if font.Family != "" {
		e.Font(font.Family)
	}
	return e
}

func (font LyricTypeface) width(c *ui.Context, s string, size float32, weight int) float32 {
	width, _ := c.MeasureText(0, ui.Span{Text: s, Font: font.Family, Lang: font.Lang, Size: size, Weight: weight})
	return width
}

func (font LyricTypeface) translationFont(themeFamily string) ui.Font {
	family := font.Family
	if family == "" {
		family = themeFamily
	}
	return ui.Font{Family: family, Lang: font.Lang, Size: 17, Weight: 600}
}
