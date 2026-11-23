package downloads

import (
	"context"
	"errors"
	"testing"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

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
	first := testutil.Wait(t, calls, "first attempt")
	row := m.Jobs[0]
	first.reply <- errors.New("temporary")
	testutil.Wait(t, posts, "first completion")()
	m.Retry(row, s)
	second := testutil.Wait(t, calls, "retry attempt")
	first.Options.Progress("retired", 90, 100)
	if testutil.Wait(t, posts, "old progress")() || row.Received != 0 || row.Finished || m.Jobs[0] != row {
		t.Fatal("retired progress changed the retried row")
	}
	second.Options.Progress("current", 20, 100)
	if !testutil.Wait(t, posts, "current progress")() || row.Received != 20 || row.Status != "current" {
		t.Fatal("current attempt lost its progress")
	}
	second.reply <- nil
	testutil.Wait(t, posts, "retry completion")()
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
	first := testutil.Wait(t, calls, "first cache")
	if count := m.Enqueue([]netease.Track{{ID: 1}, {ID: 2}}, opts, s); count != 0 || len(m.Jobs) != 2 {
		t.Fatal("pending requests were not deduplicated")
	}
	if first.Options.Cache != firstCache {
		t.Fatal("first execution did not use its cache")
	}
	current = secondCache
	opts.Directory, opts.Sidecar = "changed", false
	first.reply <- nil
	testutil.Wait(t, posts, "next queued job")()
	second := testutil.Wait(t, calls, "second cache")
	if second.Options.Cache != secondCache || second.Options.Directory != "original" || !second.Options.Sidecar || second.Options.NameFormat != download.NameTitle {
		t.Fatal("queued request changed or kept a retired cache")
	}
	second.reply <- nil
	testutil.Wait(t, posts, "second completion")()
	if m.active != nil || len(m.Jobs) != 2 || !m.Jobs[1].Finished {
		t.Fatal("queue did not finish")
	}
}
