/*
验证恢复调度区分永久失败、隐藏内容和健康模块，并保护已有快照。
*/

package app

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	diskcache "netease-music-mygo/internal/cache/disk"
	artcache "netease-music-mygo/internal/desktop/artwork"
	"netease-music-mygo/internal/netease"
	artistvm "netease-music-mygo/internal/viewmodel/artist"
)

// Permanent page failures used to keep retrying on focus and network recovery.
func TestPermanentFailureStopsRetry(t *testing.T) {
	a, posts := offlineFixture(t)
	old := albumDetail
	t.Cleanup(func() { albumDetail = old })
	calls := 0
	albumDetail = func(int64) (*netease.AlbumProfile, error) { calls++; return nil, netease.ErrNotLoggedIn }
	a.nav.Current = route{Page: pageAlbum, ID: 9}
	a.loadAlbum(9)
	refreshPost(t, posts)
	a.recoveryFrame(true)
	a.networkChanged(mygo.NetworkStatus{Online: false})
	a.networkChanged(mygo.NetworkStatus{Online: true})
	if a.needsRecovery() || a.recovery.timer != nil || a.albums.Pages.Get(9).Loading || calls != 1 {
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
	if calls != 2 || a.albums.Pages.Get(9).Album == nil || a.albums.Pages.Get(9).Error != "" {
		t.Fatal("explicit refresh could not recover a permanent failure")
	}
}

// Recovery must not fetch a failed hidden artist tab until that tab becomes visible.
func TestRecoverySkipsHiddenArtistTab(t *testing.T) {
	a, _ := offlineFixture(t)
	a.nav.Current = route{Page: pageArtist, ID: 9}
	d := a.artists.Page(9)
	d.Profile = &netease.ArtistProfile{}
	d.LatestError, d.LatestFailure = "failed", netease.ErrOffline
	if a.needsRecovery() {
		t.Fatal("hidden tab keeps background recovery alive")
	}
	a.recoverLoads()
	if d.LatestLoading {
		t.Fatal("recovery fetched a hidden tab")
	}
	a.artists.Tab = artistvm.ArtistLatest
	if !a.needsRecovery() {
		t.Fatal("visible temporary failure cannot recover")
	}
}

// Failed serialization used to be silent and could replace a valid snapshot with empty data.
func TestCacheSerializationFailurePreservesSnapshot(t *testing.T) {
	a, _ := offlineFixture(t)
	req := a.cacheRequest("test")
	req.Store.Put(req.Scope, req.Key, []byte("kept"), req.Epoch)
	var output bytes.Buffer
	writer := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(writer) })
	bad := struct {
		Private string
		Invalid chan int
	}{"session-secret", make(chan int)}
	b := diskcache.Encode(bad)
	req.Keep(b)
	write := req.Store.Prepare(req.Scope, req.Key, req.Epoch)
	write(b) // Prepared library snapshots use this separate path.
	if b != nil || !strings.Contains(output.String(), "cache: cannot encode") || strings.Contains(output.String(), bad.Private) {
		t.Fatal("encoding failure was silent or leaked the payload")
	}
	got, _ := req.Store.Read(req.Scope, req.Key)
	if string(got) != "kept" {
		t.Fatal("bad encoder replaced the old snapshot")
	}
}

// Retrying a temporary home failure must not repeatedly request another module's permanent failure.
func TestRecoverySeparatesHomeFailures(t *testing.T) {
	a, posts := offlineFixture(t)
	a.account.Profile = nil
	featured, charts := featuredPlaylists, homeCharts
	t.Cleanup(func() { featuredPlaylists, homeCharts = featured, charts })
	featuredCalls, chartCalls := 0, 0
	featuredPlaylists = func() ([]netease.Playlist, error) { featuredCalls++; return nil, netease.ErrNotLoggedIn }
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return nil, netease.ErrOffline }
	a.loadHome()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return []netease.Playlist{{ID: 1}}, nil }
	a.recoverLoads()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if featuredCalls != 1 || chartCalls != 2 || a.needsRecovery() || len(a.home.Failures) == 0 {
		t.Fatal("recovery repeated a permanent module or discarded its failure")
	}
	featuredPlaylists = func() ([]netease.Playlist, error) { featuredCalls++; return []netease.Playlist{{ID: 2}}, nil }
	a.retryLoads(true)
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if featuredCalls != 2 || chartCalls != 2 || len(a.home.Failures) > 0 {
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
	a.nav.Current = route{Page: pageArtist, ID: 9}
	a.artists.Tab = artistvm.ArtistLatest
	d := a.artists.Page(9)
	d.Error, d.Failure = "profile failed", netease.ErrOffline
	d.LatestError, d.LatestFailure = "songs refused", netease.ErrNotLoggedIn
	a.recoverLoads()
	refreshPost(t, posts)
	if calls != 0 || d.LatestLoading || d.Profile == nil || a.needsRecovery() {
		t.Fatal("profile recovery retried permanently refused songs")
	}
}

// Manual refresh must retry visible permanent artwork failures without enabling automatic retry or touching hidden covers.
func TestManualArtworkRefreshIncludesPermanentFailures(t *testing.T) {
	a, _ := offlineFixture(t)
	a.art.Put("visible", &artcache.Image{Failed: true})
	a.art.Put("hidden", &artcache.Image{Failed: true})
	a.art.BeginFrame()
	a.art.Get("visible", false)
	if a.needsRecovery() {
		t.Fatal("permanent image started automatic recovery")
	}
	manual := a.failedLoads(true)
	a.recoverLoads()
	kept := a.art.Peek("visible") != nil
	a.retryLoads(true)
	if !manual || !kept || a.art.Peek("visible") != nil || a.art.Peek("hidden") == nil {
		t.Fatal("manual artwork retry lost visibility or failure policy")
	}
}

// Requests on a hidden artist tab must not preserve retry state for the visible page.
func TestRecoveryIgnoresHiddenArtistLoads(t *testing.T) {
	a, _ := offlineFixture(t)
	a.nav.Current = route{Page: pageArtist, ID: 9}
	d := a.artists.Page(9)
	d.Profile = &netease.ArtistProfile{}
	d.LatestLoading = true
	a.recovery.active = true
	a.recovery.attempts = 3
	a.recoveryFrame(true)
	if a.recovery.attempts != 0 {
		t.Fatal("hidden request kept visible recovery busy")
	}
	a.artists.Tab = artistvm.ArtistLatest
	a.recovery.attempts = 2
	a.recoveryFrame(true)
	if a.recovery.attempts != 2 {
		t.Fatal("visible request lost retry state")
	}
}

// A partial home failure must not refetch healthy modules every time the window regains focus.
func TestHomeFailureDoesNotReloadHealthyModulesOnFocus(t *testing.T) {
	a, posts := offlineFixture(t)
	a.account.Profile = nil
	featured, charts := featuredPlaylists, homeCharts
	t.Cleanup(func() { featuredPlaylists, homeCharts = featured, charts })
	chartCalls := 0
	featuredPlaylists = func() ([]netease.Playlist, error) { return nil, netease.ErrNotLoggedIn }
	homeCharts = func() ([]netease.Playlist, error) { chartCalls++; return []netease.Playlist{{ID: 1}}, nil }
	a.loadHome()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	// Avoid the daily boundary while keeping a missing attempt stale.
	if !a.home.At.IsZero() {
		a.home.At = time.Now().Add(time.Hour)
	}
	a.freshenHome()
	for a.home.Loading {
		refreshPost(t, posts)
	}
	if chartCalls != 1 {
		t.Fatal("focus reloaded healthy data after a permanent failure", chartCalls)
	}
}
