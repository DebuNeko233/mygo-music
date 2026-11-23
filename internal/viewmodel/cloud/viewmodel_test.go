package cloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// A refresh publishes only its complete replacement, retaining the original
// searchable snapshot while later pages fail and retry, without a UI instance.
func TestCloudViewModelRefresh(t *testing.T) {
	m := Cloud{CloudListing: CloudListing{Tracks: []netease.Track{{ID: 1}}, Count: 1}, Loaded: true}
	type reply struct {
		page *netease.CloudPage
		err  error
	}
	calls, replies := make(chan int, 4), make(chan reply, 4)
	posts := make(chan func() bool, 4)
	s := CloudServices{
		Fetch: func(ctx context.Context, _, offset int) (*netease.CloudPage, error) {
			calls <- offset
			select {
			case r := <-replies:
				return r.page, r.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		Post: func(fn func() bool) { posts <- fn },
		Now:  func() time.Time { return time.Unix(1000, 0) },
	}
	t.Cleanup(m.Stop)
	m.Refresh(s)
	if got := testutil.Wait(t, calls, "refresh first page"); got != 0 {
		t.Fatal(got)
	}
	run := m.request
	m.Refresh(s)
	if !m.Loading() || m.request != run {
		t.Fatal("refresh replaced an in-flight request")
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 2}}, Count: 2, HasMore: true, NextOffset: 1}}
	testutil.Wait(t, posts, "first page")()
	if got := testutil.Wait(t, calls, "refresh next page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{err: errors.New("temporary")}
	testutil.Wait(t, posts, "failed page")()
	if m.Tracks[0].ID != 1 || len(m.Tracks) != 1 || m.pending == nil || m.Failure == nil {
		t.Fatal("failed refresh replaced the published snapshot")
	}
	m.Load(CloudNextPage, s)
	if got := testutil.Wait(t, calls, "retry same page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 3}}, Count: 2, NextOffset: 2}}
	testutil.Wait(t, posts, "completed refresh")()
	if len(m.Tracks) != 2 || m.Tracks[0].ID != 2 || m.Tracks[1].ID != 3 || m.pending != nil || m.Loading() || m.Failure != nil || m.At != s.Now() {
		t.Fatal("refresh did not publish its complete replacement")
	}
}

// Resetting a view model at the same address cannot let the retired request
// overwrite a new account's request, even when the old fetch ignores cancellation.
func TestCloudViewModelRejectsRetiredRequest(t *testing.T) {
	var m Cloud
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	closeOld, closeNew := sync.OnceFunc(func() { close(oldRelease) }), sync.OnceFunc(func() { close(newRelease) })
	t.Cleanup(closeOld)
	t.Cleanup(closeNew)
	started := make(chan context.Context, 2)
	posts := make(chan func() bool, 2)
	s := CloudServices{
		Fetch: func(ctx context.Context, _, _ int) (*netease.CloudPage, error) {
			started <- ctx
			<-oldRelease
			return &netease.CloudPage{Tracks: []netease.Track{{ID: 1}}, Count: 1, NextOffset: 1}, nil
		},
		Post: func(fn func() bool) { posts <- fn },
		Now:  func() time.Time { return time.Unix(1000, 0) },
	}
	m.Load(CloudNextPage, s)
	old := testutil.Wait(t, started, "old request")
	m.Stop()
	if old.Err() != context.Canceled {
		t.Fatal("retired request was not canceled")
	}
	m = Cloud{}
	s.Fetch = func(ctx context.Context, _, _ int) (*netease.CloudPage, error) {
		started <- ctx
		<-newRelease
		return &netease.CloudPage{Tracks: []netease.Track{{ID: 2}}, Count: 1, NextOffset: 1}, nil
	}
	m.Load(CloudNextPage, s)
	testutil.Wait(t, started, "new request")
	closeOld()
	if testutil.Wait(t, posts, "old completion")() || !m.Loading() || len(m.Tracks) != 0 {
		t.Fatal("retired completion changed the new request")
	}
	closeNew()
	if !testutil.Wait(t, posts, "new completion")() || m.Loading() || len(m.Tracks) != 1 || m.Tracks[0].ID != 2 {
		t.Fatal("new request was not published")
	}
}
