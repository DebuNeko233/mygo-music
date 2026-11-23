package viewmodel

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
)

func playlistFixture() (*Library, PlaylistServices, chan func()) {
	m := new(Library)
	m.Reset()
	m.Refresh.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	posts := make(chan func(), 8)
	s := PlaylistServices{
		Post:         func(update func() bool) { posts <- func() { update() } },
		Cache:        func(string) diskcache.Snapshot { return diskcache.Snapshot{} },
		WaitSnapshot: func(diskcache.Snapshot) {},
		Detail:       func(id int64) (*netease.Playlist, error) { return &netease.Playlist{ID: id}, nil },
		SaveSnapshot: func(diskcache.Snapshot, func() []byte) {},
		Play:         func(*netease.Playlist) {}, VisiblePlaylist: func() int64 { return 7 }, Recovering: func() bool { return false },
	}
	return m, s, posts
}

// Coalesced play requests use the offline snapshot once; the network refresh changes only the page.
func TestPlaylistLoadCoalescesPlayThroughOfflineSnapshot(t *testing.T) {
	m, s, posts := playlistFixture()
	store := diskcache.New(t.TempDir(), 1<<20)
	defer store.Close()
	req := diskcache.Snapshot{Store: store, Scope: "1", Key: "playlist/7", Epoch: store.Epoch()}
	store.Put(req.Scope, req.Key, diskcache.Encode(netease.Playlist{ID: 7, Name: "offline", Tracks: []netease.Track{{ID: 1}}}), req.Epoch)
	s.Cache = func(string) diskcache.Snapshot { return req }
	var calls atomic.Int32
	s.Detail = func(id int64) (*netease.Playlist, error) {
		calls.Add(1)
		return &netease.Playlist{ID: id, Name: "fresh", Tracks: []netease.Track{{ID: 2}}}, nil
	}
	var played []int64
	s.Play = func(pl *netease.Playlist) { played = append(played, pl.Tracks[0].ID) }
	m.LoadPlaylist(7, false, s)
	m.LoadPlaylist(7, true, s)
	m.LoadPlaylist(7, true, s)
	cached := wait(t, posts, "offline snapshot")
	fresh := wait(t, posts, "network result")
	if m.Playlists[7] != nil || !m.Loading[7] || len(played) != 0 {
		t.Fatal("worker published before dispatch")
	}
	cached()
	if m.Playlists[7].Name != "offline" || m.Loading[7] || !reflect.DeepEqual(played, []int64{1}) {
		t.Fatal("pending play did not use the offline list exactly once")
	}
	fresh()
	if calls.Load() != 1 || m.Playlists[7].Name != "fresh" || !reflect.DeepEqual(played, []int64{1}) || m.PlayWhenLoaded[7] {
		t.Fatal("refresh restarted playback or duplicated the request")
	}
}

// Both disk and network completions must respect a forgotten request or a reset lifetime before touching flags or effects.
func TestPlaylistLoadRejectsRetiredCompletions(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "forget", true: "reset"}[reset], func(t *testing.T) {
			m, s, posts := playlistFixture()
			store := diskcache.New(t.TempDir(), 1<<20)
			defer store.Close()
			req := diskcache.Snapshot{Store: store, Key: "playlist/7", Epoch: store.Epoch()}
			store.Put("", req.Key, diskcache.Encode(netease.Playlist{ID: 7, Name: "old"}), req.Epoch)
			s.Cache = func(string) diskcache.Snapshot { return req }
			effects := 0
			s.Play = func(*netease.Playlist) { effects++ }
			s.SaveSnapshot = func(diskcache.Snapshot, func() []byte) { effects++ }
			m.LoadPlaylist(7, true, s)
			cached, fresh := wait(t, posts, "old disk result"), wait(t, posts, "old network result")
			if reset {
				m.Reset()
			} else {
				m.ForgetPlaylist(7)
			}
			current := m.playlistRequest(7)
			current.Busy = true
			m.Loading[7] = true
			m.PlayWhenLoaded[7] = true
			cached()
			fresh()
			if !current.Busy || !m.Loading[7] || !m.PlayWhenLoaded[7] || m.Playlists[7] != nil || effects != 0 {
				t.Fatal("retired completion finished or published into its replacement")
			}
		})
	}
}

// A rejected play result survives an overlapping edit, then retries once and stops after a failed follow-up.
func TestPlaylistLoadRetriesAfterEditWithoutLoop(t *testing.T) {
	m, s, posts := playlistFixture()
	var calls atomic.Int32
	s.Detail = func(id int64) (*netease.Playlist, error) {
		if calls.Add(1) == 1 {
			return &netease.Playlist{ID: id}, nil
		}
		return nil, errors.New("offline")
	}
	m.LoadPlaylist(7, true, s)
	finish := m.BeginEdit()
	wait(t, posts, "overlapping load")()
	if !m.PlayWhenLoaded[7] || !m.playlistRequest(7).Retry || m.NeedsRefreshRetry(0, 7) {
		t.Fatal("overlapping edit lost pending play or bypassed the edit gate")
	}
	finish()
	services := LibraryServices{Playlist: s}
	if !m.NeedsRefreshRetry(0, 7) {
		t.Fatal("edit completion did not enable a retry")
	}
	m.RetryRefresh(0, 7, services)
	m.RetryRefresh(0, 7, services)
	wait(t, posts, "failed retry")()
	if calls.Load() != 2 || m.NeedsRefreshRetry(0, 7) || m.playlistRequest(7).Busy || !m.playlistRequest(7).Failed {
		t.Fatal("failed follow-up looped or remained busy")
	}
}

// An identical revalidation keeps both the borrowed tracks and idle redraw budget; changed privileges still publish.
func TestPlaylistLoadKeepsUnchangedTracksAndRedrawBudget(t *testing.T) {
	m, s, posts := playlistFixture()
	old := &netease.Playlist{ID: 7, Tracks: []netease.Track{{ID: 1}}}
	m.Playlists[7] = old
	changed := true
	s.Post = func(update func() bool) { posts <- func() { changed = update() } }
	s.Detail = func(int64) (*netease.Playlist, error) { p := library.CopyPlaylist(*old); return &p, nil }
	m.LoadPlaylist(7, false, s)
	wait(t, posts, "unchanged refresh")()
	if changed || m.Playlists[7] != old {
		t.Fatal("identical refresh replaced data or requested a redraw")
	}
	m.playlistRequest(7).At = time.Time{}
	s.Detail = func(int64) (*netease.Playlist, error) {
		p := library.CopyPlaylist(*old)
		p.Tracks[0].Privilege = &netease.Privilege{ID: 1, St: -1}
		return &p, nil
	}
	m.LoadPlaylist(7, false, s)
	wait(t, posts, "privilege refresh")()
	if !changed || m.Playlists[7].Tracks[0].Playable() || !old.Tracks[0].Playable() {
		t.Fatal("privilege change was lost or mutated the borrowed tracks")
	}
}

// Retention protects visible, loading, edited, unsaved and pending-play entries while evicting older eligible details.
func TestPlaylistRetentionProtectsOwners(t *testing.T) {
	var m Library
	m.Reset()
	for id := int64(1); id <= 10; id++ {
		m.Playlists[id] = &netease.Playlist{ID: id}
		m.UsePlaylist(id)
	}
	m.playlistRequest(2).Busy = true
	m.DirtySnapshots = map[int64]bool{3: true}
	m.PlayWhenLoaded[4] = true
	m.Refresh.EditedLists = map[int64]struct{}{5: {}}
	m.TrimPlaylists(1)
	if len(m.Playlists) != PlaylistCachePages || m.Playlists[6] != nil || m.Playlists[7] != nil {
		t.Fatal("eviction ignored ownership or visit order")
	}
	for id := int64(1); id <= 5; id++ {
		if m.Playlists[id] == nil {
			t.Fatalf("evicted protected entry %d", id)
		}
	}
}

// Track IDs also count toward retention, and an oversized visible playlist is kept without evicting it in a loop.
func TestPlaylistRetentionBoundsTrackIDsAndResumesAfterEdit(t *testing.T) {
	var m Library
	m.Reset()
	for id := int64(1); id <= 3; id++ {
		m.Playlists[id] = &netease.Playlist{ID: id, TrackIDs: make([]netease.TrackID, 8000)}
		m.UsePlaylist(id)
	}
	finish := m.BeginEdit()
	m.TrimPlaylists(3)
	if len(m.Playlists) != 3 {
		t.Fatal("evicted during an edit")
	}
	finish()
	m.TrimPlaylists(3)
	if m.Playlists[1] != nil || len(m.Playlists) != 2 {
		t.Fatal("track-ID budget was not resumed after the edit")
	}
	m.Playlists[3].TrackIDs = make([]netease.TrackID, PlaylistCacheTracks+1)
	m.TrimPlaylists(3)
	if len(m.Playlists) != 1 || m.Playlists[3] == nil {
		t.Fatal("oversized visible playlist was evicted")
	}
}

// Connection recovery retries temporary failures, while a refused playlist waits for a manual refresh and keeps its old content.
func TestPlaylistRecoverySeparatesManualAndAutomaticRetries(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		t.Run(map[bool]string{false: "permanent", true: "temporary"}[temporary], func(t *testing.T) {
			m, s, posts := playlistFixture()
			old := &netease.Playlist{ID: 7, Name: "old"}
			m.Playlists[7] = old
			m.TouchPlaylist(7)
			r := m.playlistRequest(7)
			r.Failed = true
			r.Failure = errors.New("refused")
			if temporary {
				r.Failure = netease.ErrOffline
			}
			if m.NeedsRecovery(7, false) != temporary || !m.NeedsRecovery(7, true) {
				t.Fatal("recovery classified the failure incorrectly")
			}
			services := LibraryServices{Playlist: s}
			m.RetryFailed(0, 7, false, services)
			if !temporary {
				if r.Busy || m.Playlists[7] != old {
					t.Fatal("automatic recovery retried a permanent failure")
				}
				m.RetryFailed(0, 7, true, services)
			}
			if !r.Busy || m.Playlists[7] != old {
				t.Fatal("retry did not retain the old page")
			}
			wait(t, posts, "recovered playlist")()
			if r.Failed || r.Busy || m.Playlists[7] == old {
				t.Fatal("recovery did not finish through normal acceptance")
			}
		})
	}
}
