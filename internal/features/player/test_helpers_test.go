/*
提供播放器测试使用的音频 HTTP 服务、请求计数和异步播放推进夹具。
*/

package player

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// songServer serves the song b as the stream of each track asked for, and
// counts the requests each track got.
func songServer(t *testing.T, b []byte) (*httptest.Server, func(id int64) int) {
	t.Helper()
	var mu sync.Mutex
	hits := map[int64]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/"), 10, 64)
		mu.Lock()
		hits[id]++
		mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, func(id int64) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[id]
	}
}

// serveURL makes songURL answer with a stream of the song id at srv.
func serveURL(t *testing.T, srv *httptest.Server, size int) {
	t.Helper()
	old := songURL
	songURL = func(id int64, level string) (*netease.Stream, error) {
		return &netease.Stream{URL: fmt.Sprintf("%s/%d", srv.URL, id), Size: int64(size), Level: level, Type: "mp3"}, nil
	}
	t.Cleanup(func() { songURL = old })
}

// pump runs what the player posts on the test's goroutine, until done is
// true: a player only changes as its posts run.
func pump(t *testing.T, ch chan func(), what string, done func() bool) {
	t.Helper()
	pumpFor(t, ch, what, 10*time.Second, done)
}

// pumpFor runs the player's posts until done, allowing for a deliberately
// long retry gap when the test exercises NetEase's risk check.
func pumpFor(t *testing.T, ch chan func(), what string, wait time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s", what)
		}
		select {
		case fn := <-ch:
			fn()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// newLoadPlayer returns a player with the given cache and the
// sound off: it plays for real, as the load does.
func newLoadPlayer(t *testing.T, cache *Cache) (chan func(), *Player) {
	t.Helper()
	ch := make(chan func(), 64)
	p := New(func(fn func()) { ch <- fn })
	p.Cache = cache
	p.volume = 0
	t.Cleanup(func() {
		p.stop()
		p.dropAhead()
		// Nothing new starts once the player has moved on, but what came
		// in, and what plays, let go of their files on other goroutines: a
		// moment after, the audio closes, and a fetch posts. The test's
		// cache cannot go while they hold it.
		p.gen++
		time.Sleep(150 * time.Millisecond)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if !run(ch) && !comingIn(cache) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	return ch, p
}

// run runs what the player posted, and reports whether anything was.
func run(ch chan func()) bool {
	any := false
	for {
		select {
		case fn := <-ch:
			fn()
			any = true
		default:
			return any
		}
	}
}

// comingIn reports whether a song still comes into the cache's directory.
func comingIn(c *Cache) bool {
	if c == nil {
		return false
	}
	parts, _ := filepath.Glob(filepath.Join(c.dir, "*.part"))
	return len(parts) > 0
}

// song returns the song the tests play: five seconds of MP3.
func song(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// downloaded returns an in-memory download that has all of b in already.
func downloaded(b []byte) *download {
	d := opened(nil, int64(len(b)))
	d.pages = make(map[int64][]byte)
	for off := 0; off < len(b); off += downloadPageSize {
		d.pages[int64(off/downloadPageSize)] = b[off:min(off+downloadPageSize, len(b))]
	}
	return d
}

// prune is the test's equivalent of maintenance after resetting spare space.
func (c *Cache) prune(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spare = 0
	c.pruneLocked(keep, 0)
}
