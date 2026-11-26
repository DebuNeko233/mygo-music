/*
为独立播放视图测试提供最小播放器、歌词和字幕夹具。
*/

package playback

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/features/preferences"
	captionsvm "netease-music-mygo/internal/ui/viewmodel/captions"
	playbackvm "netease-music-mygo/internal/ui/viewmodel/playback"
)

func lyricPaneView(t *testing.T, lines []lyrics.Line, w, h int) (*View, *ui.Tester) {
	t.Helper()
	a := newTestView(t)
	*a.Config = preferences.Default()
	a.Config.LyricEdgeFade = false
	a.Config.HotComments = false
	player.Cue(a.Player(), []netease.Track{{ID: 1, Duration: 200000}})
	player.SetLyrics(a.Player(), lines)
	player.Hold(a.Player(), 0)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		_, height := c.Size()
		ui.Column(c).Fill().Children(func() { a.LyricsPane(c, a.Player().Current(), height, ui.Rect{}, 0) })
	}, w, h, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	return a, tt
}

// newTestView supplies only playback state and a quiet player. Individual tests
// inject any extra callbacks they exercise; missing app wiring stays visible.
func newTestView(t *testing.T) *View {
	t.Helper()
	pl := newTestPlayer(t)
	return &View{
		State: &State{QueueAt: -1}, Reading: &playbackvm.LyricReading{At: -1},
		Playback: &playbackvm.Playback{}, Captions: &captionsvm.Captions{}, Config: &preferences.Settings{},
		Player: func() *player.Player { return pl },
	}
}

func newTestPlayer(t *testing.T) *player.Player {
	t.Helper()
	pl := player.New(func(func()) {})
	pl.SetVolume(0.5)
	t.Cleanup(pl.Close)
	return pl
}

// captionView has a ready model and a supplied transcript; it never starts recognition.
func captionView(t *testing.T) *View {
	t.Helper()
	v := newTestView(t)
	*v.Config = preferences.Default()
	player.Cue(v.Player(), []netease.Track{{ID: 7, ProgramID: 42, Duration: 600000}})
	v.ModelReady = func() bool { return true }
	v.CaptionsWriting = func(pos time.Duration) bool { return v.Captions.Writing(pos, false) }
	return v
}
