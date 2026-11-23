package app

import (
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/preferences"
	"netease-music-mygo/internal/userdata"
)

// Settings retain UI-specific theme and key handling here; their schema and persistence live in preferences.
type settings preferences.Settings
type shortcutKeys = preferences.ShortcutKeys

func defaultSettings() settings { return settings(preferences.Default()) }
func loadSettings() settings {
	path, err := settingsPath()
	if err != nil {
		return defaultSettings()
	}
	loaded, ok := preferences.Load(path)
	s := settings(loaded)
	if ok {
		s.Shortcuts = fillShortcutKeys(s.Shortcuts)
	}
	return s
}
func (s *settings) clamp() {
	v := preferences.Settings(*s)
	v.Clamp()
	*s = settings(v)
	s.Shortcuts = fillShortcutKeys(s.Shortcuts)
}
func (s settings) save() error                  { return preferences.Settings(s).Save() }
func (s settings) saveSoon()                    { preferences.Settings(s).SaveSoon() }
func writeUserData(path string, b []byte) error { return userdata.Write(path, b) }

// filled gives every action the key it has: the one the settings keep, when
// it is a key that set takes, else its default. An action the settings keep
// as empty has none.
func fillShortcutKeys(k shortcutKeys) shortcutKeys {
	filled := shortcutKeys{InApp: map[string]string{}, Global: map[string]string{}}
	for _, h := range hotkeys {
		filled.InApp[h.name] = h.inApp
		filled.Global[h.name] = h.global
		if acc, ok := k.InApp[h.name]; ok {
			if _, _, parsed := parseAccelerator(acc); parsed || acc == "" {
				filled.InApp[h.name] = acc
			}
		}
		if acc, ok := k.Global[h.name]; ok {
			if validGlobal(acc) || acc == "" {
				filled.Global[h.name] = acc
			}
		}
	}
	return filled
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// takeAccent takes the accent the listener chose, which the pages paint
// with.
func (s settings) takeAccent() {
	accentChosen = s.Accent
}

// applyAppearance makes the windows light, dark, or follow the system, and
// takes the accent the listener chose.
func (s settings) applyAppearance() {
	s.takeAccent()
	switch s.Appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// qualityLevel is the stream level NetEase names a quality by.
var qualityLevels = []struct{ level, label string }{
	{"standard", "标准（128k）"},
	{"higher", "较高（192k）"},
	{"exhigh", "极高（320k）"},
	{"lossless", "无损（FLAC）"},
	{"hires", "Hi-Res"},
}

// cacheLimits are the sizes the song cache may take, in megabytes.
var cacheLimits = []struct {
	mb    int
	label string
}{
	{0, "关闭"},
	{1024, "1 GB"},
	{2048, "2 GB"},
	{5120, "5 GB"},
	{10240, "10 GB"},
}
