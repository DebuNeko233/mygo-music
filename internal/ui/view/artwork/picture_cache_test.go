/*
验证背景缓存的按需读取、字节预算、替换保护和失效请求隔离。
*/

package artwork

import (
	"fmt"
	"image"
	"math"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
)

func pictureCacheFixture(t *testing.T, limit int, load func(string) *Picture) (*PictureCache, chan func(), *atomic.Int64) {
	t.Helper()
	posts := make(chan func(), 4096)
	done := make(chan struct{})
	var calls atomic.Int64
	post := func(fn func()) {
		select {
		case <-done:
			return
		case posts <- fn:
		}
	}
	c := NewPictureCache(PictureCacheOptions{Limit: limit, Post: post, PostResource: func(fn, _ func()) { post(fn) }, Load: func(path string) *Picture { calls.Add(1); return load(path) }})
	t.Cleanup(func() { c.Close(); close(done) })
	return c, posts, &calls
}
func cachedTestPicture(side int) *Picture {
	return &Picture{Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, side, side))), W: float32(side), H: float32(side)}
}

// A thousand configured files schedule only the current and actual next-song
// backgrounds; reaching a prefetched target reuses its bitmap.
func TestPictureCacheLoadsOnlyDemandedPaths(t *testing.T) {
	c, posts, calls := pictureCacheFixture(t, 1024, func(string) *Picture { return cachedTestPicture(4) })
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = fmt.Sprint(i)
	}
	s := &PictureSlot{Visible: true}
	c.Select(s, paths, 0, 1, true)
	(<-posts)()
	first := c.Select(s, paths, 0, 1, true)
	(<-posts)()
	if calls.Load() != 2 || len(c.entries) != 2 || first == nil {
		t.Fatalf("loaded %d of 1000 paths", calls.Load())
	}
	next := c.entries[paths[1]].picture
	if got := c.Select(s, paths, 1, 0, false); got != next || calls.Load() != 2 {
		t.Fatal("prefetched target was decoded again")
	}
}

// Replacing many images stays within both byte and metadata budgets. The old
// shown image remains usable throughout the new image's asynchronous load.
func TestPictureCacheBudgetAcrossThousandSelections(t *testing.T) {
	c, posts, _ := pictureCacheFixture(t, 256, func(string) *Picture { return cachedTestPicture(4) })
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = fmt.Sprint(i)
	}
	s := &PictureSlot{Visible: true}
	var before *Picture
	for i := range paths {
		if got := c.Select(s, paths, int64(i), 0, false); got != before {
			t.Fatal("replacement discarded old background before loading")
		}
		(<-posts)()
		before = c.Select(s, paths, int64(i), 0, false)
		if before == nil || c.bytes > c.limit || len(c.entries) > pictureCacheEntries {
			t.Fatalf("selection %d: bytes=%d entries=%d", i, c.bytes, len(c.entries))
		}
	}
}

// A larger-than-estimated prefetch cannot evict a hidden page's selected
// image and cause the two requests to decode and evict each other forever.
func TestPicturePrefetchNeverDisplacesSelectedImages(t *testing.T) {
	c, posts, calls := pictureCacheFixture(t, 500, func(path string) *Picture {
		if path == "large" {
			return cachedTestPicture(8)
		}
		return cachedTestPicture(4)
	})
	h, p := &PictureSlot{Visible: true}, &PictureSlot{}
	c.Select(h, []string{"home", "large"}, 0, 0, false)
	c.Select(p, []string{"playing"}, 0, 0, false)
	(<-posts)()
	(<-posts)()
	playing := c.Select(p, []string{"playing"}, 0, 0, false)
	c.Select(h, []string{"home", "large"}, 0, 1, true)
	(<-posts)()
	for range 10 {
		c.Select(h, []string{"home", "large"}, 0, 1, true)
		if c.Select(p, []string{"playing"}, 0, 0, false) != playing {
			t.Fatal("prefetch evicted selected playing background")
		}
	}
	if calls.Load() != 3 || len(c.jobs) != 0 || c.bytes > c.limit {
		t.Fatal("rejected prefetch kept retrying or exceeded budget")
	}
}

// A hidden image that cannot fit beside a visible one stays deferred. Making
// it visible admits it without creating a hidden/visible reload cycle.
func TestPictureCachePrioritizesVisibleSurface(t *testing.T) {
	c, posts, calls := pictureCacheFixture(t, 500, func(string) *Picture { return cachedTestPicture(9) })
	h, p := &PictureSlot{Visible: true}, &PictureSlot{}
	c.Select(h, []string{"home"}, 0, 0, false)
	(<-posts)()
	c.Select(p, []string{"playing"}, 0, 0, false)
	(<-posts)()
	if c.bytes > c.limit || c.Select(p, []string{"playing"}, 0, 0, false) != nil {
		t.Fatal("hidden image displaced visible background")
	}
	h.Visible, p.Visible = false, true
	c.Select(p, []string{"playing"}, 0, 0, false)
	(<-posts)()
	for range 10 {
		c.Select(h, []string{"home"}, 0, 0, false)
		c.Select(p, []string{"playing"}, 0, 0, false)
	}
	if calls.Load() != 3 || len(c.jobs) != 0 || c.bytes > c.limit {
		t.Fatal("visibility switch caused repeated decodes")
	}
}

// An obsolete blocked load cannot overwrite a newer selected picture.
func TestPictureCacheRejectsLateReplacement(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	c, posts, _ := pictureCacheFixture(t, 1024, func(path string) *Picture {
		if path == "slow" {
			close(started)
			<-release
		}
		return cachedTestPicture(4)
	})
	s := &PictureSlot{Visible: true}
	paths := []string{"old", "slow", "latest"}
	c.Select(s, paths, 0, 0, false)
	(<-posts)()
	old := c.Select(s, paths, 0, 0, false)
	c.Select(s, paths, 1, 0, false)
	<-started
	if c.Select(s, paths, 2, 0, false) != old {
		t.Fatal("rapid selection cleared the old picture")
	}
	(<-posts)()
	latest := c.Select(s, paths, 2, 0, false)
	close(release)
	(<-posts)()
	if latest == nil || latest == old || c.Select(s, paths, 2, 0, false) != latest || c.entries["slow"] != nil {
		t.Fatal("stale image was retained or replaced the latest image")
	}
}

// Invalid paths are visited at most once per selection, even after old
// failure metadata is trimmed; all-invalid configurations settle without jobs.
func TestPictureCacheAllInvalidStopsAfterOnePass(t *testing.T) {
	c, posts, calls := pictureCacheFixture(t, 1024, func(string) *Picture { return nil })
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = fmt.Sprint(i)
	}
	s := &PictureSlot{Visible: true}
	c.Select(s, paths, 0, 0, false)
	for range paths {
		(<-posts)()
	}
	for range 5 {
		if c.Select(s, paths, 0, 0, false) != nil {
			t.Fatal("invalid file produced a picture")
		}
	}
	if calls.Load() != 1000 || len(c.jobs) != 0 || len(c.entries) > pictureCacheEntries {
		t.Fatal("invalid paths kept loading or growing metadata")
	}
}

// Negative local-song identities and the minimum int64 keep a stable,
// nonnegative slot in the configured order.
func TestPictureIndexHandlesLocalKeys(t *testing.T) {
	for _, tc := range []struct {
		key     int64
		n, want int
	}{{-5, 3, 2}, {5, 3, 2}, {math.MinInt64, 3, 2}, {0, 0, 0}} {
		if got := pictureIndex(tc.key, tc.n); got != tc.want {
			t.Fatalf("key=%d index=%d want=%d", tc.key, got, tc.want)
		}
	}
}

// Changing only the next-song identity while prefetch is budget-blocked
// cannot grow a map of empty metadata entries without any completion to trim it.
func TestPictureBlockedPrefetchMetadataIsBounded(t *testing.T) {
	c, posts, calls := pictureCacheFixture(t, 85, func(string) *Picture { return cachedTestPicture(4) })
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = fmt.Sprint(i)
	}
	s := &PictureSlot{Visible: true}
	c.Select(s, paths, 0, 0, false)
	(<-posts)()
	for i := 1; i < 1000; i++ {
		c.Select(s, paths, 0, int64(i), true)
	}
	if calls.Load() != 1 || len(c.entries) > pictureCacheEntries {
		t.Fatal("blocked lookahead grew metadata or decoded extra files")
	}
}

// Results retired by a configuration edit return their slots and immediately
// let the latest target start, even when no new view frame is scheduled.
func TestPictureRetiredResultsUnblockLatestTarget(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	c, posts, calls := pictureCacheFixture(t, 1024, func(path string) *Picture {
		if path != "new" {
			started <- struct{}{}
			<-release
		}
		return cachedTestPicture(4)
	})
	h, p := &PictureSlot{Visible: true}, &PictureSlot{}
	c.Select(h, []string{"old-home"}, 0, 0, false)
	c.Select(p, []string{"old-playing"}, 0, 0, false)
	<-started
	<-started
	c.Select(h, []string{"new"}, 0, 0, false)
	c.Select(p, nil, 0, 0, false)
	c.Retain([]string{"new"})
	close(release)
	for i := 0; i < 3; i++ {
		(<-posts)()
	}
	if c.Select(h, []string{"new"}, 0, 0, false) == nil || calls.Load() != 3 || len(c.jobs) != 0 {
		t.Fatal("retired results stranded or restarted background requests")
	}
}

// When two large images cannot coexist during a page transition, the cold
// surface keeps the last background until its selected image can be admitted.
func TestPictureBudgetTransitionKeepsVisibleFallback(t *testing.T) {
	c, posts, _ := pictureCacheFixture(t, 500, func(string) *Picture { return cachedTestPicture(9) })
	h, p := &PictureSlot{Visible: true}, &PictureSlot{Visible: true}
	c.Select(h, []string{"home"}, 0, 0, false)
	(<-posts)()
	old := c.Select(h, []string{"home"}, 0, 0, false)
	if c.Select(p, []string{"playing"}, 0, 0, false) != old {
		t.Fatal("cold page cleared the visible background")
	}
	(<-posts)()
	if c.Select(p, []string{"playing"}, 0, 0, false) != old || c.bytes > c.limit {
		t.Fatal("transition lost fallback or exceeded budget")
	}
	h.Visible = false
	if c.Select(p, []string{"playing"}, 0, 0, false) != old {
		t.Fatal("admission wait lost the last background")
	}
	(<-posts)()
	now := c.Select(p, []string{"playing"}, 0, 0, false)
	if now == nil || now == old || c.bytes > c.limit {
		t.Fatal("selected background did not replace the fallback")
	}
}
