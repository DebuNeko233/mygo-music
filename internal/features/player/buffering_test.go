/*
验证缓存命中不报告网络缓冲，且旧请求不影响替换后的歌曲状态。
*/

package player

import (
	"testing"
	"time"

	"netease-music-mygo/internal/features/netease"
)

// Cached tracks remain loading until ready without reporting network buffering, with or without prefetch.
func TestCachedLoadsDoNotReportBuffering(t *testing.T) {
	defer noSongSources(t)()
	cache := NewCache(t.TempDir(), 1<<20)
	data := song(t)
	seedCache(t, cache, 1, data)
	seedCache(t, cache, 2, data)
	posts, p := newLoadPlayer(t, cache)
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	ready := func() {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for !p.Ready() {
			if p.Buffering() {
				t.Fatal("cached audio reported buffering before it became ready")
			}
			select {
			case fn := <-posts:
				fn()
			case <-deadline.C:
				t.Fatal("cached audio did not become ready")
			}
		}
		if p.Buffering() || p.Loading() || p.Err() != "" {
			t.Fatal("cached audio remained buffering or failed")
		}
	}
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)
	ready()
	select {
	case <-p.ahead.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("cached prefetch did not finish")
	}
	if p.ahead.dl == nil {
		t.Fatal("the next cached track was not prefetched")
	}
	p.Next()
	ready()
}

// An uncached URL request must report buffering even while it is still running in prefetch.
func TestUncachedLoadsReportBufferingBeforeURLReturns(t *testing.T) {
	for _, prefetched := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "prefetched"}[prefetched], func(t *testing.T) {
			asked, release := make(chan struct{}), make(chan struct{})
			old := songURL
			songURL = func(int64, string) (*netease.Stream, error) {
				close(asked)
				<-release
				return nil, netease.ErrUnavailable
			}
			t.Cleanup(func() { songURL = old })
			posts, p := newLoadPlayer(t, NewCache(t.TempDir(), 1<<20))
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
			Cue(p, []netease.Track{{ID: 1, Duration: 5000}})
			if prefetched {
				p.startPrefetch(1, "exhigh", nil, songURL, 0)
				select {
				case <-asked:
				case <-time.After(5 * time.Second):
					t.Fatal("prefetch did not request a URL")
				}
			}
			p.load(0, 1)
			pump(t, posts, "network buffering", p.Buffering)
			if !p.Loading() || p.Ready() {
				t.Fatal("a pending remote load was treated as ready")
			}
			close(release)
			pump(t, posts, "failed remote load", func() bool { return !p.Loading() })
			if p.Buffering() || p.Err() == "" {
				t.Fatal("a failed remote load kept buffering or lost its error")
			}
		})
	}
}

// A delayed remote notification from the old track must not mark its cached replacement as buffering.
func TestOldRemoteLoadDoesNotMarkCachedReplacementBuffering(t *testing.T) {
	asked, release := make(chan struct{}), make(chan struct{})
	old := songURL
	songURL = func(int64, string) (*netease.Stream, error) {
		close(asked)
		<-release
		return nil, netease.ErrUnavailable
	}
	t.Cleanup(func() { songURL = old })
	cache := NewCache(t.TempDir(), 1<<20)
	seedCache(t, cache, 2, song(t))
	posts, p := newLoadPlayer(t, cache)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	p.FetchLyrics = func(int64) (string, string, string, error) { return "", "", "", nil }
	p.PlayQueue([]netease.Track{{ID: 1, Duration: 5000}, {ID: 2, Duration: 5000}}, 0)
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the old track did not request a URL")
	}
	p.Next()
	pump(t, posts, "cached replacement", func() bool {
		if p.Buffering() {
			t.Fatal("the old remote load marked cached audio as buffering")
		}
		return p.Ready()
	})
	if p.Current().ID != 2 {
		t.Fatal("the cached replacement was not selected")
	}
	close(release)
}
