/*
验证首页模块独立发布、局部重试、缓存复用及账号重置后的结果隔离。
*/

package home

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	diskcache "netease-music-mygo/internal/cache/disk"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

func homeTestServices(posts chan func()) HomeServices {
	return HomeServices{
		Post: func(fn func()) { posts <- fn }, Cache: diskcache.Snapshot{},
		Now:         func() time.Time { return time.Date(2026, 11, 23, 8, 0, 0, 0, beijing) },
		Featured:    func() ([]netease.Playlist, error) { return []netease.Playlist{{ID: 1}}, nil },
		Charts:      func() ([]netease.Playlist, error) { return []netease.Playlist{{ID: 2}}, nil },
		Daily:       func() ([]netease.Track, error) { return []netease.Track{{ID: 3}}, nil },
		Recommended: func() ([]netease.Playlist, error) { return []netease.Playlist{{ID: 4}}, nil },
	}
}
func finishHome(t *testing.T, m *Home, posts chan func()) {
	t.Helper()
	for m.Loading {
		testutil.Wait(t, posts, "home response")()
	}
}

// A stalled daily module cannot delay charts, and failures retain data while a successful empty response clears it.
func TestHomeModulesPublishIndependently(t *testing.T) {
	m := Home{Featured: []netease.Playlist{{ID: 10}}, Daily: []netease.Track{{ID: 30}}, Recommended: []netease.Playlist{{ID: 40}}}
	posts := make(chan func(), 8)
	s := homeTestServices(posts)
	s.Personal = true
	var gates [4]chan struct{}
	var release [4]func()
	for i := range gates {
		gates[i] = make(chan struct{})
		gate := gates[i]
		release[i] = sync.OnceFunc(func() { close(gate) })
		t.Cleanup(release[i])
	}
	s.Featured = func() ([]netease.Playlist, error) { <-gates[0]; return nil, netease.ErrOffline }
	s.Charts = func() ([]netease.Playlist, error) { <-gates[1]; return []netease.Playlist{{ID: 2}}, nil }
	s.Daily = func() ([]netease.Track, error) { <-gates[2]; return nil, netease.ErrOffline }
	s.Recommended = func() ([]netease.Playlist, error) { <-gates[3]; return []netease.Playlist{}, nil }
	m.Load(s)
	m.Load(s)
	if !m.Loading || m.pending != 4 || m.At != s.Now() {
		t.Fatal("duplicate load started or full attempt lost its freshness")
	}
	release[1]()
	apply := testutil.Wait(t, posts, "charts while daily stalls")
	if len(m.Charts) != 0 {
		t.Fatal("worker published before Post")
	}
	apply()
	if len(m.Charts) != 1 || m.Charts[0].ID != 2 || !m.Loading || m.pending != 3 {
		t.Fatal("fast module waited for the remaining requests")
	}
	release[0]()
	testutil.Wait(t, posts, "featured failure")()
	release[3]()
	testutil.Wait(t, posts, "empty recommendations")()
	if m.Featured[0].ID != 10 || m.Daily[0].ID != 30 || m.Recommended == nil || len(m.Recommended) != 0 || !m.Loading {
		t.Fatal("partial results erased usable data or ignored a valid empty response")
	}
	release[2]()
	testutil.Wait(t, posts, "daily failure")()
	if m.Loading || m.pending != 0 || len(m.Failures) != 2 || m.Error != "" || !m.NeedsRetry(false) {
		t.Fatal("partial failure did not finish or hid recoverable modules")
	}
}

// Automatic retry skips permanent failures and healthy modules; manual retry can recover a permanent failure.
func TestHomeRetryPreservesHealthyModules(t *testing.T) {
	var m Home
	posts := make(chan func(), 8)
	s := homeTestServices(posts)
	var featured, charts atomic.Int32
	s.Featured = func() ([]netease.Playlist, error) { featured.Add(1); return nil, netease.ErrNotLoggedIn }
	s.Charts = func() ([]netease.Playlist, error) { charts.Add(1); return nil, netease.ErrOffline }
	m.Load(s)
	finishHome(t, &m, posts)
	if m.Error == "" || !m.NeedsRetry(false) {
		t.Fatal("fully failed home was not recoverable")
	}
	attempt := m.At
	s.Charts = func() ([]netease.Playlist, error) { charts.Add(1); return []netease.Playlist{{ID: 2}}, nil }
	m.Retry(false, s)
	finishHome(t, &m, posts)
	if featured.Load() != 1 || charts.Load() != 2 || m.NeedsRetry(false) || !m.NeedsRetry(true) || len(m.Failures) != 1 || m.At != attempt || m.Error != "" {
		t.Fatal("automatic retry repeated permanent/healthy modules or changed full-attempt freshness")
	}
	s.Featured = func() ([]netease.Playlist, error) { featured.Add(1); return []netease.Playlist{{ID: 1}}, nil }
	m.Retry(true, s)
	finishHome(t, &m, posts)
	if featured.Load() != 2 || charts.Load() != 2 || len(m.Failures) != 0 || m.NeedsRetry(true) {
		t.Fatal("manual retry did not recover exactly the failed module")
	}
}

// A signed-out load never requests private recommendations, and repeated loads/focus while busy coalesce.
func TestHomeSignedOutLoadAndFreshen(t *testing.T) {
	var m Home
	posts := make(chan func(), 8)
	s := homeTestServices(posts)
	var featured, charts atomic.Int32
	s.Featured = func() ([]netease.Playlist, error) { featured.Add(1); return []netease.Playlist{{ID: 1}}, nil }
	s.Charts = func() ([]netease.Playlist, error) { charts.Add(1); return []netease.Playlist{{ID: 2}}, nil }
	s.Daily = func() ([]netease.Track, error) { t.Error("signed-out daily request"); return nil, nil }
	s.Recommended = func() ([]netease.Playlist, error) { t.Error("signed-out personalized request"); return nil, nil }
	now := time.Date(2026, 11, 23, 5, 59, 0, 0, beijing)
	s.Now = func() time.Time { return now }
	m.Freshen(s)
	m.Freshen(s)
	m.Load(s)
	finishHome(t, &m, posts)
	if featured.Load() != 1 || charts.Load() != 1 || len(m.Daily) != 0 || len(m.Recommended) != 0 {
		t.Fatal("busy requests did not coalesce")
	}
	now = now.Add(30 * time.Second)
	m.Freshen(s)
	if m.Loading {
		t.Fatal("focus refreshed the same recommendation day")
	}
	now = now.Add(30 * time.Second)
	m.Freshen(s)
	finishHome(t, &m, posts)
	if featured.Load() != 2 || charts.Load() != 2 || m.At != now {
		t.Fatal("crossing Beijing 06:00 did not refresh once")
	}
	m.Freshen(s)
	if m.Loading {
		t.Fatal("new day's successful attempt immediately refreshed again")
	}
}

// Reset retires late results without clearing the new request's loading state or writing the old cache.
func TestHomeResetRejectsOldResults(t *testing.T) {
	var m Home
	posts := make(chan func(), 8)
	s := homeTestServices(posts)
	store := diskcache.New(t.TempDir(), 1<<20)
	t.Cleanup(store.Close)
	s.Cache = diskcache.Snapshot{Store: store, Scope: "old", Epoch: store.Epoch()}
	m.Load(s)
	oldFeatured, oldCharts := testutil.Wait(t, posts, "old result"), testutil.Wait(t, posts, "old result")
	m.Reset()
	s.Cache = diskcache.Snapshot{}
	m.Load(s)
	oldFeatured()
	oldCharts()
	if !m.Loading || m.pending != 2 || len(m.Featured) != 0 || len(m.Charts) != 0 {
		t.Fatal("retired result changed the new generation")
	}
	finishHome(t, &m, posts)
	if m.Featured[0].ID != 1 || m.Charts[0].ID != 2 {
		t.Fatal("new results were rejected")
	}
	if b, _ := store.Read("old", "featured"); b != nil {
		t.Fatal("retired reply wrote its snapshot")
	}
}

// Cached modules appear before stalled network calls, but a refresh never replaces live content with older disk data.
func TestHomeCachedModulesAndLiveContent(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "live-refresh"}[live], func(t *testing.T) {
			var m Home
			if live {
				m.Featured = []netease.Playlist{{ID: 11}}
				m.Charts = []netease.Playlist{{ID: 12}}
				m.Daily = []netease.Track{{ID: 13}}
				m.Recommended = []netease.Playlist{{ID: 14}}
			}
			dir := t.TempDir()
			store := diskcache.New(dir, 1<<20)
			t.Cleanup(store.Close)
			for key, value := range map[string]any{"featured": []netease.Playlist{{ID: 1}}, "charts": []netease.Playlist{{ID: 2}}, "daily": []netease.Track{{ID: 3}}, "recommended": []netease.Playlist{{ID: 4}}} {
				store.Put("7", key, diskcache.Encode(value), store.Epoch())
			}
			posts := make(chan func(), 12)
			s := homeTestServices(posts)
			s.Personal = true
			s.Cache = diskcache.Snapshot{Store: store, Scope: "7", Epoch: store.Epoch()}
			done := make(chan struct{})
			release := sync.OnceFunc(func() { close(done) })
			t.Cleanup(release)
			s.Featured = func() ([]netease.Playlist, error) { <-done; return []netease.Playlist{{ID: 21}}, nil }
			s.Charts = func() ([]netease.Playlist, error) { <-done; return nil, netease.ErrOffline }
			s.Daily = func() ([]netease.Track, error) { <-done; return nil, netease.ErrOffline }
			s.Recommended = func() ([]netease.Playlist, error) { <-done; return []netease.Playlist{}, nil }
			m.Load(s)
			for range 4 {
				testutil.Wait(t, posts, "cached module")()
			}
			offset := int64(0)
			if live {
				offset = 10
			}
			if m.Featured[0].ID != 1+offset || m.Charts[0].ID != 2+offset || m.Daily[0].ID != 3+offset || m.Recommended[0].ID != 4+offset || !m.Loading || m.pending != 4 {
				t.Fatal("disk cache delayed data or overwrote a live module")
			}
			release()
			finishHome(t, &m, posts)
			if m.Featured[0].ID != 21 || m.Charts[0].ID != 2+offset || m.Daily[0].ID != 3+offset || len(m.Recommended) != 0 {
				t.Fatal("network result lost retained cache or failed to apply empty success")
			}
			store.Close()
			reopened := diskcache.New(dir, 1<<20)
			defer reopened.Close()
			b, _ := reopened.Read("7", "featured")
			var featured []netease.Playlist
			if json.Unmarshal(b, &featured) != nil || len(featured) != 1 || featured[0].ID != 21 {
				t.Fatal("accepted snapshot did not persist under the existing cache key")
			}
			b, _ = reopened.Read("7", "recommended")
			var recommended []netease.Playlist
			if json.Unmarshal(b, &recommended) != nil || recommended == nil || len(recommended) != 0 {
				t.Fatal("empty successful snapshot was not persisted")
			}
		})
	}
}
