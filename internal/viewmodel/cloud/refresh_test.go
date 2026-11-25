package cloud

import (
	"context"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Refresh replaces pagination identities as well as rows, so old IDs remain eligible and removed songs do not survive.
func TestCloudRefreshRebuildsDeduplication(t *testing.T) {
	var m Cloud
	posts := make(chan func() bool, 4)
	services := CloudServices{Post: func(fn func() bool) { posts <- fn }, Now: func() time.Time { return time.Unix(1000, 0) }}
	t.Cleanup(m.Stop)
	track := func(id int64, name string) netease.Track { return netease.Track{ID: id, Name: name, Cloud: true} }
	refresh := false
	services.Fetch = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		if offset == 0 {
			name := "旧名称"
			if refresh {
				name = "新名称"
			}
			return &netease.CloudPage{Tracks: []netease.Track{track(1, name), track(1, "重复")}, Count: 2, NextOffset: 2, HasMore: true}, nil
		}
		id := int64(2)
		if refresh {
			id = 3
		}
		return &netease.CloudPage{Tracks: []netease.Track{track(1, "跨页重复"), track(id, "尾页"), {}}, Count: 2, NextOffset: 5}, nil
	}
	m.Load(CloudNextPage, services)
	testutil.Wait(t, posts, "cloud page")()
	testutil.Wait(t, posts, "cloud page")()
	if len(m.Tracks) != 2 || m.Tracks[1].ID != 2 {
		t.Fatalf("initial pagination: %+v", m.Tracks)
	}
	previous := m.Tracks
	refresh = true
	m.Refresh(services)
	testutil.Wait(t, posts, "cloud page")()
	testutil.Wait(t, posts, "cloud page")()
	if len(m.Tracks) != 2 || m.Tracks[0].ID != 1 || m.Tracks[0].Name != "新名称" || m.Tracks[1].ID != 3 {
		t.Fatalf("refreshed pagination: %+v", m.Tracks)
	}
	if previous[0].Name != "旧名称" || previous[1].ID != 2 {
		t.Fatal("refresh changed the previously published list")
	}
}
