package app

import (
	"context"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A row or sidebar match chosen during loading gets the remaining cloud songs without restarting playback.
func TestCloudQueueCompletesWhileLoading(t *testing.T) {
	for _, action := range []string{"row", "search", "add", "remove", "replace", "retry"} {
		t.Run(action, func(t *testing.T) {
			a, posts := cloudFixture(t)
			first, later := make([]netease.Track, 100), make([]netease.Track, 100)
			for i := range first {
				first[i] = cloudTrack(int64(i+1), fmt.Sprintf("First %03d", i+1))
				later[i] = cloudTrack(int64(i+101), fmt.Sprintf("Later %03d", i+101))
			}
			a.cloud = cloudViewModel{cloudListing: cloudListing{tracks: first, count: 200, offset: 100, more: true}, loaded: true}
			type reply struct {
				page *netease.CloudPage
				err  error
			}
			results := make(chan reply, 2)
			started := make(chan int, 2)
			defer close(results)
			cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
				started <- offset
				r := <-results
				return r.page, r.err
			}
			a.pl.SuspendCache() // Keep real queue operations, without audio/network work.
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if got := wait(t, started, "next cloud page"); got != 100 {
				t.Fatalf("offset %d", got)
			}
			if action == "search" {
				typePlaylistSearch(t, tt, "First 001")
				if err := tt.Click("网盘：First 001"); err != nil {
					t.Fatal(err)
				}
			} else if err := tt.Click("First 001"); err != nil {
				t.Fatal(err)
			}
			a.pl.Hold(17 * time.Second)
			current := a.pl.Current()
			switch action {
			case "add":
				a.pl.Enqueue(cloudTrack(999, "Manual"), true)
			case "remove":
				a.pl.RemoveQueueEntry(99)
			case "replace":
				a.pl.Cue([]netease.Track{cloudTrack(999, "Other queue")})
			case "retry":
				results <- reply{err: io.ErrUnexpectedEOF}
				refreshPost(t, posts)
				a.pl.Seek(23 * time.Second)
				a.loadCloud(cloudNextPage)
				if got := wait(t, started, "retry cloud page"); got != 100 {
					t.Fatalf("retry offset %d", got)
				}
			}
			results <- reply{page: &netease.CloudPage{Tracks: later, Count: 200, NextOffset: 200}}
			// Search suggestions may also post while the UI is being exercised.
			// Wait for the cloud result, rather than assuming it is the next callback.
			for a.cloud.loading() {
				refreshPost(t, posts)
			}
			tt.Frame()
			if len(a.cloud.tracks) != 200 || a.cloud.loading() || a.cloud.more {
				t.Fatal("library did not complete")
			}
			want := 200
			switch action {
			case "add":
				want = 101
			case "remove":
				want = 99
			case "replace":
				want = 1
			}
			if len(a.pl.Queue()) != want {
				t.Fatalf("queue %d, want %d", len(a.pl.Queue()), want)
			}
			if action == "replace" {
				if a.pl.Current().ID != 999 {
					t.Fatal("replaced queue changed")
				}
				return
			}
			position := 17 * time.Second
			if action == "retry" {
				position = 23 * time.Second
			}
			if a.pl.Current() != current || !a.pl.Playing() || a.pl.Position() != position {
				t.Fatal("completion restarted or moved playback")
			}
			if action == "add" && a.pl.QueuePosition(999) != 1 {
				t.Fatal("manual next song moved")
			}
			if action == "remove" && a.pl.QueuePosition(100) != -1 {
				t.Fatal("removed song was re-added")
			}
		})
	}
}

// A refresh that fails after its first page keeps the complete searchable snapshot and retries the failed offset before publishing once.
func TestCloudRefreshPublishesOnlyWhenComplete(t *testing.T) {
	a, posts := cloudFixture(t)
	original := []netease.Track{cloudTrack(1, "Old one"), cloudTrack(2, "Old two"), cloudTrack(3, "Old three")}
	oldTime := time.Unix(1000, 0)
	now := oldTime.Add(24 * time.Hour)
	a.listRefresh.now = func() time.Time { return now }
	a.cloud = cloudViewModel{cloudListing: cloudListing{tracks: original, count: 3, offset: 3}, loaded: true, at: oldTime}
	a.pl.Cue(slices.Clone(original))
	calls := make(chan int, 4)
	attempt := 0
	cloudSongs = func(_ context.Context, _, offset int) (*netease.CloudPage, error) {
		calls <- offset
		if offset == 0 {
			return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(1, "New one")}, Count: 2, NextOffset: 1, HasMore: true}, nil
		}
		attempt++
		if attempt == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &netease.CloudPage{Tracks: []netease.Track{cloudTrack(4, "New four")}, Count: 2, NextOffset: 2}, nil
	}
	a.refreshCloud()
	refreshPost(t, posts)
	if a.cloud.pending == nil || len(a.cloud.tracks) != 3 || a.cloud.tracks[0].Name != "Old one" || a.cloud.count != 3 || a.cloud.at != oldTime {
		t.Fatal("first refresh page replaced the published snapshot")
	}
	refreshPost(t, posts)
	if a.cloud.failure == nil || len(a.trackSuggestions("Old two")) != 1 || len(a.cloud.tracks) != 3 {
		t.Fatal("refresh failure discarded known tracks")
	}
	a.load(a.route)
	if a.cloud.loading() {
		t.Fatal("reopening after failure restarted refresh instead of retaining its retry offset")
	}
	a.loadCloud(cloudNextPage)
	refreshPost(t, posts)
	for _, want := range []int{0, 1, 1} {
		if got := wait(t, calls, "refresh request"); got != want {
			t.Fatalf("offset %d, want %d", got, want)
		}
	}
	if a.cloud.pending != nil || a.cloud.failure != nil || a.cloud.more || a.cloud.loading() || a.cloud.count != 2 || a.cloud.at != now || len(a.cloud.tracks) != 2 || a.cloud.tracks[0].Name != "New one" || a.cloud.tracks[1].ID != 4 {
		t.Fatalf("refresh not committed: %+v", a.cloud)
	}
	if len(a.trackSuggestions("Old two")) != 0 || len(a.trackSuggestions("New four")) != 1 {
		t.Fatal("search did not switch to the committed snapshot")
	}
	if len(a.pl.Queue()) != 3 || a.pl.Queue()[0].Name != "Old one" {
		t.Fatal("refresh edited an existing playback queue")
	}
}
