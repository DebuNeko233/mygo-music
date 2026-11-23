package app

import (
	"testing"

	"netease-music-mygo/internal/netease"
	artistvm "netease-music-mygo/internal/viewmodel/artist"
)

// Visible artist events recover temporary failures automatically and permanent failures only on explicit refresh.
func TestArtistEventsRecovery(t *testing.T) {
	for _, temporary := range []bool{true, false} {
		name := "permanent"
		failure := netease.ErrNotLoggedIn
		if temporary {
			name, failure = "temporary", netease.ErrOffline
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp()
			posts := testPosts(a)
			a.nav.Current = route{Page: pageArtist, ID: 9}
			a.artists.Tab = artistvm.ArtistEvents
			d := a.artist(9)
			d.Profile = &netease.ArtistProfile{ID: 9, AccountID: 42}
			old := accountEvents
			t.Cleanup(func() { accountEvents = old })
			accountEvents = func(int64, int, int64) ([]netease.Event, int64, bool, error) {
				return nil, 0, false, failure
			}
			a.loadEvents(9)
			refreshPost(t, posts)
			if a.needsRecovery() != temporary || !a.failedLoads(true) || d.EventsPage.Failure != failure {
				t.Fatal("artist events lost their retry policy or underlying failure")
			}
			a.artists.Tab = artistvm.ArtistAlbums
			if a.needsRecovery() || a.failedLoads(true) {
				t.Fatal("a hidden events tab kept recovery active")
			}
			a.artists.Tab = artistvm.ArtistEvents
			requested := make(chan struct{}, 2)
			release := make(chan struct{})
			defer close(release)
			accountEvents = func(uid int64, _ int, before int64) ([]netease.Event, int64, bool, error) {
				if uid != 42 || before != -1 {
					t.Error("recovery changed the events account or initial cursor")
				}
				requested <- struct{}{}
				<-release
				return []netease.Event{{ID: 1}}, 100, true, nil
			}
			a.recoverLoads()
			if !temporary {
				if d.EventsPage.Loading {
					t.Fatal("automatic recovery retried a permanent failure")
				}
				a.retryLoads(true)
			}
			wait(t, requested, "artist events recovery")
			if !a.recoveryBusy() || d.EventsPage.Failure != nil {
				t.Fatal("an active events request lost its busy state or kept a stale failure")
			}
			a.retryLoads(true)
			select {
			case <-requested:
				t.Fatal("refresh started an overlapping events request")
			default:
			}
			release <- struct{}{}
			refreshPost(t, posts)
			if a.failedLoads(true) || a.recoveryBusy() || !d.EventsPage.Loaded || len(d.Events) != 1 {
				t.Fatal("recovery failed to publish events and clear its error")
			}
		})
	}
}
