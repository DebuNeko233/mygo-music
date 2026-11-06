package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type settings struct {
	// Notch shows the quick player around the notch: the listener turns
	// it on.
	Notch bool `json:"notch"`
	// NotchActivity shows the cover and the music's levels beside the
	// notch while music plays, which hides the menu bar there.
	NotchActivity bool `json:"notchActivity"`
	// NotchVirtual keeps a notch of the app's own showing at the top of a
	// display without one; else it shows only while the pointer asks.
	NotchVirtual bool `json:"notchVirtual"`
	// NotchGreeted says the notch showed itself, once, as the first song
	// played (greet).
	NotchGreeted bool `json:"notchGreeted"`
	// Quality is the level of the streams: standard, higher, exhigh,
	// lossless or hires.
	Quality string `json:"quality"`
	// Appearance is light, dark, or system to follow the system.
	Appearance string `json:"appearance"`
	// Smoke makes the progress bars comets trailing smoke as music plays,
	// for a few percent of a core: the listener turns it on.
	Smoke bool `json:"smoke"`
	// LyricsStage shows lyrics on the stage, Folia's style, in Now
	// Playing, rather than scrolling.
	LyricsStage bool `json:"lyricsStage"`
	// StageStyle is how the stage sets its lyrics (stageStyles): empty for
	// Folia's classic, or the id of another of its visualizers.
	StageStyle string `json:"stageStyle,omitempty"`
	// StageSparks throws sparks off each word on the stage as it is sung,
	// whatever its style.
	StageSparks bool `json:"stageSparks,omitempty"`
	// Volume is where the volume was left, from 0 to 1.
	Volume float64 `json:"volume"`
	// Normalize evens out the loudness of tracks, by what NetEase
	// measured of each.
	Normalize bool `json:"normalize"`
	// HotComments shows a song's hot comments in its quiet parts, in
	// place of the lyrics (quietComment): the listener turns it on.
	HotComments bool `json:"hotComments"`
	// Fog fills Now Playing's backdrop with fog of the cover's color,
	// rising and swelling with the bass: an experiment the listener turns
	// on.
	Fog bool `json:"fog"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// Captions writes down episodes as they play, for their captions,
	// once the listener has downloaded the model.
	Captions bool `json:"captions"`
	// CacheMB is how many megabytes the songs kept on disk may take; 0
	// keeps none.
	CacheMB int `json:"cacheMB"`
	// CacheDir is where those songs are kept; empty for the app's own
	// cache directory, as the system gives it.
	CacheDir string `json:"cacheDir,omitempty"`
	// Accent is the colour the app takes, from accents; empty for the one
	// its appearance gives.
	Accent string `json:"accent,omitempty"`
	// BackdropHome and BackdropPlaying are the pictures the pages stand
	// on, by path, in the order they come: the home page's and Now
	// Playing's. Empty for pages that stand on none.
	BackdropHome    []string `json:"backdropHome,omitempty"`
	BackdropPlaying []string `json:"backdropPlaying,omitempty"`
	// BackdropOpacity is how much of those pictures shows over the page's
	// own colour, from 0 to 1.
	BackdropOpacity float64 `json:"backdropOpacity,omitempty"`
	// LyricWindow shows the lyrics in a window of their own, over every
	// other window: the listener turns it on.
	LyricWindow bool `json:"lyricWindow,omitempty"`
	// LyricLines is how many lines the window shows at once: one or two.
	LyricLines int `json:"lyricLines,omitempty"`
	// LyricLocked keeps the lyrics window where it is: it is dragged by
	// its lines, and locked, it has none that move it.
	LyricLocked bool `json:"lyricLocked,omitempty"`
	// LyricSize is how large the lines stand, in points, and LyricWeight
	// how heavy they are.
	LyricSize   float64 `json:"lyricSize,omitempty"`
	LyricWeight int     `json:"lyricWeight,omitempty"`
	// LyricFont is the family the lines show in, by the name lyricFonts
	// gives it; empty for the system's.
	LyricFont string `json:"lyricFont,omitempty"`
	// LyricPlayed and LyricIdle colour the line being sung and those not
	// yet, by the names lyricColors gives them.
	LyricPlayed string `json:"lyricPlayed,omitempty"`
	LyricIdle   string `json:"lyricIdle,omitempty"`
	// LyricTrans shows each line's translation under it, in
	// LyricTransFont and LyricTransColor.
	LyricTrans      bool   `json:"lyricTrans,omitempty"`
	LyricTransFont  string `json:"lyricTransFont,omitempty"`
	LyricTransColor string `json:"lyricTransColor,omitempty"`
	// LyricOutlineWidth is the text outline thickness in pixels, 0 for none.
	// LyricOutlineColor is the outline colour, by the names lyricColors gives.
	LyricOutlineWidth float64 `json:"lyricOutlineWidth,omitempty"`
	LyricOutlineColor string  `json:"lyricOutlineColor,omitempty"`
	// LyricX and LyricY are where the lyrics window was left, in screen
	// points; 0 and 0 for never placed. LyricWidth and LyricHeight are how
	// large it was left, which is how large it opens again, and at least as
	// large as the lines need.
	LyricX      int `json:"lyricX,omitempty"`
	LyricY      int `json:"lyricY,omitempty"`
	LyricWidth  int `json:"lyricWidth,omitempty"`
	LyricHeight int `json:"lyricHeight,omitempty"`
	// SearchHistory keeps submitted keywords, most recent first.
	SearchHistory []string `json:"searchHistory,omitempty"`

	path string // where they were loaded from, and are saved to
}

func defaultSettings() settings {
	return settings{
		NotchActivity: true, Quality: "exhigh", Appearance: "system", Volume: 0.8, Normalize: true,
		AppIcon: "dock-and-menubar", CacheMB: 2048, Captions: true, BackdropOpacity: 0.4,
		LyricSize: 26, LyricWeight: 700, LyricPlayed: "白色", LyricIdle: "银灰",
		LyricTrans: true, LyricTransColor: "银灰", LyricLines: 2,
		LyricOutlineWidth: 0, LyricOutlineColor: "墨黑",
	}
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// oldName is what the app was called before it took its bundle's name, and
// kept its data under.
const oldName = "网易云音乐"

// moveOldData moves the user data and the song cache kept under oldName to
// the directories of the app's name, unless those hold something already:
// the login, settings and where playback was left go on. It must run before
// anything reads them.
func moveOldData() {
	for _, name := range []mygo.PathName{mygo.PathUserData, mygo.PathCache} {
		dir, err := mygo.App.Path(name)
		if err != nil {
			continue
		}
		moveDir(filepath.Join(filepath.Dir(dir), oldName), dir)
	}
}

// moveDir moves the directory old to dir when dir is empty, as Path made
// it, and old exists.
func moveDir(old, dir string) {
	if old == dir {
		return
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if os.Remove(dir) == nil {
		if err := os.Rename(old, dir); err != nil {
			os.MkdirAll(dir, 0o700)
		}
	}
}

// loadSettings reads the settings, or returns the defaults.
func loadSettings() settings {
	s := defaultSettings()
	path, err := settingsPath()
	if err != nil {
		return s
	}
	s.path = path
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		// On parse error, return defaults with path set for saving
		return s
	}
	// Clamp values to valid ranges
	s.clamp()
	return s
}

// clamp ensures all settings values are within valid ranges.
func (s *settings) clamp() {
	s.Volume = max(0, min(1, s.Volume))
	s.CacheMB = max(0, s.CacheMB)
	s.BackdropOpacity = max(0, min(1, s.BackdropOpacity))
	s.LyricSize = max(12, min(72, s.LyricSize))
	s.LyricWeight = max(100, min(900, s.LyricWeight))
	s.LyricOutlineWidth = max(0, min(2.5, s.LyricOutlineWidth))
	s.LyricLines = max(1, min(4, s.LyricLines))
	if s.LyricWidth < 0 {
		s.LyricWidth = 0
	}
	if s.LyricHeight < 0 {
		s.LyricHeight = 0
	}
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s settings) save() error {
	path := s.path
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Written beside the settings and moved onto them, so that what is
	// there is either the old settings or the new ones, never half of
	// them: a crash as they are written loses nothing.
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
