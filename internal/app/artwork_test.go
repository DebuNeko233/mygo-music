package app

import (
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"
)

// Slow UI callbacks must keep their slots: scrolling past more covers
// cannot leave an unbounded number of decoded results waiting for the UI.
func TestArtJobsBoundPendingResults(t *testing.T) {
	posts := make(chan func(), 64)
	c := newArtCache(func(fn func()) { posts <- fn })
	t.Cleanup(c.close)
	old := loadArt
	t.Cleanup(func() { loadArt = old })
	loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
		img := image.NewRGBA(image.Rect(0, 0, 48, 48))
		return ui.NewBitmap(img), img, 512, nil
	}
	for i := range 60 {
		c.get(fmt.Sprint("cover-", i), false)
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
		c.invalidate(fmt.Sprint("cover-", i))
	}
	for _, post := range ready {
		post()
	}
	if len(c.art) != 0 {
		t.Fatal("invalidated results entered the cache")
	}
	c.get("visible-now", true)
	wait(t, posts, "new visible cover")()
	if a := c.get("visible-now", true); a == nil || a.backdrop == nil {
		t.Fatal("completion did not free a slot for the visible cover")
	}
}

// When one result consumes most of the byte budget, the next completion
// waits without posting; invalidation releases that budget too.
func TestArtResultsRespectByteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		posts := make(chan func(), artJobs)
		c := newArtCache(func(fn func()) { posts <- fn })
		defer c.close()
		old := loadArt
		defer func() { loadArt = old }()
		loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			return ui.NewBitmap(img), img, artResultLimit * 3 / 4, nil
		}
		c.get("first", false)
		first := <-posts
		c.get("second", false)
		synctest.Wait()
		if len(posts) != 0 {
			t.Fatal("posted a second result over the byte budget")
		}
		c.invalidate("first")
		c.invalidate("second")
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
	bmp, img, err := decodeArt(b)
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
		c.add(key, &artwork{used: now, size: 30})
		now = now.Add(time.Minute)
	}
	c.get("first", false)
	c.add("fourth", &artwork{used: now, size: 30})
	if _, ok := c.art["second"]; ok {
		t.Error("kept the cover asked for least recently over the limit")
	}
	for _, key := range []string{"first", "fourth"} {
		if _, ok := c.art[key]; !ok {
			t.Errorf("dropped %q, asked for last", key)
		}
	}
}

// Covers stay under the limit however long since they showed. They went
// after 5 minutes not asked for: going back to a page after a while on
// others, or to the window after a while away, loaded them again.
func TestArtCacheKeepsCoversUnderLimit(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now = func() time.Time { return now }
	page := &artwork{used: now, size: 100 << 10}
	c.add("page", page)
	for range 60 {
		now = now.Add(time.Minute)
		c.get("other", false)
	}
	if c.get("page", false) != page {
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
	held := &artwork{used: now, size: 30}
	c.add("now playing", held)
	for _, key := range []string{"other", "another"} {
		now = now.Add(time.Minute)
		c.add(key, &artwork{used: now, size: 30})
	}
	if _, ok := c.art["now playing"]; ok {
		t.Fatal("kept a cover over the limit")
	}
	if got := c.get("now playing", false); got != held {
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
	old := loadArt
	defer func() { loadArt = old }()
	loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
		bmp, img, err := decodeArt(b)
		return bmp, img, len(b), err
	}
	posted := make(chan func(), 1)
	c := newArtCache(func(f func()) { posted <- f })
	c.now, c.limit = func() time.Time { return time.Unix(1e9, 0) }, 1<<30
	c.get("cover", false)
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
		defer c.close()
		old := loadArt
		defer func() { loadArt = old }()
		loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			return ui.NewBitmap(img), img, 16, nil
		}
		c.get("cover", false)
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
			defer c.close()
			now := time.Unix(100, 0)
			c.now = func() time.Time { return now }
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			old, newCover := ui.NewBitmap(img), ui.NewBitmap(img)
			url := "https://fixture.invalid/side?param=80y80"
			size := pixelBytes(old)
			c.add(url, &artwork{bmp: old, loaded: now, used: now, size: size})
			now = now.Add(24*time.Hour + time.Second)
			original := loadArt
			defer func() { loadArt = original }()
			loadArt = func(string) (*ui.Bitmap, image.Image, int, error) {
				if scenario == "failure" {
					return nil, nil, 0, errors.New("offline")
				}
				return newCover, img, 0, nil
			}
			if scenario == "full" {
				for range cap(c.sem) {
					c.sem <- struct{}{}
				}
				if got := bitmap(c.get(url, false)); got != old {
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
				if got := bitmap(c.get(url, false)); got != old {
					t.Fatal("old cover disappeared before replacement")
				}
			}
			callback := wait(t, posts, "refresh completion")
			callback()
			want := newCover
			if scenario == "failure" {
				want = old
			}
			if got := bitmap(c.get(url, false)); got != want {
				t.Fatal("wrong cover after refresh")
			}
			if len(c.busy) != 0 || c.size != size {
				t.Fatal("refresh retried immediately or miscounted the cover")
			}
		})
	}
}
