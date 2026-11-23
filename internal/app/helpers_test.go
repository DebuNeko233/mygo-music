package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/player"

	// isolateUserData keeps what the app writes, the settings, the session and
	// the transcripts, in a directory of the test's: it wrote them to the real
	// one, which fails on a machine where that cannot be written.
	"netease-music-mygo/internal/pagecache"
)

func isolateUserData(t *testing.T) {
	t.Helper()
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
}

func newTestApp() *app {
	a := &app{
		searches: pagecache.New[route, *searchData](searchBusy),
		lyricAt:  -1, queueAt: -1,
	}
	a.library.Reset()
	a.artists.Reset()
	a.albums.Reset()
	a.users.Reset()
	a.podcasts.Reset()
	a.art = newArtCache(func(func()) {})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}

// testPosts gives each app its own UI queue; install it before starting work.
func testPosts(a *app) chan func() {
	posts := make(chan func(), 64)
	a.postMu.Lock()
	a.postTo = func(fn func(), _ bool) { posts <- fn }
	a.postMu.Unlock()
	return posts
}

// drag drags from the middle of r by dx, over a few frames, and lets go
// after hold, still.
func drag(tt *ui.Tester, r ui.Rect, dx float32, hold time.Duration) {
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	for i := 1; i <= 6; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x+dx*float32(i)/6, y)
	}
	if hold > 0 {
		tt.Advance(hold)
	}
	tt.Release(x+dx, y)
	tt.Advance(time.Second)
	tt.Frame()
}
