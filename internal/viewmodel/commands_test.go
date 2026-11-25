/*
验证后台任务仅在所属线程发布状态，并在下载清理发布后推进队列。
*/

package viewmodel_test

import (
	"context"
	"testing"
	"time"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	cloudvm "netease-music-mygo/internal/viewmodel/cloud"
	downloadsvm "netease-music-mygo/internal/viewmodel/downloads"
)

// A worker publishes only on the owning thread and follows the complete cloud listing automatically.
func TestCloudPublishesOnOwnerThread(t *testing.T) {
	var m cloudvm.Cloud
	type call struct {
		offset int
		reply  chan *netease.CloudPage
	}
	calls, posts := make(chan call, 2), make(chan func() bool, 2)
	now := time.Unix(1000, 0)
	s := cloudvm.CloudServices{
		Fetch: func(ctx context.Context, _, offset int) (*netease.CloudPage, error) {
			reply := make(chan *netease.CloudPage, 1)
			calls <- call{offset, reply}
			select {
			case page := <-reply:
				return page, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		Post: func(update func() bool) { posts <- update },
		Now:  func() time.Time { return now },
	}
	t.Cleanup(m.Stop)
	m.Load(cloudvm.CloudNextPage, s)
	first := receive(t, calls)
	if first.offset != 0 {
		t.Fatal("initial request did not start at zero")
	}
	first.reply <- &netease.CloudPage{Tracks: []netease.Track{{ID: 1}}, Count: 2, NextOffset: 1, HasMore: true}
	publish := receive(t, posts)
	if len(m.Tracks) != 0 || !m.Loading() || len(calls) != 0 {
		t.Fatal("worker changed presentation state or started another request before publication")
	}
	if !publish() || len(m.Tracks) != 1 || !m.Loading() {
		t.Fatal("publication did not start the next page")
	}
	second := receive(t, calls)
	if second.offset != 1 {
		t.Fatal("pagination lost the server offset")
	}
	second.reply <- &netease.CloudPage{Tracks: []netease.Track{{ID: 1}, {ID: 2}}, Count: 2, NextOffset: 2}
	if !receive(t, posts)() || m.Loading() || m.More || len(m.Tracks) != 2 || m.At != now {
		t.Fatal("complete listing was not published and deduplicated")
	}
	m.Load(cloudvm.CloudNextPage, s)
	if m.Loading() {
		t.Fatal("completed listing restarted without a refresh command")
	}
}

// Canceling an execution leaves it busy until its posted cleanup runs, then advances the queue once.
func TestDownloadsWaitForPublishedCleanup(t *testing.T) {
	var m downloadsvm.Downloads
	type call struct {
		ctx context.Context
		id  int64
	}
	calls, posts := make(chan call, 4), make(chan func() bool, 4)
	s := downloadsvm.DownloadServices{
		Save: func(ctx context.Context, tr netease.Track, _ download.Options) (download.Result, error) {
			calls <- call{ctx, tr.ID}
			<-ctx.Done()
			return download.Result{}, ctx.Err()
		},
		Post:  func(update func() bool) { posts <- update },
		Cache: func() *player.Cache { return nil },
	}
	t.Cleanup(m.CancelAll)
	if m.Enqueue([]netease.Track{{ID: 1}, {ID: 2}}, download.Options{Directory: t.TempDir()}, s) != 2 {
		t.Fatal("queue did not accept both requests")
	}
	first := receive(t, calls)
	row := m.Jobs[0]
	m.Cancel(row)
	if first.ctx.Err() != context.Canceled || !m.Busy() || row.Finished || m.CanRetry(row) {
		t.Fatal("cancellation skipped execution cleanup")
	}
	publish := receive(t, posts)
	if !m.Busy() || row.Finished || len(calls) != 0 {
		t.Fatal("worker changed state or advanced the queue before publication")
	}
	if !publish() || !row.Finished || !row.Cancelled || !m.CanRetry(row) {
		t.Fatal("cleanup did not finish the canceled row")
	}
	second := receive(t, calls)
	if first.id != 1 || second.id != 2 || !m.Busy() {
		t.Fatal("queue did not advance to exactly the next request")
	}
	m.CancelAll()
	if !receive(t, posts)() || m.Busy() || !m.Jobs[1].Finished {
		t.Fatal("queue did not settle after cancellation")
	}
	m.ClearFinished()
	if len(m.Jobs) != 0 {
		t.Fatal("finished rows remained after clearing")
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for command result")
		var zero T
		return zero
	}
}
