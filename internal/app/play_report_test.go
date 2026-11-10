package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A song that begins playing is reported to NetEase, once, so that it counts
// in what the listener played lately; an episode is not a song. The app
// reported nothing, so its plays never showed there.
func TestPlayingReportsSongsOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		play netease.Track
		want int64
	}{
		{"song", netease.Track{ID: 7, Name: "Song"}, 7},
		{"episode", netease.Track{ID: 8, ProgramID: 900, Name: "Episode"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.profile = &netease.Profile{}
			calls := make(chan int64, 4)
			reportPlay = func(id int64) error {
				calls <- id
				return nil
			}
			defer func() { reportPlay = netease.ReportPlay }()

			tt := ui.NewTesterWith(func(c *ui.Context) { a.view(c) }, 800, 600, ui.TesterOptions{})
			a.pl.Cue([]netease.Track{tc.play})
			a.pl.Hold(0)
			tt.Frame()
			if tc.want == 0 {
				select {
				case id := <-calls:
					t.Fatalf("reported %d, which is not a song", id)
				case <-time.After(200 * time.Millisecond):
				}
				return
			}
			select {
			case id := <-calls:
				if id != tc.want {
					t.Fatalf("reported %d, want %d", id, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("song %d went unreported", tc.want)
			}
			tt.Frame()
			select {
			case id := <-calls:
				t.Fatalf("reported %d twice", id)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}
