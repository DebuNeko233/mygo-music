package recent

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

func recentTestServices(posts chan func()) RecentServices {
	return RecentServices{
		SignedIn: true, Post: func(fn func()) { posts <- fn }, Now: func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
		Cache: func() diskcache.Snapshot { return diskcache.Snapshot{} },
		Songs: func(int) ([]netease.Play, error) { return []netease.Play{{Track: netease.Track{ID: 1}}}, nil },
		Playlists: func(int) ([]netease.Played[netease.Playlist], error) {
			return []netease.Played[netease.Playlist]{{Item: netease.Playlist{ID: 2}}}, nil
		},
		Albums: func(int) ([]netease.Played[netease.AlbumProfile], error) {
			return []netease.Played[netease.AlbumProfile]{{Item: netease.AlbumProfile{Album: netease.Album{ID: 3, Name: "album"}}}}, nil
		},
		Podcasts: func(int) ([]netease.Played[netease.Podcast], error) {
			return []netease.Played[netease.Podcast]{{Item: netease.Podcast{ID: 4}}}, nil
		},
		Voices: func(int) ([]netease.Played[netease.Voice], error) {
			return []netease.Played[netease.Voice]{{Item: netease.Voice{Program: netease.Program{ID: 5}}}}, nil
		},
	}
}
func finishRecent(t *testing.T, d *Recent, posts chan func()) {
	t.Helper()
	for d.Loading() {
		testutil.Wait(t, posts, "recent publication")()
	}
}

// All five requests run independently, failed tabs retain data, and their accepted responses coalesce into one complete snapshot.
func TestRecentIndependentTabsAndSnapshot(t *testing.T) {
	d := Recent{snapshotChecked: true}
	d.SetSongs([]netease.Play{{Track: netease.Track{ID: 9}}})
	d.Tabs[RecentSongs].Shown = true
	dir := t.TempDir()
	store := diskcache.New(dir, 1<<20)
	t.Cleanup(store.Close)
	posts := make(chan func(), 16)
	s := recentTestServices(posts)
	s.Cache = func() diskcache.Snapshot {
		return diskcache.Snapshot{Store: store, Scope: "7", Key: "recent", Epoch: store.Epoch()}
	}
	var gates [RecentTabCount]chan struct{}
	var release [RecentTabCount]func()
	started := make(chan int, 10)
	for i := range gates {
		gate := make(chan struct{})
		gates[i] = gate
		release[i] = sync.OnceFunc(func() { close(gate) })
		t.Cleanup(release[i])
	}
	block := func(tab, limit int) {
		if limit != 300 {
			t.Errorf("request limit %d", limit)
		}
		started <- tab
		<-gates[tab]
	}
	s.Songs = func(limit int) ([]netease.Play, error) { block(RecentSongs, limit); return nil, netease.ErrOffline }
	pl, al, pod, vo := s.Playlists, s.Albums, s.Podcasts, s.Voices
	s.Playlists = func(limit int) ([]netease.Played[netease.Playlist], error) {
		block(RecentPlaylists, limit)
		return pl(limit)
	}
	s.Albums = func(limit int) ([]netease.Played[netease.AlbumProfile], error) {
		block(RecentAlbums, limit)
		return al(limit)
	}
	s.Podcasts = func(limit int) ([]netease.Played[netease.Podcast], error) {
		block(RecentPodcasts, limit)
		return pod(limit)
	}
	s.Voices = func(limit int) ([]netease.Played[netease.Voice], error) { block(RecentVoices, limit); return vo(limit) }
	d.Load(s)
	d.Load(s)
	seen := map[int]bool{}
	for range RecentTabCount {
		seen[testutil.Wait(t, started, "concurrent request")] = true
	}
	if len(seen) != RecentTabCount {
		t.Fatal("requests were duplicated or serialized")
	}
	for _, tab := range []int{RecentAlbums, RecentSongs, RecentPlaylists, RecentPodcasts} {
		release[tab]()
		testutil.Wait(t, posts, "independent tab")()
	}
	if !d.Loading() || !d.Tabs[RecentAlbums].Shown || len(d.AlbumItems) != 1 || d.AlbumItems[0].ID != 3 || d.Tracks[0].ID != 9 || d.Tabs[RecentSongs].Failure != netease.ErrOffline {
		t.Fatal("stalled voice tab hid results or a failed songs response erased usable data")
	}
	if b, _ := store.Read("7", "recent"); b != nil {
		t.Fatal("partial concurrent replies wrote multiple incomplete snapshots")
	}
	release[RecentVoices]()
	testutil.Wait(t, posts, "last tab")()
	if d.Loading() || d.snapshotDirty {
		t.Fatal("final response did not finish snapshot coalescing")
	}
	store.Close()
	reopened := diskcache.New(dir, 1<<20)
	defer reopened.Close()
	b, _ := reopened.Read("7", "recent")
	var saved RecentSnapshot
	if json.Unmarshal(b, &saved) != nil || saved.Tabs != 31 || saved.Songs[0].Track.ID != 9 || saved.Albums[0].Item.ID != 3 || saved.Voices[0].Item.Program.ID != 5 {
		t.Fatal("coalesced snapshot lost another tab or the old successful songs")
	}
}

// Legacy snapshots without a tab mask stay readable, while a masked snapshot does not claim unavailable tabs loaded.
func TestRecentSnapshotCompatibilityAndFailure(t *testing.T) {
	for _, mask := range []uint8{0, 1 << RecentAlbums} {
		t.Run(map[uint8]string{0: "legacy", 1 << RecentAlbums: "masked"}[mask], func(t *testing.T) {
			store := diskcache.New(t.TempDir(), 1<<20)
			defer store.Close()
			store.Put("7", "recent", diskcache.Encode(RecentSnapshot{Tabs: mask, Songs: []netease.Play{{Track: netease.Track{ID: 42}}}, Albums: []netease.Played[netease.AlbumProfile]{{Item: netease.AlbumProfile{Album: netease.Album{ID: 77}}}}}), store.Epoch())
			var d Recent
			posts := make(chan func(), 16)
			s := recentTestServices(posts)
			s.Cache = func() diskcache.Snapshot {
				return diskcache.Snapshot{Store: store, Scope: "7", Key: "recent", Epoch: store.Epoch()}
			}
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			s.Songs = func(int) ([]netease.Play, error) { <-release; return nil, netease.ErrOffline }
			s.Playlists = func(int) ([]netease.Played[netease.Playlist], error) { <-release; return nil, netease.ErrOffline }
			s.Albums = func(int) ([]netease.Played[netease.AlbumProfile], error) { <-release; return nil, netease.ErrOffline }
			s.Podcasts = func(int) ([]netease.Played[netease.Podcast], error) { <-release; return nil, netease.ErrOffline }
			s.Voices = func(int) ([]netease.Played[netease.Voice], error) { <-release; return nil, netease.ErrOffline }
			d.Load(s)
			if !d.ReadingSnapshot || len(d.Tracks) != 0 {
				t.Fatal("snapshot was read synchronously on the owner thread")
			}
			testutil.Wait(t, posts, "snapshot")()
			if d.ReadingSnapshot || len(d.Tracks) != 1 || d.Tracks[0].ID != 42 || len(d.AlbumItems) != 1 || d.AlbumItems[0].ID != 77 {
				t.Fatal("cache failed to restore derived view data before the network")
			}
			for i, tab := range d.Tabs {
				if tab.Shown != (mask == 0 || mask&(1<<i) != 0) {
					t.Fatal("legacy/masked availability changed")
				}
			}
			unblock()
			finishRecent(t, &d, posts)
			if d.Tracks[0].ID != 42 || d.AlbumItems[0].ID != 77 {
				t.Fatal("failed refresh erased cached data")
			}
		})
	}
}

// Replacing or clearing the cache during its read rejects the old snapshot and uses the new lifetime for network persistence.
func TestRecentSnapshotCacheLifetime(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "epoch"
		if replace {
			name = "store"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			current := diskcache.New(dir, 1<<20)
			defer func() { current.Close() }()
			current.Put("7", "recent", diskcache.Encode(RecentSnapshot{Songs: []netease.Play{{Track: netease.Track{ID: 100}}}}), current.Epoch())
			var d Recent
			posts := make(chan func(), 16)
			s := recentTestServices(posts)
			s.Cache = func() diskcache.Snapshot {
				return diskcache.Snapshot{Store: current, Scope: "7", Key: "recent", Epoch: current.Epoch()}
			}
			s.CacheCurrent = func(req diskcache.Snapshot) bool { return req.Store == current && req.Epoch == current.Epoch() }
			d.Load(s)
			old := testutil.Wait(t, posts, "retired cache read")
			if replace {
				current.Close()
				dir = t.TempDir()
				current = diskcache.New(dir, 1<<20)
			} else {
				done := make(chan bool, 1)
				current.Clear("7", func() { done <- true })
				testutil.Wait(t, done, "cache clear")
			}
			old()
			if len(d.Tracks) != 0 || !d.Loading() {
				t.Fatal("retired cache data appeared or prevented network refresh")
			}
			finishRecent(t, &d, posts)
			current.Close()
			current = diskcache.New(dir, 1<<20)
			b, _ := current.Read("7", "recent")
			var saved RecentSnapshot
			if json.Unmarshal(b, &saved) != nil || len(saved.Songs) != 1 || saved.Songs[0].Track.ID != 1 {
				t.Fatal("network result used the retired cache epoch/store")
			}
		})
	}
}

// Reset retires pending tab replies while preserving tab selection; signed-out loads cannot restart private jobs.
func TestRecentResetAndSignedOut(t *testing.T) {
	d := Recent{Selected: RecentVoices}
	posts := make(chan func(), 16)
	s := recentTestServices(posts)
	d.Load(s)
	old := make([]func(), RecentTabCount)
	for i := range old {
		old[i] = testutil.Wait(t, posts, "old recent reply")
	}
	d.Reset()
	if d.Selected != RecentVoices || d.Loading() || len(d.Tracks) != 0 {
		t.Fatal("reset lost selection or retained private data/jobs")
	}
	signedOut := s
	signedOut.SignedIn = false
	signedOut.Cache = func() diskcache.Snapshot { t.Fatal("signed-out cache request"); return diskcache.Snapshot{} }
	d.Load(signedOut)
	d.LoadTab(RecentSongs, signedOut)
	if d.Loading() {
		t.Fatal("signed-out command started private requests")
	}
	d.Load(s)
	for _, apply := range old {
		apply()
	}
	if !d.Tabs[RecentSongs].Loading || len(d.Tracks) != 0 {
		t.Fatal("old reply changed a new login's loading state")
	}
	finishRecent(t, &d, posts)
	if len(d.Tracks) != 1 || d.Tracks[0].ID != 1 {
		t.Fatal("new generation failed to publish")
	}
}

// Only stale tabs revalidate automatically, failed attempts obey the shared interval, and explicit retry updates only that tab.
func TestRecentFreshnessAndRetry(t *testing.T) {
	var d Recent
	posts := make(chan func(), 16)
	s := recentTestServices(posts)
	now := s.Now()
	s.Now = func() time.Time { return now }
	var songs atomic.Int32
	s.Songs = func(int) ([]netease.Play, error) { songs.Add(1); return nil, netease.ErrOffline }
	d.Load(s)
	finishRecent(t, &d, posts)
	if d.NeedsLoad(now) || songs.Load() != 1 {
		t.Fatal("fresh/failed record immediately polled again")
	}
	now = now.Add(viewmodel.ListsFresh - time.Nanosecond)
	if d.NeedsLoad(now) {
		t.Fatal("refreshed before the shared interval")
	}
	now = now.Add(time.Nanosecond)
	if !d.NeedsLoad(now) {
		t.Fatal("stale records did not revalidate")
	}
	for i := range d.Tabs {
		if i != RecentSongs {
			d.Tabs[i].At = now
		}
	}
	d.Load(s)
	finishRecent(t, &d, posts)
	if songs.Load() != 2 || d.Tabs[RecentSongs].Failure == nil {
		t.Fatal("stale failed tab did not finish its retry")
	}
	before := d.Tabs[RecentAlbums].At
	s.Songs = func(int) ([]netease.Play, error) { songs.Add(1); return []netease.Play{}, nil }
	d.LoadTab(RecentSongs, s)
	d.LoadTab(RecentSongs, s)
	finishRecent(t, &d, posts)
	if songs.Load() != 3 || !d.Tabs[RecentSongs].Shown || d.Tabs[RecentSongs].Failure != nil || len(d.Tracks) != 0 || d.Tabs[RecentAlbums].At != before {
		t.Fatal("retry duplicated, failed to apply empty success or touched healthy tabs")
	}
}
