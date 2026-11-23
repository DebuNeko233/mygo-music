package viewmodel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
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
	if got := wait(t, calls, "refresh first page"); got != 0 {
		t.Fatal(got)
	}
	run := m.request
	m.Refresh(s)
	if !m.Loading() || m.request != run {
		t.Fatal("refresh replaced an in-flight request")
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 2}}, Count: 2, HasMore: true, NextOffset: 1}}
	wait(t, posts, "first page")()
	if got := wait(t, calls, "refresh next page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{err: errors.New("temporary")}
	wait(t, posts, "failed page")()
	if m.Tracks[0].ID != 1 || len(m.Tracks) != 1 || m.pending == nil || m.Failure == nil {
		t.Fatal("failed refresh replaced the published snapshot")
	}
	m.Load(CloudNextPage, s)
	if got := wait(t, calls, "retry same page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 3}}, Count: 2, NextOffset: 2}}
	wait(t, posts, "completed refresh")()
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
	old := wait(t, started, "old request")
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
	wait(t, started, "new request")
	closeOld()
	if wait(t, posts, "old completion")() || !m.Loading() || len(m.Tracks) != 0 {
		t.Fatal("retired completion changed the new request")
	}
	closeNew()
	if !wait(t, posts, "new completion")() || m.Loading() || len(m.Tracks) != 1 || m.Tracks[0].ID != 2 {
		t.Fatal("new request was not published")
	}
}

type modelDownloadCall struct {
	Options download.Options
	reply   chan error
}

func downloadModelFixture(t *testing.T) (*Downloads, DownloadServices, chan modelDownloadCall, chan func() bool) {
	t.Helper()
	m := &Downloads{}
	calls := make(chan modelDownloadCall, 4)
	posts := make(chan func() bool, 8)
	s := DownloadServices{
		Save: func(ctx context.Context, _ netease.Track, opts download.Options) (download.Result, error) {
			reply := make(chan error, 1)
			calls <- modelDownloadCall{opts, reply}
			select {
			case err := <-reply:
				if err != nil {
					return download.Result{}, err
				}
				return download.Result{Path: "/tmp/song.flac"}, nil
			case <-ctx.Done():
				return download.Result{}, ctx.Err()
			}
		},
		Post:  func(fn func() bool) { posts <- fn },
		Cache: func() *player.Cache { return nil },
	}
	t.Cleanup(m.CancelAll)
	return m, s, calls, posts
}

// Retrying preserves the row identity but retires the old execution's progress
// callback; stale callbacks must not change the new attempt or request a redraw.
func TestDownloadViewModelRetryRejectsOldProgress(t *testing.T) {
	m, s, calls, posts := downloadModelFixture(t)
	m.Enqueue([]netease.Track{{ID: 1}}, download.Options{Directory: "/tmp", NameFormat: download.NameTitle}, s)
	first := wait(t, calls, "first attempt")
	row := m.Jobs[0]
	first.reply <- errors.New("temporary")
	wait(t, posts, "first completion")()
	m.Retry(row, s)
	second := wait(t, calls, "retry attempt")
	first.Options.Progress("retired", 90, 100)
	if wait(t, posts, "old progress")() || row.Received != 0 || row.Finished || m.Jobs[0] != row {
		t.Fatal("retired progress changed the retried row")
	}
	second.Options.Progress("current", 20, 100)
	if !wait(t, posts, "current progress")() || row.Received != 20 || row.Status != "current" {
		t.Fatal("current attempt lost its progress")
	}
	second.reply <- nil
	wait(t, posts, "retry completion")()
	if !row.Finished || row.Err != nil || row.Result.Path == "" || m.active != nil {
		t.Fatal("retry did not finish")
	}
}

// Pending jobs keep their output snapshot while each execution obtains the
// current cache from the model service, including after a cache-folder change.
func TestDownloadViewModelRequestAndCacheOwnership(t *testing.T) {
	m, s, calls, posts := downloadModelFixture(t)
	firstCache, secondCache := player.NewCache(t.TempDir(), 1<<20), player.NewCache(t.TempDir(), 1<<20)
	current := firstCache
	s.Cache = func() *player.Cache { return current }
	opts := download.Options{Directory: "original", Quality: "lossless", Sidecar: true, NameFormat: download.NameTitle}
	m.Enqueue([]netease.Track{{ID: 1}, {ID: 2}}, opts, s)
	first := wait(t, calls, "first cache")
	if count := m.Enqueue([]netease.Track{{ID: 1}, {ID: 2}}, opts, s); count != 0 || len(m.Jobs) != 2 {
		t.Fatal("pending requests were not deduplicated")
	}
	if first.Options.Cache != firstCache {
		t.Fatal("first execution did not use its cache")
	}
	current = secondCache
	opts.Directory, opts.Sidecar = "changed", false
	first.reply <- nil
	wait(t, posts, "next queued job")()
	second := wait(t, calls, "second cache")
	if second.Options.Cache != secondCache || second.Options.Directory != "original" || !second.Options.Sidecar || second.Options.NameFormat != download.NameTitle {
		t.Fatal("queued request changed or kept a retired cache")
	}
	second.reply <- nil
	wait(t, posts, "second completion")()
	if m.active != nil || len(m.Jobs) != 2 || !m.Jobs[1].Finished {
		t.Fatal("queue did not finish")
	}
}

func wait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: " + what)
		var zero T
		return zero
	}
}
