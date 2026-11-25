package playback

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/testutil"
)

// Flung far to the left, the cover flies off for the next song; to the
// right, for the one before, however far this one has played.
func TestFlingCover(t *testing.T) {
	a, tt, r := coverView(t)
	if a.Player().Current().ID != 2 {
		t.Fatalf("playing %d", a.Player().Current().ID)
	}
	testutil.Drag(tt, r, -r.W*0.8, 0)
	if id := a.Player().Current().ID; id != 3 {
		t.Fatalf("flung left: playing %d", id)
	}
	if r2, ok := tt.Find("专辑封面"); !ok || r2.X < r.X-2 || r2.X > r.X+2 {
		t.Errorf("the next cover rests at %v, not %v", r2, r)
	}
	testutil.Drag(tt, r, r.W*0.8, 0)
	if id := a.Player().Current().ID; id != 2 {
		t.Errorf("flung right: playing %d", id)
	}
}

// A drag too short, let go still, springs back, the song playing on.
func TestCoverSpringsBack(t *testing.T) {
	a, tt, r := coverView(t)
	testutil.Drag(tt, r, -r.W*0.3, 300*time.Millisecond)
	if id := a.Player().Current().ID; id != 2 {
		t.Fatalf("a short drag changed the song to %d", id)
	}
	if r2, _ := tt.Find("专辑封面"); r2.X < r.X-2 || r2.X > r.X+2 {
		t.Errorf("the cover rests at %v, not %v", r2, r)
	}
}

// Two fingers swiping across the trackpad fling it too.
func TestSwipeCover(t *testing.T) {
	a, tt, r := coverView(t)
	x, y := r.X+r.W/2, r.Y+r.H/2
	for range 10 {
		tt.Scroll(x, y, r.W*0.06, 0)
		tt.Advance(16 * time.Millisecond)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if id := a.Player().Current().ID; id != 3 {
		t.Errorf("swiped left: playing %d", id)
	}
}

// Played from the menu or the keys, the next song's cover slides in from
// the right and the one before's from the left, each coming to rest in its
// place.
func TestCoverSlidesWithTheSong(t *testing.T) {
	a, tt, r := coverView(t)
	tt.SetPreferences(ui.Preferences{})
	for _, step := range []struct {
		name string
		play func()
		id   int64
		dir  float32
	}{{"next", a.Player().Next, 3, 1}, {"prev", a.Player().Prev, 2, -1}} {
		step.play()
		tt.Frame()
		tt.Advance(60 * time.Millisecond)
		tt.Frame()
		if id := a.Player().Current().ID; id != step.id {
			t.Fatalf("%s: playing %d", step.name, id)
		}
		if r2, _ := tt.Find("专辑封面"); (r2.X-r.X)*step.dir < r.W*0.1 {
			t.Errorf("%s: the cover comes in at %v, from %v", step.name, r2.X, r.X)
		}
		tt.Advance(time.Second)
		tt.Frame()
		if r2, _ := tt.Find("专辑封面"); r2.X < r.X-2 || r2.X > r.X+2 {
			t.Errorf("%s: the cover rests at %v, not %v", step.name, r2, r)
		}
	}
}

func coverView(t *testing.T) (*View, *ui.Tester, ui.Rect) {
	t.Helper()
	v := newTestView(t)
	v.HasTrackCover = func(*netease.Track) bool { return false }
	v.Player().Cue([]netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}, {ID: 3, Name: "三"}})
	v.Player().Next()
	v.Player().SetLyrics(nil)
	tt := ui.NewTesterWith(func(c *ui.Context) {
		ui.Column(c).Fill().Center().Children(func() { v.NPCover(c, v.Player().Current(), nil, 280) })
	}, 860, 560, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	tt.Advance(500 * time.Millisecond)
	r, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	return v, tt, r
}
