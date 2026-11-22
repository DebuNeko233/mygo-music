package app

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
	m := cloudViewModel{cloudListing: cloudListing{tracks: []netease.Track{{ID: 1}}, count: 1}, loaded: true}
	type reply struct {
		page *netease.CloudPage
		err  error
	}
	calls, replies := make(chan int, 4), make(chan reply, 4)
	posts := make(chan func() bool, 4)
	s := cloudServices{
		fetch: func(ctx context.Context, _, offset int) (*netease.CloudPage, error) {
			calls <- offset
			select {
			case r := <-replies:
				return r.page, r.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		post: func(fn func() bool) { posts <- fn },
		now:  func() time.Time { return time.Unix(1000, 0) },
	}
	t.Cleanup(m.stop)
	m.refresh(s)
	if got := wait(t, calls, "refresh first page"); got != 0 {
		t.Fatal(got)
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 2}}, Count: 2, HasMore: true, NextOffset: 1}}
	wait(t, posts, "first page")()
	if got := wait(t, calls, "refresh next page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{err: errors.New("temporary")}
	wait(t, posts, "failed page")()
	if m.tracks[0].ID != 1 || len(m.tracks) != 1 || m.pending == nil || m.failure == nil {
		t.Fatal("failed refresh replaced the published snapshot")
	}
	m.load(cloudNextPage, s)
	if got := wait(t, calls, "retry same page"); got != 1 {
		t.Fatal(got)
	}
	replies <- reply{page: &netease.CloudPage{Tracks: []netease.Track{{ID: 3}}, Count: 2, NextOffset: 2}}
	wait(t, posts, "completed refresh")()
	if len(m.tracks) != 2 || m.tracks[0].ID != 2 || m.tracks[1].ID != 3 || m.pending != nil || m.loading || m.failure != nil || m.at != s.now() {
		t.Fatal("refresh did not publish its complete replacement")
	}
}

// Resetting a view model at the same address cannot let the retired request
// overwrite a new account's request, even when the old fetch ignores cancellation.
func TestCloudViewModelRejectsRetiredRequest(t *testing.T) {
	var m cloudViewModel
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	closeOld, closeNew := sync.OnceFunc(func() { close(oldRelease) }), sync.OnceFunc(func() { close(newRelease) })
	t.Cleanup(closeOld)
	t.Cleanup(closeNew)
	started := make(chan context.Context, 2)
	posts := make(chan func() bool, 2)
	s := cloudServices{
		fetch: func(ctx context.Context, _, _ int) (*netease.CloudPage, error) {
			started <- ctx
			<-oldRelease
			return &netease.CloudPage{Tracks: []netease.Track{{ID: 1}}, Count: 1, NextOffset: 1}, nil
		},
		post: func(fn func() bool) { posts <- fn },
		now:  func() time.Time { return time.Unix(1000, 0) },
	}
	m.load(cloudNextPage, s)
	old := wait(t, started, "old request")
	m.stop()
	if old.Err() != context.Canceled {
		t.Fatal("retired request was not canceled")
	}
	m = cloudViewModel{}
	s.fetch = func(ctx context.Context, _, _ int) (*netease.CloudPage, error) {
		started <- ctx
		<-newRelease
		return &netease.CloudPage{Tracks: []netease.Track{{ID: 2}}, Count: 1, NextOffset: 1}, nil
	}
	m.load(cloudNextPage, s)
	wait(t, started, "new request")
	closeOld()
	if wait(t, posts, "old completion")() || !m.loading || len(m.tracks) != 0 {
		t.Fatal("retired completion changed the new request")
	}
	closeNew()
	if !wait(t, posts, "new completion")() || m.loading || len(m.tracks) != 1 || m.tracks[0].ID != 2 {
		t.Fatal("new request was not published")
	}
}

type modelDownloadCall struct {
	options download.Options
	reply   chan error
}

func downloadModelFixture(t *testing.T) (*downloadViewModel, downloadServices, chan modelDownloadCall, chan func() bool) {
	t.Helper()
	m := &downloadViewModel{}
	calls := make(chan modelDownloadCall, 4)
	posts := make(chan func() bool, 8)
	s := downloadServices{
		save: func(ctx context.Context, _ netease.Track, opts download.Options) (download.Result, error) {
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
		post:  func(fn func() bool) { posts <- fn },
		cache: func() *player.Cache { return nil },
	}
	t.Cleanup(m.cancelAll)
	return m, s, calls, posts
}

// Retrying preserves the row identity but retires the old execution's progress
// callback; stale callbacks must not change the new attempt or request a redraw.
func TestDownloadViewModelRetryRejectsOldProgress(t *testing.T) {
	m, s, calls, posts := downloadModelFixture(t)
	m.enqueue([]netease.Track{{ID: 1}}, download.Options{Directory: "/tmp", NameFormat: download.NameTitle}, s)
	first := wait(t, calls, "first attempt")
	row := m.jobs[0]
	first.reply <- errors.New("temporary")
	wait(t, posts, "first completion")()
	m.retry(row, s)
	second := wait(t, calls, "retry attempt")
	first.options.Progress("retired", 90, 100)
	if wait(t, posts, "old progress")() || row.received != 0 || row.finished || m.jobs[0] != row {
		t.Fatal("retired progress changed the retried row")
	}
	second.options.Progress("current", 20, 100)
	if !wait(t, posts, "current progress")() || row.received != 20 || row.status != "current" {
		t.Fatal("current attempt lost its progress")
	}
	second.reply <- nil
	wait(t, posts, "retry completion")()
	if !row.finished || row.err != nil || row.result.Path == "" || m.active != nil {
		t.Fatal("retry did not finish")
	}
}

// Pending jobs keep their output snapshot while each execution obtains the
// current cache from the model service, including after a cache-folder change.
func TestDownloadViewModelRequestAndCacheOwnership(t *testing.T) {
	m, s, calls, posts := downloadModelFixture(t)
	firstCache, secondCache := player.NewCache(t.TempDir(), 1<<20), player.NewCache(t.TempDir(), 1<<20)
	current := firstCache
	s.cache = func() *player.Cache { return current }
	opts := download.Options{Directory: "original", Quality: "lossless", Sidecar: true, NameFormat: download.NameTitle}
	m.enqueue([]netease.Track{{ID: 1}, {ID: 2}}, opts, s)
	first := wait(t, calls, "first cache")
	if first.options.Cache != firstCache {
		t.Fatal("first execution did not use its cache")
	}
	current = secondCache
	opts.Directory, opts.Sidecar = "changed", false
	first.reply <- nil
	wait(t, posts, "next queued job")()
	second := wait(t, calls, "second cache")
	if second.options.Cache != secondCache || second.options.Directory != "original" || !second.options.Sidecar || second.options.NameFormat != download.NameTitle {
		t.Fatal("queued request changed or kept a retired cache")
	}
	second.reply <- nil
	wait(t, posts, "second completion")()
	if m.active != nil || len(m.jobs) != 2 || !m.jobs[1].finished {
		t.Fatal("queue did not finish")
	}
}
