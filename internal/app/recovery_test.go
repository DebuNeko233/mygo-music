package app

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/netease"
)

// Permanent page failures used to keep retrying on focus and network recovery.
func TestPermanentFailureStopsRetry(t *testing.T) {
	a, posts := offlineFixture(t)
	old := albumDetail
	t.Cleanup(func() { albumDetail = old })
	calls := 0
	albumDetail = func(int64) (*netease.AlbumProfile, error) { calls++; return nil, netease.ErrNotLoggedIn }
	a.route = route{page: pageAlbum, id: 9}
	a.loadAlbum(9)
	refreshPost(t, posts)
	a.recoveryFrame(true)
	a.networkChanged(mygo.NetworkStatus{Online: false})
	a.networkChanged(mygo.NetworkStatus{Online: true})
	if a.needsRecovery() || a.recovery.timer != nil || a.albums[9].loading || calls != 1 {
		t.Fatal("permanent failure started an automatic request or timer")
	}
	if !a.failedLoads(true) {
		t.Fatal("permanent failure lost its manual refresh action")
	}
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		calls++
		return &netease.AlbumProfile{Album: netease.Album{ID: id}}, nil
	}
	a.retryLoads(true)
	refreshPost(t, posts)
	if calls != 2 || a.albums[9].album == nil || a.albums[9].err != "" {
		t.Fatal("explicit refresh could not recover a permanent failure")
	}
}

// Recovery must not fetch a failed hidden artist tab until that tab becomes visible.
func TestRecoverySkipsHiddenArtistTab(t *testing.T) {
	a, _ := offlineFixture(t)
	a.route = route{page: pageArtist, id: 9}
	d := a.artist(9)
	d.profile = &netease.ArtistProfile{}
	d.latestErr, d.latestFailure = "failed", netease.ErrOffline
	if a.needsRecovery() {
		t.Fatal("hidden tab keeps background recovery alive")
	}
	a.recoverLoads()
	if d.latestLoading {
		t.Fatal("recovery fetched a hidden tab")
	}
	a.artistTab = artistLatest
	if !a.needsRecovery() {
		t.Fatal("visible temporary failure cannot recover")
	}
}

// Failed serialization used to be silent and could replace a valid snapshot with empty data.
func TestCacheSerializationFailurePreservesSnapshot(t *testing.T) {
	a, _ := offlineFixture(t)
	req := a.cacheRequest("test")
	req.store.Put(req.scope, req.key, []byte("kept"), req.epoch)
	var output bytes.Buffer
	writer := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(writer) })
	bad := struct {
		Private string
		Invalid chan int
	}{"session-secret", make(chan int)}
	b := cacheBytes(bad)
	req.keep(b)
	write := req.store.Prepare(req.scope, req.key, req.epoch)
	write(b) // Prepared library snapshots use this separate path.
	if b != nil || !strings.Contains(output.String(), "cache: cannot encode") || strings.Contains(output.String(), bad.Private) {
		t.Fatal("encoding failure was silent or leaked the payload")
	}
	got, _ := req.store.Read(req.scope, req.key)
	if string(got) != "kept" {
		t.Fatal("bad encoder replaced the old snapshot")
	}
}

// Retrying a temporary home failure must not repeatedly request another module's permanent failure.
func TestRecoverySeparatesHomeFailures(t *testing.T) {
	a, posts := offlineFixture(t)
	a.profile = nil
	featured, charts := featuredPlaylists, homeCharts
	t.Cleanup(func() { featuredPlaylists, homeCharts = featured, charts })
	featuredCalls, chartCalls := 0, 0
	featuredPlaylists = func() ([]netease.Playlist, error) { featuredCalls++; return nil, netease.ErrNotLoggedIn }
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return nil, netease.ErrOffline }
	a.loadHome()
	for a.homeLoading {
		refreshPost(t, posts)
	}
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return []netease.Playlist{{ID: 1}}, nil }
	a.recoverLoads()
	for a.homeLoading {
		refreshPost(t, posts)
	}
	if featuredCalls != 1 || chartCalls != 2 || a.needsRecovery() || len(a.homeFailures) == 0 {
		t.Fatal("recovery repeated a permanent module or discarded its failure")
	}
	featuredPlaylists = func() ([]netease.Playlist, error) { featuredCalls++; return []netease.Playlist{{ID: 2}}, nil }
	a.retryLoads(true)
	for a.homeLoading {
		refreshPost(t, posts)
	}
	if featuredCalls != 2 || chartCalls != 2 || len(a.homeFailures) > 0 {
		t.Fatal("manual retry lost a failed module or retried healthy data")
	}
}

// Retrying an artist profile must not implicitly retry a permanently failed songs tab.
func TestRecoveryDoesNotCascadeIntoPermanentArtistTab(t *testing.T) {
	a, posts := offlineFixture(t)
	profile, latest := artistDetail, artistLatestSongs
	t.Cleanup(func() { artistDetail, artistLatestSongs = profile, latest })
	artistDetail = func(int64) (*netease.ArtistProfile, error) { return &netease.ArtistProfile{}, nil }
	calls := 0
	artistLatestSongs = func(int64, int) ([]netease.Track, error) { calls++; return nil, netease.ErrNotLoggedIn }
	a.route = route{page: pageArtist, id: 9}
	a.artistTab = artistLatest
	d := a.artist(9)
	d.err, d.failure = "profile failed", netease.ErrOffline
	d.latestErr, d.latestFailure = "songs refused", netease.ErrNotLoggedIn
	a.recoverLoads()
	refreshPost(t, posts)
	if calls != 0 || d.latestLoading || d.profile == nil || a.needsRecovery() {
		t.Fatal("profile recovery retried permanently refused songs")
	}
}

// Manual refresh must retry visible permanent artwork failures without enabling automatic retry or touching hidden covers.
func TestManualArtworkRefreshIncludesPermanentFailures(t *testing.T) {
	a, _ := offlineFixture(t)
	a.art.add("visible", &artwork{failed: true})
	a.art.add("hidden", &artwork{failed: true})
	a.art.beginFrame()
	a.art.get("visible", false)
	if a.needsRecovery() {
		t.Fatal("permanent image started automatic recovery")
	}
	manual := a.failedLoads(true)
	a.recoverLoads()
	kept := a.art.art["visible"] != nil
	a.retryLoads(true)
	if !manual || !kept || a.art.art["visible"] != nil || a.art.art["hidden"] == nil {
		t.Fatal("manual artwork retry lost visibility or failure policy")
	}
}

// Requests on a hidden artist tab must not preserve retry state for the visible page.
func TestRecoveryIgnoresHiddenArtistLoads(t *testing.T) {
	a, _ := offlineFixture(t)
	a.route = route{page: pageArtist, id: 9}
	d := a.artist(9)
	d.profile = &netease.ArtistProfile{}
	d.latestLoading = true
	a.recovery.active = true
	a.recovery.attempts = 3
	a.recoveryFrame(true)
	if a.recovery.attempts != 0 {
		t.Fatal("hidden request kept visible recovery busy")
	}
	a.artistTab = artistLatest
	a.recovery.attempts = 2
	a.recoveryFrame(true)
	if a.recovery.attempts != 2 {
		t.Fatal("visible request lost retry state")
	}
}

// A partial home failure must not refetch healthy modules every time the window regains focus.
func TestHomeFailureDoesNotReloadHealthyModulesOnFocus(t *testing.T) {
	a, posts := offlineFixture(t)
	a.profile = nil
	featured, charts := featuredPlaylists, homeCharts
	t.Cleanup(func() { featuredPlaylists, homeCharts = featured, charts })
	chartCalls := 0
	featuredPlaylists = func() ([]netease.Playlist, error) { return nil, netease.ErrNotLoggedIn }
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return []netease.Playlist{{ID: 1}}, nil }
	a.loadHome()
	for a.homeLoading {
		refreshPost(t, posts)
	}
	// Avoid the daily boundary while keeping a missing attempt stale.
	if !a.homeAt.IsZero() {
		a.homeAt = time.Now().Add(time.Hour)
	}
	a.freshenHome()
	for a.homeLoading {
		refreshPost(t, posts)
	}
	if chartCalls != 1 {
		t.Fatal("focus reloaded healthy data after a permanent failure", chartCalls)
	}
}
