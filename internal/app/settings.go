package app

import (
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/desktop/shortcuts"
	"netease-music-mygo/internal/preferences"
)

// Settings retain UI-specific theme and key handling here; their schema and persistence live in preferences.
type settings preferences.Settings

func defaultSettings() settings { return settings(preferences.Default()) }
func loadSettings() settings {
	path, err := settingsPath()
	if err != nil {
		return defaultSettings()
	}
	loaded, ok := preferences.Load(path)
	s := settings(loaded)
	if ok {
		s.Shortcuts = shortcuts.Fill(s.Shortcuts)
	}
	return s
}
func (s *settings) clamp() {
	v := preferences.Settings(*s)
	v.Clamp()
	*s = settings(v)
	s.Shortcuts = shortcuts.Fill(s.Shortcuts)
}
func (s settings) save() error { return preferences.Settings(s).Save() }
func (s settings) saveSoon()   { preferences.Settings(s).SaveSoon() }

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
