package artwork

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Slow UI callbacks must keep their slots: scrolling past more covers
// cannot leave an unbounded number of decoded results waiting for the UI.
func TestArtJobsBoundPendingResults(t *testing.T) {
	posts := make(chan func(), 64)
	c := newArtCache(func(fn func()) { posts <- fn })
	t.Cleanup(c.Close)

	c.load = func(string) (*ui.Bitmap, image.Image, int, error) {
		img := image.NewRGBA(image.Rect(0, 0, 48, 48))
		return ui.NewBitmap(img), img, 512, nil
	}
	for i := range 60 {
		c.Get(fmt.Sprint("cover-", i), false)
	}
	var ready []func()
	for range artJobs {
		ready = append(ready, wait(t, posts, "cover result"))
	}
	if len(c.busy) != artJobs {
		t.Fatalf("accepted %d jobs while the UI was busy, want %d", len(c.busy), artJobs)
	}
	// Invalidating a queued result must still release its slot and cannot
	// bring its pixels back after the invalidation.
	for i := range artJobs {
		c.Invalidate(fmt.Sprint("cover-", i), false)
	}
	for _, post := range ready {
		post()
	}
	if len(c.art) != 0 {
		t.Fatal("invalidated results entered the cache")
	}
	c.Get("visible-now", true)
	wait(t, posts, "new visible cover")()
	if a := c.Get("visible-now", true); a == nil || a.Backdrop == nil {
		t.Fatal("completion did not free a slot for the visible cover")
	}
}

// When one result consumes most of the byte budget, the next completion
// waits without posting; invalidation releases that budget too.
func TestArtResultsRespectByteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		posts := make(chan func(), artJobs)
		c := newArtCache(func(fn func()) { posts <- fn })
		defer c.Close()

		c.load = func(string) (*ui.Bitmap, image.Image, int, error) {
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			return ui.NewBitmap(img), img, artResultLimit * 3 / 4, nil
		}
		c.Get("first", false)
		first := <-posts
		c.Get("second", false)
		synctest.Wait()
		if len(posts) != 0 {
			t.Fatal("posted a second result over the byte budget")
		}
		c.Invalidate("first", false)
		c.Invalidate("second", false)
		first()
		(<-posts)()
	})
}

// An animated WebP cover, as some playlists have, shows its first frame,
// in the bitmap too, which decodes its data again once it drops its pixels.
func TestAnimatedWebPCover(t *testing.T) {
	b, err := os.ReadFile("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}
	bmp, img, _, err := DecodeSized(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != 24 || r.Dy() != 24 {
		t.Errorf("first frame is %v, want 24×24", r)
	}
	if w, h := bmp.Size(); w != 24 || h != 24 {
		t.Errorf("bitmap is %d×%d, want 24×24", w, h)
	}
}

// Once the covers take more than the limit, those asked for least
// recently go, so that a long session does not keep every cover it
// showed; one asked for again stays, however long ago it loaded.
func TestArtCacheDropsLeastRecentCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 100
	for _, key := range []string{"first", "second", "third"} {
		c.add(key, &Image{used: now, size: 30})
		now = now.Add(time.Minute)
	}
	c.Get("first", false)
	c.add("fourth", &Image{used: now, size: 30})
	if _, ok := c.art["second"]; ok {
		t.Error("kept the cover asked for least recently over the limit")
	}
	for _, key := range []string{"first", "fourth"} {
		if _, ok := c.art[key]; !ok {
			t.Errorf("dropped %q, asked for last", key)
		}
	}
}

// Offscreen completions must leave visible Image intact and stay within the budget.
func TestVisibleArtworkSurvivesOffscreenCompletions(t *testing.T) {
	c := newArtCache(func(func()) {})
	now := time.Unix(100, 0)
	c.now, c.limit = func() time.Time { return now }, 100
	visible := &Image{size: 35, used: now}
	c.add("visible", visible)
	c.Get("visible", false)
	now = now.Add(time.Second)
	c.add("offscreen", &Image{size: 35, used: now})
	now = now.Add(time.Second)
	if !c.add("new", &Image{size: 60, used: now}) || c.art["offscreen"] != nil || c.size != 95 {
		t.Fatalf("wanted offscreen eviction with 95 bytes in cache, got %d", c.size)
	}
	if c.add("oversized", &Image{size: 70, used: now}) || c.art["visible"] != visible || c.size != 95 {
		t.Fatal("admitting Image would displace the visible cover")
	}
	c.Get("oversized", false)
	if c.busy["oversized"] != 0 {
		t.Fatal("a rejected cover immediately restarted decoding")
	}
	c.BeginFrame()
	if !c.add("oversized", &Image{size: 70, used: now}) || c.size > c.limit {
		t.Fatal("an offscreen cover still prevented admission")
	}
}

// An eviction only slightly over budget should keep a cover the user just scrolled past.
func TestRecentlyVisibleCoverSurvivesBatchEviction(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 100
	bmp := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	c.add("old", &Image{size: 20, Bitmap: bmp})
	c.add("recent", &Image{size: 40, Bitmap: bmp})
	c.Get("recent", false)
	c.BeginFrame()
	c.add("arrived", &Image{size: 60})
	if c.art["recent"] == nil || c.art["old"] != nil || c.size != c.limit {
		t.Fatal("batch trimming evicted the recently visible cover despite room for it")
	}
	c.add("required", &Image{size: 70})
	if c.art["recent"] != nil || c.size > c.limit {
		t.Fatal("a recently visible cover exceeded the memory limit")
	}
}

// A previously decoded size fills in while the requested cover size loads.
func TestCoverUsesDecodedSizeWhileLoadingAnother(t *testing.T) {
	const source = "https://p1.music.126.net/cover.jpg"
	const different = "https://p1.music.126.net/another.jpg"
	c := newArtCache(func(func()) {})
	posts := make(chan func(), artJobs)
	c.post = func(fn func()) { posts <- fn }
	t.Cleanup(c.Close)
	requests := make(chan string, artJobs)

	large := image.NewRGBA(image.Rect(0, 0, 128, 128))
	c.load = func(url string) (*ui.Bitmap, image.Image, int, error) {
		requests <- url
		return ui.NewBitmap(large), large, 0, nil
	}
	small := &Image{family: source, Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 32, 32)))}
	c.add(netease.Sized(source, 32), small)
	url := netease.Sized(source, 128)
	var shown *Image
	view := ui.NewTester(func(ctx *ui.Context) {
		box := ui.Box(ctx).Size(40, 40)
		shown = c.GetInView(url, false, box)
	}, 100, 100)
	if shown != small || wait(t, requests, "requested cover size") != url {
		t.Fatal("the decoded cover did not fill in while the larger size loaded")
	}
	wait(t, posts, "larger cover")()
	view.Frame()
	if shown == nil || shown == small {
		t.Fatal("the decoded fallback was not replaced by the requested size")
	}
	c.add(url, &Image{Failed: true, retryable: true})
	view.Frame()
	if shown != small {
		t.Fatal("a temporary error removed the decoded fallback")
	}
	c.ImageChanged(url, netease.ImageRevoked)
	view.Frame()
	if shown == nil || shown.Bitmap != nil {
		t.Fatal("the smaller variant masked a revoked cover")
	}
	url = netease.Sized(different, 128)
	view.Frame()
	if shown != nil {
		t.Fatal("a different cover used this image as a fallback")
	}
	wait(t, posts, "unrelated cover")()
}

// A revocation hides the image, cancels old results and retains a manual-retry failure.
func TestRevokedCoverLeavesFailure(t *testing.T) {
	url := "https://fixture.invalid/cover"
	c := newArtCache(func(func()) {})
	for _, blur := range []bool{false, true} {
		key := artworkKey(url, blur)
		c.add(key, &Image{size: 8})
		c.busy[key] = 1
	}
	c.ImageChanged(url, netease.ImageRevoked)
	for _, blur := range []bool{false, true} {
		key := artworkKey(url, blur)
		if got := c.Get(url, blur); got == nil || !got.Failed || c.busy[key] != 0 {
			t.Fatalf("revoked variant %q did not retain its failure", key)
		}
	}
	c.RetryFailed(false)
	if c.size != 0 || len(c.art) != 2 {
		t.Fatal("automatic recovery cleared definitive image failures")
	}
	c.RetryFailed(true)
	if len(c.art) != 0 {
		t.Fatal("manual refresh could not retry the revoked variants")
	}
}

// A small cover must not keep the full pixels of a larger disk-cache variant.
func TestArtworkVariantDecodesToRequestedSize(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 600, 300))); err != nil {
		t.Fatal(err)
	}
	bmp, _, encoded, err := DecodeSized(buf.Bytes(), 80)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := bmp.Size(); w != 80 || h != 40 || encoded != 0 {
		t.Fatalf("small variant retained %d×%d pixels and %d encoded bytes", w, h, encoded)
	}
}

// Covers stay under the limit however long since they showed. They went
// after 5 minutes not asked for: going back to a page after a while on
// others, or to the window after a while away, loaded them again.
func TestArtCacheKeepsCoversUnderLimit(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now = func() time.Time { return now }
	page := &Image{used: now, size: 100 << 10}
	c.add("page", page)
	for range 60 {
		now = now.Add(time.Minute)
		c.Get("other", false)
	}
	if c.Get("page", false) != page {
		t.Error("dropped a cover not asked for in an hour, under the limit")
	}
}

// A cover dropped while something still holds it comes back as it was.
// It loaded again: Now Playing's cover went while other pages showed,
// though Now Playing still held it, and flashed in once it opened again.
func TestArtCacheKeepsHeldCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 60
	held := &Image{used: now, size: 30}
	c.add("now playing", held)
	for _, key := range []string{"other", "another"} {
		now = now.Add(time.Minute)
		c.add(key, &Image{used: now, size: 30})
	}
	if _, ok := c.art["now playing"]; ok {
		t.Fatal("kept a cover over the limit")
	}
	if got := c.Get("now playing", false); got != held {
		t.Error("loaded again a cover something still held")
	}
	runtime.KeepAlive(held)
}

// A cover's size counts the pixels its data decodes to, and the smaller
// copies it shows from, not only its data: the limit has to bound the
// memory they take as well. Its completion runs on the cache's dispatcher.
func TestArtCacheSizesCoversByTheirPixels(t *testing.T) {
	b, err := os.ReadFile("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}

	loadArt := func(string) (*ui.Bitmap, image.Image, int, error) {
		bmp, img, _, err := DecodeSized(b, 0)
		return bmp, img, len(b), err
	}
	posted := make(chan func(), 1)
	c := newArtCache(func(f func()) { posted <- f })
	c.load = loadArt
	c.now, c.limit = func() time.Time { return time.Unix(1e9, 0) }, 1<<30
	c.Get("cover", false)
	select {
	case finish := <-posted:
		finish()
	case <-time.After(5 * time.Second):
		t.Fatal("the cover never landed")
	}
	// 24×24 pixels, and the smaller copies, about a third again.
	if want := len(b) + 24*24*4*4/3; c.size != want {
		t.Errorf("counted %d bytes of the cover, want %d", c.size, want)
	}
}

// Releasing the decoded pixels waits for the cover cache's dispatcher rather
// than bypassing it on a timer goroutine, as releasing them beside a frame did.
func TestArtReleaseUsesTheDispatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		posts := make(chan func(), 2)
		c := newArtCache(func(fn func()) { posts <- fn })
		defer c.Close()

		c.load = func(string) (*ui.Bitmap, image.Image, int, error) {
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			return ui.NewBitmap(img), img, 16, nil
		}
		c.Get("cover", false)
		(<-posts)()
		time.Sleep(artRelease)
		synctest.Wait()
		select {
		case <-posts:
		default:
			t.Fatal("the timer released pixels off the dispatcher")
		}
	})
}

// Expiry used to show a placeholder while a visible cover was being refreshed.
func TestSidebarRefreshKeepsVisibleCover(t *testing.T) {
	for _, scenario := range []string{"replacement", "failure", "full"} {
		t.Run(scenario, func(t *testing.T) {
			posts := make(chan func(), 1)
			c := newArtCache(func(f func()) { posts <- f })
			defer c.Close()
			now := time.Unix(100, 0)
			c.now = func() time.Time { return now }
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			old, newCover := ui.NewBitmap(img), ui.NewBitmap(img)
			url := "https://fixture.invalid/side?param=80y80"
			size := pixelBytes(old)
			c.add(url, &Image{Bitmap: old, loaded: now, used: now, size: size})
			now = now.Add(24*time.Hour + time.Second)

			c.load = func(string) (*ui.Bitmap, image.Image, int, error) {
				if scenario == "failure" {
					return nil, nil, 0, errors.New("offline")
				}
				return newCover, img, 0, nil
			}
			if scenario == "full" {
				for range cap(c.sem) {
					c.sem <- struct{}{}
				}
				if got := bitmap(c.Get(url, false)); got != old {
					t.Fatal("full queue hid visible cover")
				}
				if len(c.busy) != 0 {
					t.Fatal("full queue accepted another request")
				}
				for range cap(c.sem) {
					<-c.sem
				}
			}
			for range 2 {
				if got := bitmap(c.Get(url, false)); got != old {
					t.Fatal("old cover disappeared before replacement")
				}
			}
			callback := wait(t, posts, "refresh completion")
			callback()
			want := newCover
			if scenario == "failure" {
				want = old
			}
			if got := bitmap(c.Get(url, false)); got != want {
				t.Fatal("wrong cover after refresh")
			}
			if len(c.busy) != 0 || c.size != size {
				t.Fatal("refresh retried immediately or miscounted the cover")
			}
		})
	}
}

// A disk replacement must keep the visible cover even when an older decode is already queued.
func TestArtworkReplacementKeepsVisibleCover(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprintf("decode=%t", succeeds), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				posts := make(chan func(), artJobs)
				requests := make(chan chan *ui.Bitmap, artJobs)
				stop := make(chan struct{})
				defer close(stop)
				c := newArtCache(func(fn func()) { posts <- fn })
				defer c.Close()
				now := time.Unix(1e9, 0)
				c.now = func() time.Time { return now }
				const url = "https://fixture.invalid/side?param=80y80"
				img := image.NewRGBA(image.Rect(0, 0, 8, 8))
				old, outdated, fresh := ui.NewBitmap(img), ui.NewBitmap(img), ui.NewBitmap(img)
				size := pixelBytes(old)
				c.add(url, &Image{Bitmap: old, loaded: now, used: now, size: size})

				c.load = func(string) (*ui.Bitmap, image.Image, int, error) {
					result := make(chan *ui.Bitmap, 1)
					requests <- result
					select {
					case bmp := <-result:
						if bmp != nil {
							return bmp, img, 0, nil
						}
					case <-stop:
					}
					return nil, nil, 0, errors.New("invalid replacement image")
				}
				get := func(want *ui.Bitmap) {
					t.Helper()
					if got := bitmap(c.Get(url, false)); got != want {
						t.Fatalf("visible bitmap = %p, want %p", got, want)
					}
				}

				// A notification must also refresh images loaded less than 24 hours ago.
				c.Invalidate(url, true)
				get(old)
				(<-requests) <- outdated
				oldPost := <-posts

				c.Invalidate(url, true)
				get(old)
				next := <-requests
				oldPost()
				get(old)
				if succeeds {
					next <- fresh
				} else {
					next <- nil
				}
				(<-posts)()
				if succeeds {
					get(fresh)
				} else {
					get(old)
				}
				synctest.Wait()
				if len(requests) != 0 || len(c.busy) != 0 || c.size != size {
					t.Fatal("replacement left queued work, retried immediately or miscounted the cache")
				}
			})
		})
	}
}

// Failed visible Image can be retried, but navigating away does not start
// downloads for every old failure ever seen.
func TestOfflineArtworkRetryTracksVisibleRows(t *testing.T) {
	c := newArtCache(func(func()) {})
	c.add("shown", &Image{Failed: true, retryable: true})
	c.add("hidden", &Image{Failed: true, retryable: true})
	c.BeginFrame()
	c.Get("shown", false)
	if !c.HasFailed(false) {
		t.Fatal("visible failure was forgotten")
	}
	c.RetryFailed(false)
	if c.art["shown"] != nil || c.art["hidden"] == nil {
		t.Fatal("retried hidden covers or retained visible failure")
	}
	c.BeginFrame()
	if c.HasFailed(false) {
		t.Fatal("offscreen failures keep recovery alive")
	}
}

// A stale cover load finishing after invalidation cannot undo a newer load.
func TestOfflineArtworkInvalidationRejectsOldLoad(t *testing.T) {

	posts := make(chan func(), 4)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	loadArt := func(string) (*ui.Bitmap, image.Image, int, error) {
		// Downloads are started in a controlled order below.
		n := calls.Add(1)
		entered <- struct{}{}
		if n == 1 {
			<-release
		}
		return nil, nil, 0, errors.New("bad cover")
	}
	c := newArtCache(func(f func()) { posts <- f })
	c.load = loadArt
	c.Get("cover", false)
	<-entered
	c.Invalidate("cover", false)
	c.Get("cover", false)
	<-entered
	refreshPost(t, posts)
	fresh := c.art["cover"]
	close(release)
	refreshPost(t, posts)
	if c.art["cover"] != fresh {
		t.Fatal("invalidated load replaced its successor")
	}
}
