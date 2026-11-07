package app

import (
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"testing"
)

// Whole-list Shuffle kept the first playable song fixed on both the playlist
// and daily pages; repeated clicks must vary the head without losing tracks.
func TestWholeListShuffleStartsRandomly(t *testing.T) {
	for _, page := range []string{"playlist", "daily"} {
		t.Run(page, func(t *testing.T) {
			isolateUserData(t)
			a := newTestApp()
			a.settings = defaultSettings()
			// Exercise the real buttons and queue without starting network/audio jobs.
			a.pl.SuspendCache()
			tracks := make([]netease.Track, 16)
			for i := range tracks {
				tracks[i] = netease.Track{ID: int64(i + 1), Name: "Song", Duration: 200000}
			}
			var view func(*ui.Context)
			if page == "playlist" {
				pl := &netease.Playlist{ID: 7, Name: "Fixture", Tracks: tracks}
				view = func(c *ui.Context) { a.playlistHeader(c, pl) }
			} else {
				a.daily = tracks
				view = a.dailyPage
			}
			tt := ui.NewTester(view, 1100, 800)
			heads := map[int64]bool{}
			for range 24 {
				if err := tt.Click("随机播放"); err != nil {
					t.Fatal(err)
				}
				current := a.pl.Current()
				if current == nil || !a.pl.Shuffle() {
					t.Fatal("shuffle did not start")
				}
				heads[current.ID] = true
				queue := a.pl.Queue()
				if len(queue) != len(tracks) {
					t.Fatal("lost tracks")
				}
				for i := range queue {
					if queue[i].ID != int64(i+1) || tracks[i].ID != int64(i+1) {
						t.Fatal("changed the source list")
					}
				}
			}
			if len(heads) < 2 {
				t.Fatalf("shuffle fixed its head: %v", heads)
			}
			// An explicitly picked song remains the shuffle's first song.
			a.pl.PlayQueue(tracks, 3)
			if a.pl.Current().ID != 4 {
				t.Fatal("shuffle displaced the picked song")
			}
		})
	}
}
