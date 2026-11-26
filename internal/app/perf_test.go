/*
验证播放栏、封面切换和正在播放页的帧预算，以及背景动画收尾。
*/

package app

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/lyrics"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	artcache "netease-music-mygo/internal/ui/view/artwork"
	playbackview "netease-music-mygo/internal/ui/view/playback"
	"netease-music-mygo/internal/ui/view/testutil"
)

// The player bar cover asks for frames while fading, then rests.
func TestPlayerBarCoverFrameBudget(t *testing.T) {
	a := newTestApp()
	before, after := "test://before", "test://after"
	for _, url := range []string{before, after} {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		a.art.Put(netease.Sized(url, barCoverPixels), &artcache.Image{Bitmap: ui.NewBitmap(img)})
	}
	track := &netease.Track{Album: netease.Album{PicURL: before}}
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		a.views.playback.PlayerBarArtwork(c, track)
	}, 64, 64)
	tt.Advance(time.Second)
	track.Album.PicURL = after
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("a new cover did not schedule animation frames")
	}
	tt.Advance(time.Second)
	resting := builds
	tt.Advance(time.Second)
	if tt.FramePending() || builds != resting {
		t.Fatal("a finished cover transition kept scheduling frames")
	}
}

// A cover slide moves through painter poses, then releases its outgoing
// layer and stops rebuilding. Count After wakeups as well as display frames.
func TestCoverSlideFrameBudget(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		a := newTestApp()
		player.Cue(a.pl, []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
		builds := 0
		tt := ui.NewTesterWith(func(c *ui.Context) {
			builds++
			ui.Column(c).Fill().Center().Children(func() { a.views.playback.NPCover(c, a.pl.Current(), nil, 220) })
		}, 320, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
		a.pl.Next()
		tt.Frame()
		tt.Advance(time.Second)
		if a.NPCoverState.Switching || a.NPCoverState.Previous != nil {
			t.Fatal("the outgoing cover outlived its animation")
		}
		before := builds
		tt.Advance(time.Second)
		if n := builds - before; n != 0 {
			t.Errorf("reduced=%v: a resting cover rebuilt %d times", reduced, n)
		}
	}
}

// playingApp is an app playing a track with lyrics, at a position the test
// sets with pl.Seek, and the number of times its view was built.
func playingApp() (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	player.Cue(a.pl, []netease.Track{tr})
	// A line every 10 seconds, sung over the first 5 of them.
	var lrc string
	for i := range 12 {
		lrc += "[0" + string(rune('0'+i*10/60)) + ":" + string(rune('0'+i*10%60/10)) + "0.00]一二三四五六\n"
	}
	lines := lyrics.Build(lrc, "", "")
	for i := range lines {
		l := &lines[i]
		l.End = l.At + 5*time.Second
		l.Words = lyrics.SpreadWords(l.Text, l.At, l.End)
	}
	player.SetLyrics(a.pl, lines)
	player.Hold(a.pl, 0)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	return a, tt, &builds
}

// Now Playing asks for frames only as fast as what it shows changes. The
// Tester runs timers as it settles a frame, so this counts only what does
// not change for a while: a frame log of the app (MYGO_FRAME_STATS) measures
// the rest.
func TestNowPlayingFrameBudget(t *testing.T) {
	a, tt, _ := playingApp()
	// Keep the desktop layout and frame cadence while reducing software raster work.
	tt.SetScale(0.25)
	if n := testutil.FramesAt(a.pl, tt, 12*time.Second); n > 3 {
		t.Errorf("a page under the player: %d frames a second", n)
	}
	// Its rows still, the wall asks for no more than the lyrics beside it
	// do: it moves only as it is moved.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.playbackState.Open, a.playbackState.Pane = true, playbackview.PaneLyrics
	lyricsFrames := testutil.FramesAt(a.pl, tt, 12*time.Second)
	a.playbackState.Pane = playbackview.PaneWall
	if n := testutil.FramesAt(a.pl, tt, 12*time.Second); n > lyricsFrames+3 {
		t.Errorf("the wall, still: %d frames a second, the lyrics %d", n, lyricsFrames)
	}
	tt.SetPreferences(ui.Preferences{})
	a.playbackState.Pane = playbackview.PaneStage
	a.settings.StageSparks = true
	for _, s := range playbackview.StageStyles {
		a.settings.StageStyle = s.ID
		if n := testutil.FramesAt(a.pl, tt, 18*time.Second); n > 12 {
			t.Errorf("the stage between lines, in %s: %d frames a second", s.Label, n)
		}
	}
}

// Another song's backdrop fades in over the last one's, which is then let
// go, so that the backdrop stops asking for frames.
func TestBackdropFadeEnds(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.art.Put(netease.Sized("a", 600)+"#backdrop", &artcache.Image{Tint: ui.RGB(200, 40, 40)})
	a.art.Put(netease.Sized("b", 600)+"#backdrop", &artcache.Image{Tint: ui.RGB(40, 40, 200)})
	q := a.pl.Queue()
	q[1].Album.PicURL, q[2].Album.PicURL = "a", "b"
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	if a.BackdropCache.Before == nil {
		t.Fatal("the backdrop changed songs without fading")
	}
	tt.Advance(playbackview.BackdropFade + 100*time.Millisecond)
	tt.Frame()
	if a.BackdropCache.Before != nil {
		t.Error("the last song's backdrop still fades after the fade")
	}
}

// A paused playlist must stop scheduling frames with its glass locate
// button visible, after locating hides it, and after its header changes.
func TestPlaylistLocateFrameBudget(t *testing.T) {
	_, pl, tt := locateFixture(t, 199, false)
	for _, stage := range []string{"offscreen", "visible", "header changed"} {
		switch stage {
		case "visible":
			clickLocate(t, tt)
		case "header changed":
			pl.Description = "A refreshed playlist description."
			tt.Frame()
		}
		tt.Move(100, 100)
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
		asked := 0
		for range 30 {
			tt.Advance(time.Second / 60)
			if tt.FramePending() {
				asked++
				tt.Frame()
			}
		}
		if asked > 1 {
			t.Fatalf("%s idle locate requested %d frames", stage, asked)
		}
	}
}

func hotCommentStageClock(style string, reduced bool, pos time.Duration) (*app, *ui.Tester, *int) {
	a, _, _ := quietApp(pos)
	a.settings.StageStyle = style
	builds := 0
	var start time.Time
	tt := ui.NewTesterWith(func(c *ui.Context) {
		if start.IsZero() {
			start = c.Now()
		}
		builds++
		player.Hold(a.pl, pos+c.Now().Sub(start))
		ui.Column(c).Fill().Children(func() { a.views.playback.Stage(c, a.pl.Current(), ui.RGB(255, 100, 100)) })
	}, 1200, 800, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return a, tt, &builds
}

// Reduce Motion must cover hot-comment entry, replacement and exit too:
// comments appear centered immediately, without fades, movement or frame loops.
func TestStageHotCommentsReducedMotion(t *testing.T) {
	for _, style := range playbackview.StageStyles {
		t.Run(style.Label, func(t *testing.T) {
			a, tt, builds := hotCommentStageClock(style.ID, true, 0)
			elapsed := time.Duration(0)
			for _, step := range []struct {
				at time.Duration
				id int64
			}{{1250 * time.Millisecond, 1}, {10150 * time.Millisecond, 2}, {19050 * time.Millisecond, 0}} {
				tt.Advance(step.at - elapsed)
				elapsed = step.at
				card, shown := tt.Find("查看评论")
				if step.id == 0 {
					if shown {
						t.Fatal("comment lingered after the quiet ended")
					}
					continue
				}
				if !shown || a.Quiet.Cm.ID != step.id || math.Abs(float64(card.Y-(800-card.H)/2)) > 0.5 {
					t.Fatalf("comment did not appear immediately in place: id=%d bounds=%v shown=%v", step.id, card, shown)
				}
				before := *builds
				tt.Advance(200 * time.Millisecond)
				elapsed += 200 * time.Millisecond
				if *builds != before {
					t.Fatalf("static hot comment requested %d extra builds", *builds-before)
				}
			}
		})
	}
}
