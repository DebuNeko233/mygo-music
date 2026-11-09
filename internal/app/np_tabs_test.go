package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Resizing keeps navigation centered over the resting cover, 24 DIPs above it, with small windows keeping every button reachable.
func TestNowPlayingTabsFollowCover(t *testing.T) {
	for _, tr := range []netease.Track{
		{ID: 1, Name: "歌曲", Duration: 200000},
		{ID: 1, Name: "节目", ProgramID: 2, Description: "节目简介", Duration: 200000},
	} {
		t.Run(tr.Name, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.pl.Cue([]netease.Track{tr})
			a.pl.SetLyrics(nil)
			a.comments = commentState{track: tr.ID, loaded: true}
			a.nowPlaying = true
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			for _, size := range [][2]int{{1200, 800}, {1920, 1080}, {2560, 1440}, {1000, 1400}, {860, 560}, {1200, 800}} {
				tt.SetSize(size[0], size[1])
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				cover, ok := tt.Find("专辑封面")
				if !ok {
					t.Fatal("no cover after resizing")
				}
				first, ok := tt.Find(a.npTabs()[0].label)
				if !ok {
					t.Fatal("no first tab after resizing")
				}
				last, ok := tt.Find("评论")
				if !ok {
					t.Fatal("no comment tab after resizing")
				}
				groupW := last.X + last.W - first.X
				center := first.X + groupW/2
				scrub, ok := tt.Find("播放进度")
				if !ok || math.Abs(float64(cover.X+cover.W/2-scrub.X-scrub.W/2)) > 1 {
					t.Fatalf("at %v: cover %v is not centered in its playback column %v", size, cover, scrub)
				}
				volume, ok := tt.AccessByRole(ui.RoleSlider, "音量")
				if !ok || math.Abs(float64(cover.X+cover.W/2+6-volume.Bounds.X-volume.Bounds.W/2)) > 1 {
					t.Fatalf("at %v: volume track %v lost its optical offset under cover %v", size, volume.Bounds, cover)
				}
				want := max(12+groupW/2, min(cover.X+cover.W/2, float32(size[0])-12-groupW/2))
				if math.Abs(float64(center-want)) > 1 || math.Abs(float64(cover.Y-first.Y-first.H-24)) > 1 {
					t.Fatalf("at %v: tabs center %v (want %v), cover gap %v", size, center, want, cover.Y-first.Y-first.H)
				}
				if first.X < 11 || last.X+last.W > float32(size[0])-11 || first.Y < 30 {
					t.Fatalf("at %v: navigation overlaps the window edge: %v to %v", size, first, last)
				}
				if err := tt.Click("评论"); err != nil {
					t.Fatal(err)
				}
				if a.npPane != paneComments {
					t.Fatalf("at %v: comment button did not choose its pane", size)
				}
				a.comments.total = 12345678
				tt.Advance(time.Second)
				tt.Frame()
				moved, _ := tt.Find(a.npTabs()[0].label)
				end, _ := tt.Find(a.commentsTab())
				if math.Abs(float64((moved.X+end.X+end.W)/2-center)) > 1 || moved.Y != first.Y {
					t.Fatalf("at %v: the comment count moved the navigation's axis", size)
				}
				a.comments.total = 0
				a.npPane = a.npTabs()[0].pane
			}
		})
	}
}

// Showing the stage's style picker cannot shift the main navigation or overlap its comment button.
func TestNowPlayingStyleStaysBesideTabs(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "歌曲"}})
	a.comments = commentState{track: 1, loaded: true}
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 860, 560)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	first, _ := tt.Find("歌词")
	if err := tt.Click("律动"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, _ := tt.Find("歌词"); got.X != first.X || got.Y != first.Y {
		t.Fatal("showing the style picker moved the navigation")
	}
	style, ok := tt.Find("律动效果")
	comments, _ := tt.Find("评论")
	if !ok || style.X < comments.X+comments.W || style.X+style.W > 860 {
		t.Fatalf("style picker %v overlaps comments %v or the window edge", style, comments)
	}
}
