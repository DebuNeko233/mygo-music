package app

import (
	"errors"
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/netease"
)

// Closing dispatch releases posted cover byte budgets and slots even if their UI callback never runs.
func TestArtShutdownReleasesPostedResults(t *testing.T) {
	a := newApp()
	posts := testPosts(a)
	c := artcache.New(artcache.Options{Post: a.post, PostResource: a.postResource, Load: func(string) (*ui.Bitmap, image.Image, int, error) {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		return ui.NewBitmap(img), img, 512, nil
	}})

	t.Cleanup(c.Close)
	c.Get("pending-cover", false)
	fn := wait(t, posts, "cover result")
	c.Close()
	a.closePosts()
	fn()
	stats := c.Stats()
	if stats.PendingBytes != 0 || stats.Jobs != 0 || stats.Entries != 0 {
		t.Fatalf("retained cover resources: %+v", stats)
	}
}

// Built rows outside the viewport must leave decode slots for the visible rows.
func TestCoverRequestsFollowTheViewport(t *testing.T) {
	a := newTestApp()
	post := make(chan func(), artJobs)
	requests := make(chan string, artJobs)
	a.art = artcache.New(artcache.Options{Post: func(fn func()) { post <- fn }, Load: func(url string) (*ui.Bitmap, image.Image, int, error) {
		requests <- url
		return nil, nil, 0, errors.New("fixture has no artwork")
	}})
	defer a.art.Close()

	const above = "https://fixture.invalid/above"
	const below = "https://fixture.invalid/below"
	tt := ui.NewTester(func(c *ui.Context) {
		a.art.BeginFrame()
		ui.Scroll(c).Fill().Children(func() {
			a.views.artwork.Cover(c.Key("above"), above, 80, 22, 4)
			ui.Box(c).Height(400)
			a.views.artwork.Cover(c.Key("below"), below, 80, 22, 4)
		})
	}, 140, 120)
	if got := wait(t, requests, "visible cover"); got != netease.Sized(above, 80) || a.art.Stats().Pending != 1 {
		t.Fatalf("unexpected initial request %q (%d loads)", got, a.art.Stats().Pending)
	}
	wait(t, post, "visible cover completion")()
	tt.Scroll(10, 10, 0, 420)
	if got := wait(t, requests, "cover after scrolling"); got != netease.Sized(below, 80) {
		t.Fatalf("scroll requested %q, want the newly visible cover", got)
	}
	wait(t, post, "scrolled cover completion")()
}
