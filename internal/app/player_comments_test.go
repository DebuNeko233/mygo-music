package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The player's bubble opens the current song or episode's comments, not its lyrics or description.
func TestPlayerBarOpensComments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		track  netease.Track
		thread string
		pane   int
	}{
		{"song", netease.Track{ID: 13, Name: "歌", Duration: 600000}, "R_SO_4_13", paneStage},
		{"episode", netease.Track{ID: 13, Name: "节目", ProgramID: 3, Duration: 600000}, "A_DJ_1_3", paneQueue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserData(t)
			asked := make(chan string, 1)
			old := threadComments
			threadComments = func(thread string, _, _ int) (*netease.Comments, error) {
				asked <- thread
				return &netease.Comments{Total: 1, Latest: []netease.Comment{{ID: 9, Content: "好听"}}}, nil
			}
			defer func() { threadComments = old }()
			a := newTestApp()
			posts := testPosts(a)
			a.settings = defaultSettings()
			a.settings.LyricsStage = true
			a.npPane = tc.pane
			a.comments = commentState{track: 2, thread: "R_SO_4_2", loaded: true}
			a.pl.Cue([]netease.Track{tc.track})
			a.pl.SetLyrics(nil)
			a.pl.Hold(30 * time.Second)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if err := tt.Click("评论"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			tt.Frame()
			if thread := wait(t, asked, "the current track's comments"); thread != tc.thread {
				t.Fatalf("asked for %q, want %q", thread, tc.thread)
			}
			refreshPost(t, posts)
			tt.Frame()
			if !a.nowPlaying || a.npPane != paneComments || !a.comments.loaded || a.comments.track != tc.track.ID {
				t.Fatalf("Now Playing open %v on pane %d, comments %+v", a.nowPlaying, a.npPane, a.comments)
			}
			if !tt.HasText("好听") {
				t.Fatal("the requested comments did not reach the pane")
			}
			if !a.settings.LyricsStage || !a.pl.Playing() || a.pl.Position() != 30*time.Second || a.pl.Current().ID != tc.track.ID || len(a.pl.Queue()) != 1 {
				t.Fatal("opening comments changed the lyrics preference or playback")
			}
		})
	}
}

// The player's comment button does nothing when there is no current track.
func TestPlayerBarCommentsWithoutTrack(t *testing.T) {
	a := newTestApp()
	a.npPane = paneQueue
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("评论"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.nowPlaying || a.npPane != paneQueue || a.comments.loading {
		t.Fatal("opened or loaded comments without a track")
	}
}
