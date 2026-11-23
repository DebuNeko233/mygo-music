package preferences

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"netease-music-mygo/internal/userdata"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type Settings struct {
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
	Quality            string `json:"quality"`
	DownloadQuality    string `json:"downloadQuality"`
	DownloadNameFormat string `json:"downloadNameFormat,omitempty"`
	DownloadDirectory  string `json:"downloadDirectory,omitempty"`
	DownloadSidecar    bool   `json:"downloadSidecar"`
	// Appearance is light, dark, or system to follow the system.
	Appearance string `json:"appearance"`
	// UIFont overrides the default typeface for the app's interface.
	UIFont string `json:"uiFont,omitempty"`
	// PlaybackLyricFont is the preferred face for in-app lyrics and translations.
	PlaybackLyricFont string `json:"playbackLyricFont,omitempty"`
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
	// LyricEdgeFade enables the experimental effects in the scrolling lyrics pane.
	LyricEdgeFade bool `json:"lyricEdgeFade"`
	// LyricSpectrum shows the moving spectrum behind lyrics and the stage.
	LyricSpectrum bool `json:"lyricSpectrum,omitempty"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// Captions writes down episodes as they play, for their captions,
	// once the listener has downloaded the model.
	Captions bool `json:"captions"`
	// CacheMB is how many megabytes the songs kept on disk may take; 0
	// keeps none.
	CacheMB int `json:"cacheMB"`
	// CacheDir contains songs, artwork and browsing data; empty uses the
	// system cache.
	CacheDir string `json:"cacheDir,omitempty"`
	// Accent is the colour the app takes, from accents; empty for the one
	// its appearance gives.
	Accent string `json:"accent,omitempty"`
	// BackdropHome and BackdropPlaying are the pictures the pages stand
	// on, by path, in the order they come: the home page's and Now
	// Playing's. Empty for pages that stand on none.
	BackdropHome    []string `json:"backdropHome,omitempty"`
	BackdropPlaying []string `json:"backdropPlaying,omitempty"`
	// Each page blends its pictures over its own colour, from 0 to 1.
	BackdropHomeOpacity    float64 `json:"backdropHomeOpacity"`
	BackdropPlayingOpacity float64 `json:"backdropPlayingOpacity"`
	// LyricWindow shows the lyrics in a window of their own, over every
	// other window: the listener turns it on.
	LyricWindow bool `json:"lyricWindow,omitempty"`
	// LyricKaraoke highlights the current desktop line word by word when
	// the song supplies word timings.
	LyricKaraoke bool `json:"lyricKaraoke,omitempty"`
	// LyricLines is how many lines the window shows at once: one or two.
	LyricLines int `json:"lyricLines,omitempty"`
	// LyricAlign and LyricNextAlign align the first and second desktop lines:
	// 0 for left, 1 for centre and 2 for right. A single line uses LyricAlign.
	LyricAlign     int `json:"lyricAlign"`
	LyricNextAlign int `json:"lyricNextAlign"`
	// LyricLineGap is the space between two desktop lines in points.
	LyricLineGap float64 `json:"lyricLineGap,omitempty"`
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
	// Shortcuts are the keys the listener gave the actions of hotkeys, by
	// action: those that work in the app, and those that work wherever it
	// is. An action missing keeps its default, and one that is empty has no
	// key at all.
	Shortcuts ShortcutKeys `json:"shortcuts,omitempty"`

	Path string `json:"-"` // where they were loaded from, and are saved to
}

// shortcutKeys are the two sets of keys the settings keep, by action.
type ShortcutKeys struct {
	InApp  map[string]string `json:"inApp,omitempty"`
	Global map[string]string `json:"global,omitempty"`
}

func Default() Settings {
	return Settings{
		NotchActivity: true, Quality: "exhigh", DownloadQuality: "exhigh", Appearance: "system", Volume: 0.8, Normalize: true,
		AppIcon: "dock-and-menubar", CacheMB: 2048, Captions: true,
		BackdropHomeOpacity: 0.4, BackdropPlayingOpacity: 0.4,
		LyricSize: 26, LyricWeight: 700, LyricPlayed: "白色", LyricIdle: "银灰",
		LyricTrans: true, LyricTransColor: "银灰", LyricLines: 2,
		LyricAlign: 1, LyricNextAlign: 1, LyricLineGap: 8, LyricEdgeFade: true,
		LyricOutlineWidth: 0, LyricOutlineColor: "墨黑",
	}
}

// loadSettings reads the settings, or returns the defaults.
func Load(path string) (Settings, bool) {
	s := Default()
	s.Path = path
	b, err := os.ReadFile(path)
	if err != nil {
		return s, false
	}
	decoded := s
	if err := json.Unmarshal(b, &decoded); err != nil {
		// Invalid JSON may partially populate decoded. Reject the whole file.
		return s, false
	}
	decoded.Clamp()
	return decoded, true
}

// clamp ensures all settings values are within valid ranges.
func (s *Settings) Clamp() {
	s.Volume = max(0, min(1, s.Volume))
	s.CacheMB = max(0, s.CacheMB)
	s.BackdropHomeOpacity = max(0, min(1, s.BackdropHomeOpacity))
	s.BackdropPlayingOpacity = max(0, min(1, s.BackdropPlayingOpacity))
	s.LyricSize = max(12, min(72, s.LyricSize))
	s.LyricWeight = max(100, min(900, s.LyricWeight))
	s.LyricOutlineWidth = max(0, min(2.5, s.LyricOutlineWidth))
	s.LyricLines = max(1, min(4, s.LyricLines))
	s.LyricAlign = max(0, min(2, s.LyricAlign))
	s.LyricNextAlign = max(0, min(2, s.LyricNextAlign))
	s.LyricLineGap = max(0, min(40, s.LyricLineGap))
	if s.LyricWidth < 0 {
		s.LyricWidth = 0
	}
	if s.LyricHeight < 0 {
		s.LyricHeight = 0
	}
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s Settings) Save() error {
	if s.Path == "" {
		return nil
	}
	// A transactional change (such as moving the cache) must land after
	// earlier slider updates, even while those are still being batched.
	settingsWrites.wait()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return userdata.Write(s.Path, b)
}

// saveSoon keeps the settings without waiting for the disk. A key that steps
// the lyric size or the volume takes them again on every repeat, and the
// window draws between the repeats: none of those may wait on a write and a
// sync. What reaches the disk is the settings as they stand when the write
// runs. A short batching interval combines separate slider/key events even
// when each write would finish before the next event arrives.
func (s Settings) SaveSoon() {
	if s.Path == "" {
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		log.Printf("settings: %v", err)
		return
	}
	settingsWrites.submit(settingsWrite{path: s.Path, data: b})
}

// settingsWrite is the settings as they are to be written.
type settingsWrite struct {
	path string
	data []byte
}

// settingsWrites writes them, on a goroutine of its own.
var settingsWrites = settingsSaver{write: userdata.Write}

const settingsWriteDelay = 250 * time.Millisecond

type settingsSaver struct {
	mu       sync.Mutex
	pending  *settingsWrite
	done     chan struct{}
	flush    chan struct{}
	flushing bool
	write    func(string, []byte) error
}

func (w *settingsSaver) submit(job settingsWrite) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = &job
	if w.done == nil {
		w.done = make(chan struct{})
		w.flush, w.flushing = make(chan struct{}), false
		go w.run(w.flush)
	}
}

func (w *settingsSaver) run(flush <-chan struct{}) {
	timer := time.NewTimer(settingsWriteDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-flush:
		}
		w.mu.Lock()
		job := w.pending
		w.pending = nil
		w.mu.Unlock()
		if err := w.write(job.path, job.data); err != nil {
			log.Printf("settings: %v", err)
		}
		w.mu.Lock()
		if w.pending == nil {
			close(w.done)
			w.done = nil
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		// Save periodically during a long drag; waiting for the last input
		// event could postpone persistence indefinitely.
		timer.Reset(settingsWriteDelay)
	}
}

// wait flushes without the batching delay before shutdown or a transactional
// save. Older queued settings must not land after the newer synchronous save.
func (w *settingsSaver) wait() {
	w.mu.Lock()
	done := w.done
	if done != nil && !w.flushing {
		w.flushing = true
		close(w.flush)
	}
	w.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Flush persists pending input before shutdown or a transactional save.
func Flush() { settingsWrites.wait() }
