package app

import (
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

const lyricFontSystem = "跟随系统"

// Font pickers share the system's installed family names.
var lyricFontNames = sync.OnceValue(func() []string {
	names := []string{lyricFontSystem}
	for _, f := range ui.FontFamilies() {
		if f.Name != "" && f.Name != lyricFontSystem {
			names = append(names, f.Name)
		}
	}
	return names
})

func (s *settings) uiTheme(base *ui.Theme) ui.Theme {
	theme := *base
	if s.UIFont != "" {
		theme.Font = s.UIFont
	}
	return theme
}

type lyricTypeface struct {
	family string
	lang   string
}

type lyricFontSelection struct {
	name     string
	families map[string]string
}

func (a *app) playbackTypeface(script lyrics.Script, locale string) lyricTypeface {
	font := playbackLyricTypeface(script, locale)
	chosen := a.settings.PlaybackLyricFont
	if font.family == "" && (chosen != "" || a.settings.UIFont != "") {
		font.family = "system-ui"
	}
	if chosen == "" {
		return font
	}
	selection := &a.playbackFont
	if selection.name != chosen {
		selection.name = chosen
		selection.families = make(map[string]string)
	}
	family, ok := selection.families[font.family]
	if !ok {
		family = font.family
		if first, _, _ := strings.Cut(family, ","); first != chosen {
			family = chosen + ", " + family
		}
		selection.families[font.family] = family
	}
	font.family = family
	return font
}

func playbackLyricTypeface(script lyrics.Script, locale string) lyricTypeface {
	switch script {
	case lyrics.JapaneseScript:
		return lyricTypeface{lyricDocumentFont("ja"), "ja"}
	case lyrics.HanScript:
		language := hanLyricLanguage(locale)
		return lyricTypeface{lyricDocumentFont(language), language}
	case lyrics.LatinScript:
		return lyricTypeface{family: lyricDocumentFont("en-US")}
	default:
		return lyricTypeface{}
	}
}

func hanLyricLanguage(locale string) string {
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

func (font lyricTypeface) apply(e ui.Element) ui.Element {
	e.Lang(font.lang)
	if font.family != "" {
		e.Font(font.family)
	}
	return e
}

func (font lyricTypeface) width(c *ui.Context, s string, size float32, weight int) float32 {
	width, _ := c.MeasureText(0, ui.Span{Text: s, Font: font.family, Lang: font.lang, Size: size, Weight: weight})
	return width
}

func (font lyricTypeface) translationFont(themeFamily string) ui.Font {
	family := font.family
	if family == "" {
		family = themeFamily
	}
	return ui.Font{Family: family, Lang: font.lang, Size: 17, Weight: 600}
}
