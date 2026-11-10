package app

import (
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

// The song the listener leaves is reported with the position it reached and
// why it stopped, so that NetEase files the play with its length; an episode
// is not a song, and one heard for two seconds is not worth a play. The app
// reported nothing, so its plays never showed in what it played lately.
func TestLeavingSongsReportsThePlays(t *testing.T) {
	song := func(id int64) netease.Track { return netease.Track{ID: id, Duration: 200000} }
	for _, tc := range []struct {
		name  string
		heard netease.Track
		at    time.Duration
		then  func(a *app)
		want  []reportedPlay
	}{
		{
			// Another song takes its place.
			name:  "moved on",
			heard: song(7),
			at:    95 * time.Second,
			then:  func(a *app) { a.pl.Cue([]netease.Track{song(8)}) },
			want:  []reportedPlay{{7, 95 * time.Second, netease.MovedOn}},
		},
		{
			// The song starts over from its beginning after playing through.
			name:  "played through",
			heard: song(7),
			at:    199 * time.Second,
			then:  func(a *app) { a.pl.Hold(0) },
			want:  []reportedPlay{{7, 199 * time.Second, netease.Playthrough}},
		},
		{
			name:  "left after two seconds",
			heard: song(7),
			at:    2 * time.Second,
			then:  func(a *app) { a.pl.Cue([]netease.Track{song(8)}) },
			want:  nil,
		},
		{
			name:  "episode",
			heard: netease.Track{ID: 8, ProgramID: 900, Duration: 200000},
			at:    95 * time.Second,
			then:  func(a *app) { a.pl.Cue([]netease.Track{song(9)}) },
			want:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.profile = &netease.Profile{}
			calls := make(chan reportedPlay, 4)
			reportPlay = func(id int64, played time.Duration, end string) error {
				calls <- reportedPlay{id, played, end}
				return nil
			}
			defer func() { reportPlay = netease.ReportPlay }()

			tt := ui.NewTesterWith(func(c *ui.Context) { a.view(c) }, 800, 600, ui.TesterOptions{})
			a.pl.Cue([]netease.Track{tc.heard})
			a.pl.Hold(tc.at)
			tt.Frame()

			tc.then(a)
			tt.Frame()

			if tc.want == nil {
				select {
				case got := <-calls:
					t.Fatalf("reported %+v, want none", got)
				case <-time.After(200 * time.Millisecond):
				}
				return
			}
			select {
			case got := <-calls:
				if got != tc.want[0] {
					t.Fatalf("reported %+v, want %+v", got, tc.want[0])
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("play went unreported, want %+v", tc.want[0])
			}
			tt.Frame()
			select {
			case got := <-calls:
				t.Fatalf("reported %+v twice", got)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}
