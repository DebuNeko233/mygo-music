/*
验证列表封面保留的页面、数量和字节边界，以及压力与失效时的资源释放。
*/

package artwork

import (
	"fmt"
	"image"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
)

func keptListImage(size int) *Image {
	return &Image{Bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 4, 4))), size: size}
}

// The return allowance favors newer pages and never adds bytes outside the cache budget.
func TestListCoverRetentionByteAndPageLimits(t *testing.T) {
	for _, test := range []struct{ cost, kept int }{{1 << 20, 3}, {3 << 20, 2}} {
		t.Run(fmt.Sprint(test.cost), func(t *testing.T) {
			c := newArtCache(func(func()) {})
			t.Cleanup(c.Close)
			for page := range 5 {
				key := fmt.Sprintf("list-%d", page)
				c.add(key, keptListImage(test.cost))
				c.noteListCover(navigation.Route{Page: navigation.Playlist, ID: int64(page)}, key)
				c.BeginFrame()
			}
			c.add("large", keptListImage(c.limit-test.cost*test.kept))
			runtime.GC()
			for page := range 5 {
				key := fmt.Sprintf("list-%d", page)
				if (c.art[key] != nil) != (page >= 5-test.kept) {
					t.Fatalf("wrong retention for %s under pressure", key)
				}
			}
			s := listStats(c)
			if s.Bytes > c.limit || s.RetainedListBytes != test.cost*test.kept || s.RetainedListEntries != test.kept || s.Entries != test.kept+1 {
				t.Fatalf("retention exceeded its shared/page allowance: %+v", s)
			}
		})
	}
}

// Scrolling replaces a page's viewport; neither overscan nor past screens accumulate protection.
func TestListCoverRetentionTracksLastViewport(t *testing.T) {
	c := newArtCache(func(func()) {})
	t.Cleanup(c.Close)
	c.limit = 10000
	page := navigation.Route{Page: navigation.Playlist, ID: 1}
	for i := range 40 {
		key := fmt.Sprint(i)
		c.add(key, keptListImage(100))
		c.noteListCover(page, key)
	}
	c.BeginFrame()
	if s := listStats(c); s.RetainedListEntries != 30 {
		t.Fatalf("one viewport retained too many covers: %+v", s)
	}
	for i := 30; i < 40; i++ {
		c.noteListCover(page, fmt.Sprint(i))
	}
	c.BeginFrame()
	c.add("large", keptListImage(9000))
	for i := range 40 {
		if (c.art[fmt.Sprint(i)] != nil) != (i >= 30) {
			t.Fatalf("retained a scrolled-past cover or evicted the latest viewport at %d", i)
		}
	}
	if s := listStats(c); s.Bytes > c.limit || s.RetainedListEntries != 10 {
		t.Fatalf("viewport accumulated old cover costs: %+v", s)
	}
}

// Shared album images count once, and visible work can release the oldest protection at the hard limit.
func TestListCoverRetentionSharesImagesAndYieldsToHardLimit(t *testing.T) {
	c := newArtCache(func(func()) {})
	t.Cleanup(c.Close)
	c.limit = 100
	c.add("shared", keptListImage(20))
	for page := range 3 {
		key := fmt.Sprint(page)
		c.add(key, keptListImage(20))
		p := navigation.Route{Page: navigation.Playlist, ID: int64(page)}
		c.noteListCover(p, "shared")
		c.noteListCover(p, key)
		c.BeginFrame()
	}
	if s := listStats(c); s.RetainedListBytes != 80 || s.RetainedListEntries != 4 {
		t.Fatalf("shared album was charged more than once: %+v", s)
	}
	c.add("visible-large", keptListImage(40))
	if c.art["0"] != nil || c.art["1"] == nil || c.art["2"] == nil || c.art["shared"] == nil || c.size > c.limit {
		t.Fatal("hard pressure did not release the oldest protection first")
	}
}

// Ordinary playback artwork is not promoted to list retention, and revocation cannot resurrect a retained image.
func TestListCoverRetentionDoesNotPinPlaybackOrRevokedImages(t *testing.T) {
	c := newArtCache(func(func()) {})
	t.Cleanup(c.Close)
	c.now = func() time.Time { return time.Unix(100, 0) }
	c.add("playback", keptListImage(100))
	c.Get("playback", false)
	if listStats(c).RetainedListEntries != 0 {
		t.Fatal("playback artwork acquired list protection")
	}
	c.add("list", keptListImage(100))
	c.noteListCover(navigation.Route{Page: navigation.Playlist}, "list")
	c.BeginFrame()
	c.ImageChanged("list", netease.ImageRevoked)
	runtime.GC()
	if listStats(c).RetainedListBytes != 0 || bitmap(c.Get("list", false)) != nil {
		t.Fatal("revoked image remained retained or came back from a weak reference")
	}
}

// Retention accounting is derived only in tests, without counters on the drawing path.
type listCacheStats struct {
	Stats
	RetainedListBytes, RetainedListEntries int
}

func listStats(c *Cache) listCacheStats {
	kept := c.retainedListKeys()
	bytes := 0
	for key := range kept {
		bytes += c.art[key].size
	}
	return listCacheStats{c.Stats(), bytes, len(kept)}
}
