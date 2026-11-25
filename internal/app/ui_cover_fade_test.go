package app

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
)

// Stage and wall covers must hold the old picture while loading, blend to
// the latest selection and release obsolete pictures after rapid skips.
func TestPaneCoverFollowsLatestSelection(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		pane        int
	}{
		{"stage", "律动封面", playbackview.PaneStage},
		{"wall", "选歌封面", playbackview.PaneWall},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, tt, arts := paintedCoverApp(t, 5, false)
			a.playbackState.Pane = tc.pane
			tt.Frame()
			tt.Advance(time.Second)
			state := &a.StageCover
			if tc.pane == playbackview.PaneWall {
				state = &a.WallCover
			}
			center := func() color.RGBA {
				t.Helper()
				r, ok := tt.Find(tc.label)
				if !ok {
					t.Fatal("playing cover is missing")
				}
				img := tt.Image()
				scale := float32(img.Bounds().Dx()) / 640
				return img.RGBAAt(int((r.X+r.W/2)*scale), int((r.Y+r.H/2)*scale))
			}
			save := func(name string) {
				t.Helper()
				if dir := os.Getenv("NM_FRAMES"); dir != "" {
					f, err := os.Create(filepath.Join(dir, tc.name+"-cover-"+name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("save frame: %v, %v", err, closeErr)
					}
				}
			}
			old := center()
			save("before")
			q := a.pl.Queue()
			key := func(i int) string {
				return netease.Sized(q[i].Album.PicURL, nowPlayingCoverPixels) + "#backdrop"
			}
			for _, i := range []int{1, 2} {
				a.art.Invalidate(netease.Sized(q[i].Album.PicURL, nowPlayingCoverPixels), false)
				holdArtwork(a.art, key(i))
			}
			if err := tt.Click("下一首"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.pl.Current().Key() != q[1].Key() || state.Current != arts[0] || center() != old {
				t.Fatal("Next cleared the visible cover while its replacement was loading")
			}
			a.pl.Jump(2)
			tt.Frame()
			a.art.Put(key(1), arts[1])
			tt.Frame()
			if state.Current != arts[0] || center() != old {
				t.Fatal("an obsolete completion replaced the held cover")
			}
			a.art.Put(key(2), arts[2])
			tt.Frame()
			if state.Current != arts[2] || state.Previous != arts[0] || center() != old {
				t.Fatal("the latest ready cover did not start over the old picture")
			}
			tt.Advance(playbackview.CoverFadeDuration / 2)
			mixed := center()
			if mixed.R <= old.R || mixed.R >= 120 || mixed.G <= 140 || mixed.G >= old.G {
				t.Fatalf("both covers should cover the center during the blend: %v", mixed)
			}
			save("blend")
			a.pl.Jump(3)
			tt.Frame()
			if state.Current != arts[2] || state.Previous != arts[0] {
				t.Fatal("a rapid skip interrupted the visible blend")
			}
			tt.Advance(2 * playbackview.CoverFadeDuration)
			if state.Current != arts[3] || state.Previous != nil || !state.ChangedAt.IsZero() {
				t.Fatal("the latest selection did not settle and release its predecessor")
			}
			save("after")
			a.art.Put(key(4), &artcache.Image{Failed: true})
			a.pl.Next()
			tt.Frame()
			tt.Advance(2 * playbackview.CoverFadeDuration)
			if state.Current != nil || state.Previous != nil {
				t.Fatal("a failed image kept the previous song's cover indefinitely")
			}
			save("placeholder")
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			a.playbackState.Pane = playbackview.PaneLyrics
			tt.Frame()
			a.pl.Jump(0)
			a.playbackState.Pane = tc.pane
			tt.Frame()
			if state.Current != arts[0] || state.Previous != nil || center() != old {
				t.Fatal("returning with reduced motion should show the ready cover immediately")
			}
		})
	}
}
