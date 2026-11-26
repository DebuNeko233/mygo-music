/*
验证背景图片的解码并发、关闭时资源回收，以及释放 CPU 像素后的重绘。
*/

package app

import (
	"fmt"
	"image/color"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	artcache "netease-music-mygo/internal/ui/view/artwork"
)

// Many configured paths load only the selected home and playing images.
func TestBackdropDecodePublicationIsBounded(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	dir := t.TempDir()
	for i := range 7 {
		path := filepath.Join(dir, fmt.Sprintf("%d.png", i))
		writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
		if i == 0 {
			a.settings.BackdropPlaying = []string{path}
		} else {
			a.settings.BackdropHome = append(a.settings.BackdropHome, path)
		}
	}
	a.preloadPictures()
	first, second := <-posts, <-posts
	a.postMu.Lock()
	pending := len(a.resources)
	a.postMu.Unlock()
	if pending != 2 {
		t.Fatalf("unbounded pending results: %d", pending)
	}
	first()
	second()
	if a.homePicture() == nil || a.playingPicture() == nil {
		t.Fatal("selected surfaces were not prewarmed")
	}
	a.pics.Close()
	a.closePosts()
}

// Closing dispatch discards unpublished selected pictures; later delivery
// cannot publish them or return their resource slots twice.
func TestBackdropCloseReleasesDecodeSlots(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	dir := t.TempDir()
	for i := range 2 {
		path := filepath.Join(dir, fmt.Sprintf("%d.png", i))
		writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
		if i == 0 {
			a.settings.BackdropHome = []string{path}
		} else {
			a.settings.BackdropPlaying = []string{path}
		}
	}
	a.preloadPictures()
	first, second := <-posts, <-posts
	a.pics.Close()
	a.closePosts()
	first()
	second()
	if a.homePicture() != nil || a.playingPicture() != nil {
		t.Fatal("closed cache published discarded pictures")
	}
}

// Tests of rendering use the actual asynchronous loader before building UI.
func warmTestBackdrops(t *testing.T, a *app) {
	t.Helper()
	posts := testPosts(a)
	a.preloadPictures()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(a.settings.BackdropHome) > 0 && a.homePicture() == nil || len(a.settings.BackdropPlaying) > 0 && a.playingPicture() == nil {
		select {
		case fn := <-posts:
			fn()
		case <-deadline.C:
			t.Fatal("selected backgrounds did not load")
		}
	}
	t.Cleanup(func() { a.pics.Close(); a.closePosts() })
}

// A displayed background can drop CPU pixels and still reconstruct the same
// picture when a renderer needs them again, without replacing the cache entry.
func TestBackdropRedrawAfterPixelRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "red.png")
	writePicture(t, path, 64, 64, color.RGBA{R: 255, A: 255})
	synctest.Test(t, func(t *testing.T) {
		a := newTestApp()
		a.settings = defaultSettings()
		a.settings.BackdropHome = []string{path}
		a.settings.BackdropHomeOpacity = 1
		posts := testPosts(a)
		a.preloadPictures()
		(<-posts)()
		entry := a.homePicture()
		tt := ui.NewTester(a.view, 320, 240)
		tt.Frame()
		if redPixels(tt.Image()) < 320*240/2 {
			t.Fatal("background did not draw before pixel release")
		}
		(<-posts)() // Virtual time advances to the one-shot pixel-release callback.
		tt.Frame()
		if a.homePicture() != entry || redPixels(tt.Image()) < 320*240/2 {
			t.Fatal("released background did not reconstruct on redraw")
		}
		a.closePosts()
	})
}

// Startup prewarms the first selected picture before the player is constructed.
func TestBackdropPreloadBeforePlayerExists(t *testing.T) {
	a := newApp()
	path := filepath.Join(t.TempDir(), "home.png")
	writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
	a.settings.BackdropHome = []string{path}
	posts := testPosts(a)
	a.preloadPictures()
	(<-posts)()
	if a.homePicture() == nil {
		t.Fatal("startup did not prewarm the home picture")
	}
	a.pics.Close()
	a.closePosts()
}

// A song change keeps the old background painted throughout a blocked decode;
// the newly selected image replaces it only after publication.
func TestBackdropSongChangeKeepsOldPictureWhileLoading(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	dir := t.TempDir()
	red, blue := filepath.Join(dir, "red.png"), filepath.Join(dir, "blue.png")
	writePicture(t, red, 32, 32, color.RGBA{R: 255, A: 255})
	writePicture(t, blue, 32, 32, color.RGBA{B: 255, A: 255})
	started, release := make(chan struct{}), make(chan struct{})
	a.pics = artcache.NewPictureCache(artcache.PictureCacheOptions{Post: func(fn func()) { a.dispatch(fn, false) }, PostResource: a.postResource, Load: func(path string) *artcache.Picture {
		if path == blue {
			close(started)
			<-release
		}
		return artcache.DecodePicture(path)
	}})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		a.pics.Close()
		a.closePosts()
	})
	a.settings.BackdropHome = []string{red, blue}
	player.Cue(a.pl, []netease.Track{{ID: 0}})
	tt := ui.NewTester(func(c *ui.Context) {
		if pic := a.homePicture(); pic != nil {
			a.views.playback.HomeBackdrop(c, pic, 1)
		}
	}, 320, 240)
	tt.Frame()
	(<-posts)()
	tt.Frame()
	old := a.homePicture()
	player.Cue(a.pl, []netease.Track{{ID: 1}})
	tt.Frame()
	<-started
	if redPixels(tt.Image()) < 320*240/2 {
		t.Fatal("loading replacement cleared the old background")
	}
	close(release)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for a.homePicture() == old {
		select {
		case fn := <-posts:
			fn()
		case <-deadline.C:
			t.Fatal("replacement did not arrive")
		}
	}
	tt.Frame()
	r, g, b, _ := tt.Image().At(160, 120).RGBA()
	if b < 240*257 || r > 20*257 || g > 20*257 {
		t.Fatal("ready blue picture did not replace red background")
	}
}
