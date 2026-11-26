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

	"github.com/egoist/mygo/ui"
)

// Incoming decoded pictures occupy at most two publication slots until the
// owner accepts them; a larger configured set still finishes loading.
func TestBackdropDecodePublicationIsBounded(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	dir := t.TempDir()
	for i := range 7 {
		path := filepath.Join(dir, fmt.Sprintf("%d.png", i))
		writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
		a.settings.BackdropHome = append(a.settings.BackdropHome, path)
	}
	a.preloadPictures()
	first, second := <-posts, <-posts
	a.postMu.Lock()
	pending := len(a.resources)
	a.postMu.Unlock()
	if pending != 2 || len(a.pictureJobs) != 2 {
		t.Fatalf("unbounded pending results: %d, slots: %d", pending, len(a.pictureJobs))
	}
	first()
	second()
	loaded := func() int {
		n := 0
		for _, entry := range a.pics {
			if entry.pic != nil {
				n++
			}
		}
		return n
	}
	for loaded() < 7 {
		(<-posts)()
	}
	if len(a.pictureJobs) != 0 {
		t.Fatal("accepted images kept their decode slots")
	}
	a.closePosts()
}

// Closing dispatch discards unpublished pictures and returns their slots;
// later delivery of the already queued callbacks cannot release them twice.
func TestBackdropCloseReleasesDecodeSlots(t *testing.T) {
	a := newTestApp()
	posts := testPosts(a)
	dir := t.TempDir()
	for i := range 2 {
		path := filepath.Join(dir, fmt.Sprintf("%d.png", i))
		writePicture(t, path, 8, 8, color.RGBA{R: 255, A: 255})
		a.settings.BackdropHome = append(a.settings.BackdropHome, path)
	}
	a.preloadPictures()
	first, second := <-posts, <-posts
	a.closePosts()
	if len(a.pictureJobs) != 0 {
		t.Fatal("closed dispatch retained unpublished pictures")
	}
	first()
	second()
	for _, entry := range a.pics {
		if entry.pic != nil {
			t.Fatal("closed dispatch published a discarded picture")
		}
	}
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
		entry := a.pics[path]
		tt := ui.NewTester(a.view, 320, 240)
		tt.Frame()
		if redPixels(tt.Image()) < 320*240/2 {
			t.Fatal("background did not draw before pixel release")
		}
		(<-posts)() // Virtual time advances to the one-shot pixel-release callback.
		tt.Frame()
		if a.pics[path] != entry || redPixels(tt.Image()) < 320*240/2 {
			t.Fatal("released background did not reconstruct on redraw")
		}
		a.closePosts()
	})
}
