package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Prefetch warms the same 600px backdrop entry the next song uses, even
// after shuffling or wrapping, and repeated builds share its request.
func TestPrefetchesNextCover(t *testing.T) {
	for _, mode := range []string{"next", "shuffle", "wrap", "single"} {
		t.Run(mode, func(t *testing.T) {
			offline := netease.Offline()
			netease.SetOffline(true) // player metadata stays out of this cache test
			t.Cleanup(func() { netease.SetOffline(offline) })
			asked := make(chan string, 8)
			posts := make(chan func(), 8)
			old := loadArt
			t.Cleanup(func() { loadArt = old })
			loadArt = func(url string) (*ui.Bitmap, image.Image, int, error) {
				asked <- url
				img := image.NewRGBA(image.Rect(0, 0, 8, 8))
				return ui.NewBitmap(img), img, 16, nil
			}
			a := newTestApp()
			a.settings = defaultSettings()
			a.settings.HotComments = false
			a.art = newArtCache(func(fn func()) { posts <- fn })
			q := []netease.Track{
				{ID: 1, Album: netease.Album{PicURL: "test://one"}},
				{ID: 2, Album: netease.Album{PicURL: "test://two"}},
				{ID: 3, Album: netease.Album{PicURL: "test://three"}},
			}
			if mode == "single" {
				q = q[:1]
			}
			a.pl.Cue(q)
			if mode == "wrap" || mode == "shuffle" {
				a.pl.Next()
				a.pl.Next()
			}
			if mode == "shuffle" {
				a.pl.ToggleShuffle()
			}
			order, at := a.pl.PlayOrder()
			want := q[order[(at+1)%len(order)]]
			url := netease.Sized(want.Album.PicURL, 600)
			a.pl.SetLyrics(nil)
			wanted := map[string]bool{url: true, netease.Sized(a.pl.Current().Album.PicURL, 600): true}
			tt := ui.NewTesterWith(a.nowPlayingView, 640, 440, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			tt.Frame()
			for range len(wanted) {
				if got := wait(t, asked, "cover"); !wanted[got] {
					t.Fatalf("requested unexpected cover %q", got)
				}
				wait(t, posts, "decoded cover")()
			}
			tt.Frame()
			entry := a.art.art[url+"#backdrop"]
			if entry == nil || entry.bmp == nil || entry.backdrop == nil {
				t.Fatal("did not warm the large cover with its backdrop")
			}
			if a.art.get(url, true) != entry || len(a.art.busy) != 0 || len(asked) != 0 {
				t.Fatal("showing the prefetched cover loaded it again")
			}
			a.pl.Next()
			if a.pl.Current().ID != want.ID {
				t.Fatal("prefetched a song other than the one Next plays")
			}
		})
	}
}

// Speculative covers are not requested offline, for an empty queue, or
// past the end of a radio, whose next track must come from its feed.
func TestCoverPrefetchSkipsUnavailableNext(t *testing.T) {
	a := newTestApp()
	a.prefetchCover()
	a.pl.Cue([]netease.Track{{ID: 1, Album: netease.Album{PicURL: "test://one"}}})
	a.network.offline = true
	a.prefetchCover()
	a.network.offline = false
	a.pl.AttachRadio("radio", func(func([]netease.Track)) {})
	a.prefetchCover()
	if len(a.art.busy) != 0 {
		t.Fatal("started a cover request without an available next song")
	}
}

// paintedCoverApp supplies ready images without network or disk access.
func paintedCoverApp(t *testing.T, n int, reduced bool) (*app, *ui.Tester, []*artwork) {
	t.Helper()
	offline := netease.Offline()
	netease.SetOffline(true)
	t.Cleanup(func() { netease.SetOffline(offline) })
	a := newTestApp()
	a.network.offline = true
	a.settings = defaultSettings()
	a.settings.HotComments = false
	q := make([]netease.Track, n)
	arts := make([]*artwork, n)
	for i := range q {
		q[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprint(i + 1), Duration: 200000,
			Album: netease.Album{PicURL: fmt.Sprintf("test://cover/%d", i)}}
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := range 64 {
			for x := range 64 {
				img.SetRGBA(x, y, color.RGBA{R: uint8(60 + i*30), G: uint8(180 - i*20), B: uint8(50 + i*25), A: 255})
			}
		}
		arts[i] = &artwork{bmp: ui.NewBitmap(img), tint: ui.RGB(uint8(60+i*30), uint8(180-i*20), uint8(50+i*25))}
		for _, size := range []int{96, 120, 240, 600} {
			url := netease.Sized(q[i].Album.PicURL, size)
			a.art.add(url, arts[i])
			a.art.add(url+"#backdrop", arts[i])
		}
	}
	a.pl.Cue(q)
	a.pl.Hold(0)
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTesterWith(a.view, 1000, 700,
		ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}, Dark: true})
	tt.Advance(2 * time.Second)
	return a, tt, arts
}

// Next after a shuffle enters from the right; two-song Next and Prev
// keep their direction even though their queue indices are identical.
func TestCoverDirectionAfterShuffleAndWrap(t *testing.T) {
	for _, n := range []int{2, 5} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			a, tt, _ := paintedCoverApp(t, n, false)
			a.pl.Next()
			a.pl.SetLyrics(nil)
			tt.Frame()
			tt.Advance(time.Second)
			a.pl.ToggleShuffle()
			tt.Frame()
			for _, step := range []struct {
				play func()
				dir  float32
			}{{a.pl.Next, 1}, {a.pl.Next, 1}, {a.pl.Prev, -1}} {
				step.play()
				a.pl.SetLyrics(nil)
				tt.Frame()
				if a.npCoverState.direction != step.dir {
					t.Fatalf("direction %v, want %v", a.npCoverState.direction, step.dir)
				}
				tt.Advance(time.Second)
			}
		})
	}
}

// Replacing a queue around the same song invalidates its old position;
// a later selection must compare positions in the new order.
func TestCoverDirectionAfterQueueReplacement(t *testing.T) {
	a, tt, _ := paintedCoverApp(t, 5, false)
	a.pl.Next()
	a.pl.SetLyrics(nil)
	tt.Frame()
	tt.Advance(time.Second)
	q := a.pl.Queue()
	a.pl.PlayQueue([]netease.Track{q[0], q[2], q[3], q[1], q[4]}, 3)
	a.pl.SetLyrics(nil)
	tt.Frame()
	a.pl.Jump(2)
	a.pl.SetLyrics(nil)
	tt.Frame()
	if a.npCoverState.direction != -1 {
		t.Fatal("selected an earlier song in the new queue, but entered from the right")
	}
}

// Looking up the outgoing song by ID chose the first A in A/B/C/A, even
// when the last A was playing. Selection must use its actual queue position.
func TestCoverDirectionWithDuplicateSongs(t *testing.T) {
	for _, pick := range []struct {
		from, to int
		want     float32
	}{{3, 2, -1}, {3, 1, -1}, {0, 2, 1}} {
		t.Run(fmt.Sprintf("%d-to-%d", pick.from, pick.to), func(t *testing.T) {
			a, tt, _ := paintedCoverApp(t, 3, false)
			a.pl.SuspendCache()
			original := a.pl.Queue()
			q := append([]netease.Track(nil), original...)
			q = append(q, original[0])
			a.pl.PlayQueue(q, pick.from)
			a.pl.SetLyrics(nil)
			tt.Frame()
			tt.Advance(time.Second)
			rest, ok := tt.Find("专辑封面")
			if !ok {
				t.Fatal("no resting cover")
			}
			a.pl.Jump(pick.to)
			a.pl.SetLyrics(nil)
			tt.Frame()
			if a.npCoverState.direction != pick.want {
				t.Fatalf("direction %v, want %v", a.npCoverState.direction, pick.want)
			}
			tt.Advance(80 * time.Millisecond)
			moving, ok := tt.Find("专辑封面")
			if !ok || (moving.X-rest.X)*pick.want <= 1 {
				t.Fatalf("cover did not enter from the selected direction: %v -> %v", rest, moving)
			}
		})
	}
}

// A different queue has no shared positions with the old one, even when
// it contains the old song ID at a later slot. Use a neutral entrance.
func TestCoverDirectionDoesNotReuseOldQueuePositions(t *testing.T) {
	a, tt, _ := paintedCoverApp(t, 3, false)
	a.pl.SuspendCache()
	a.pl.Next()
	a.pl.SetLyrics(nil)
	tt.Frame()
	tt.Advance(time.Second)
	q := a.pl.Queue()
	a.pl.PlayQueue([]netease.Track{q[0], q[2], q[1]}, 1)
	a.pl.SetLyrics(nil)
	tt.Frame()
	if a.npCoverState.direction != 1 {
		t.Fatal("replacement inferred a backward move from the old song ID")
	}
}

// A burst applies every click and gives each image its own entrance,
// keeping only its immediate predecessor and releasing it after the slide.
func TestRapidCoverChangesRestartTheEntrance(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			a, tt, arts := paintedCoverApp(t, 5, reduced)
			for i := 1; i < len(arts); i++ {
				if err := tt.Click("下一首"); err != nil {
					t.Fatal(err)
				}
				a.pl.SetLyrics(nil)
				tt.Frame()
				cs := &a.npCoverState
				if a.pl.Current().ID != int64(i+1) || cs.art != arts[i] || cs.previous != arts[i-1] || cs.slide != i || cs.previousSlide != i-1 {
					t.Fatalf("step %d: playing %d, shown %d, slide %d; entrance was not restarted", i, a.pl.Current().ID, cs.shown, cs.slide)
				}
				tt.Advance(80 * time.Millisecond)
				if dir := os.Getenv("NM_FRAMES"); dir != "" {
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("cover-burst-%v-%d.png", reduced, i)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			tt.Advance(time.Second)
			if a.npCoverState.previous != nil || a.npCoverState.switching {
				t.Fatal("kept the outgoing artwork after the slide")
			}
		})
	}
}

// Same-direction and alternating bursts used to replace a picture in the
// tail of an old animation. Every ready target must now move in anew.
func TestRapidCoverChangesAlwaysMove(t *testing.T) {
	for _, gap := range []time.Duration{50 * time.Millisecond, 80 * time.Millisecond, 150 * time.Millisecond} {
		for _, pattern := range []string{"next", "previous", "alternate"} {
			t.Run(fmt.Sprintf("%s/%v", pattern, gap), func(t *testing.T) {
				a, tt, _ := paintedCoverApp(t, 8, false)
				if pattern == "previous" {
					a.pl.Jump(7)
					a.pl.SetLyrics(nil)
					tt.Frame()
					tt.Advance(time.Second)
				}
				rest, ok := tt.Find("专辑封面")
				if !ok {
					t.Fatal("no resting cover")
				}
				for i := range 4 {
					label, dir := "下一首", float32(1)
					if pattern == "previous" || pattern == "alternate" && i%2 != 0 {
						label, dir = "上一首", -1
					}
					before := a.npCoverState.slide
					previous := a.npCoverState.art
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
					a.pl.SetLyrics(nil)
					tt.Frame()
					cs := &a.npCoverState
					if cs.slide != before+1 || cs.previousSlide != before || cs.previous != previous {
						t.Fatalf("step %d did not hand the current layer to a fresh entrance", i)
					}
					// Observe motion, rather than only checking a generation counter.
					tt.Advance(gap / 2)
					moving, ok := tt.Find("专辑封面")
					if !ok || (moving.X-rest.X)*dir < rest.W*0.05 {
						t.Fatalf("step %d: cover at %+v did not enter from direction %v", i, moving, dir)
					}
					tt.Advance(gap - gap/2)
				}
				tt.Advance(time.Second)
				settled, ok := tt.Find("专辑封面")
				if !ok || abs32(settled.X-rest.X) > 2 || a.npCoverState.switching || a.npCoverState.previous != nil {
					t.Fatal("the latest cover did not settle and release its predecessor")
				}
			})
		}
	}
}

// Returning after a track changed on another pane must still cover the
// first frame while the new image is transparent and has no live predecessor.
func TestCoverSwitchAfterHiddenPane(t *testing.T) {
	a, tt, _ := paintedCoverApp(t, 3, false)
	r, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	x, y := int(r.X+r.W/2), int(r.Y+r.H/2)
	covered := tt.Image().RGBAAt(x, y)
	a.npPane = paneStage
	tt.Frame()
	a.pl.Next()
	a.pl.SetLyrics(nil)
	tt.Frame()
	a.npPane = paneLyrics
	tt.Frame()
	if got := tt.Image().RGBAAt(x, y); got != covered {
		t.Fatalf("first frame showed %v, want the held cover %v", got, covered)
	}
	tt.Advance(time.Second)
	if a.npCoverState.switching || a.npCoverState.shown != 2 {
		t.Fatal("the new cover did not settle after returning")
	}
}

// A missing new image leaves the last cover visible until the requested
// one arrives; a stale completion cannot replace the newer selection.
func TestCoverWaitsForTheLatestArtwork(t *testing.T) {
	a, tt, arts := paintedCoverApp(t, 4, false)
	for _, tr := range a.pl.Queue()[1:] {
		a.art.invalidate(netease.Sized(tr.Album.PicURL, 600))
		// Hold the requests without starting workers.
		a.art.busy[netease.Sized(tr.Album.PicURL, 600)+"#backdrop"] = 1
	}
	a.pl.Next()
	a.pl.SetLyrics(nil)
	tt.Frame()
	a.pl.Next()
	a.pl.SetLyrics(nil)
	tt.Frame()
	if a.npCoverState.art != arts[0] {
		t.Fatal("replaced the last image with a blank card")
	}
	q := a.pl.Queue()
	a.art.add(netease.Sized(q[1].Album.PicURL, 600)+"#backdrop", arts[1])
	tt.Frame()
	if a.npCoverState.art != arts[0] {
		t.Fatal("showed an obsolete request's cover")
	}
	a.art.add(netease.Sized(q[2].Album.PicURL, 600)+"#backdrop", arts[2])
	tt.Frame()
	if a.npCoverState.art != arts[2] || a.npCoverState.previous != arts[0] {
		t.Fatal("did not switch from the held image to the latest one")
	}
}
