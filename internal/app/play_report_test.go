package app

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

type reportedPlay struct {
	id     int64
	played time.Duration
	end    string
}

// A song that starts is announced to NetEase, and the song the listener
// leaves is then reported with the position it reached and why it stopped, so
// that the play is filed with its length; an episode is not a song, and one
// heard for two seconds is not worth a play. The app reported nothing, so its
// plays never showed in what it played lately.
func TestSongsAreAnnouncedAndThenReported(t *testing.T) {
	song := func(id int64) netease.Track { return netease.Track{ID: id, Duration: 200000} }
	for _, tc := range []struct {
		name   string
		heard  netease.Track
		at     time.Duration
		then   func(a *app)
		starts []int64
		plays  []reportedPlay
	}{
		{
			name:   "moved on",
			heard:  song(7),
			at:     95 * time.Second,
			then:   func(a *app) { a.pl.Cue([]netease.Track{song(8)}) },
			starts: []int64{7, 8},
			plays:  []reportedPlay{{7, 95 * time.Second, netease.MovedOn}},
		},
		{
			// The song plays through, then starts over from its beginning.
			name:   "played through",
			heard:  song(7),
			at:     199 * time.Second,
			then:   func(a *app) { a.pl.Hold(0) },
			starts: []int64{7, 7},
			plays:  []reportedPlay{{7, 199 * time.Second, netease.Playthrough}},
		},
		{
			name:   "left after two seconds",
			heard:  song(7),
			at:     2 * time.Second,
			then:   func(a *app) { a.pl.Cue([]netease.Track{song(8)}) },
			starts: []int64{7, 8},
		},
		{
			name:   "episode",
			heard:  netease.Track{ID: 8, ProgramID: 900, Duration: 200000},
			at:     95 * time.Second,
			then:   func(a *app) { a.pl.Cue([]netease.Track{song(9)}) },
			starts: []int64{9},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.profile = &netease.Profile{}
			starts := make(chan int64, 8)
			plays := make(chan reportedPlay, 8)
			reportPlayStart = func(id int64) error {
				starts <- id
				return nil
			}
			reportPlay = func(id int64, played time.Duration, end string) error {
				plays <- reportedPlay{id, played, end}
				return nil
			}
			defer func() {
				reportPlayStart = netease.ReportPlayStart
				reportPlay = netease.ReportPlay
			}()

			tt := ui.NewTesterWith(func(c *ui.Context) { a.view(c) }, 800, 600, ui.TesterOptions{})
			a.pl.Cue([]netease.Track{tc.heard})
			a.pl.Hold(tc.at)
			tt.Frame()

			tc.then(a)
			tt.Frame()
			tt.Frame()
			time.Sleep(200 * time.Millisecond)

			if got := drain(starts); !slices.Equal(got, tc.starts) {
				t.Errorf("announced %v, want %v", got, tc.starts)
			}
			if got := drain(plays); !slices.Equal(got, tc.plays) {
				t.Errorf("reported %+v, want %+v", got, tc.plays)
			}
		})
	}
}

// drain takes what was sent, and nothing more.
func drain[T any](ch chan T) []T {
	var got []T
	for {
		select {
		case v := <-ch:
			got = append(got, v)
		default:
			return got
		}
	}
}
