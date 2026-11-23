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
	"netease-music-mygo/internal/viewmodel"
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
			a.cloud = viewmodel.Cloud{CloudListing: viewmodel.CloudListing{Tracks: first, Count: 200, Offset: 100, More: true}, Loaded: true}
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
				a.loadCloud(viewmodel.CloudNextPage)
				if got := wait(t, started, "retry cloud page"); got != 100 {
					t.Fatalf("retry offset %d", got)
				}
			}
			results <- reply{page: &netease.CloudPage{Tracks: later, Count: 200, NextOffset: 200}}
			// Search suggestions may also post while the UI is being exercised.
			// Wait for the cloud result, rather than assuming it is the next callback.
			for a.cloud.Loading() {
				refreshPost(t, posts)
			}
			tt.Frame()
			if len(a.cloud.Tracks) != 200 || a.cloud.Loading() || a.cloud.More {
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
	a.library.Refresh.Now = func() time.Time { return now }
	a.cloud = viewmodel.Cloud{CloudListing: viewmodel.CloudListing{Tracks: original, Count: 3, Offset: 3}, Loaded: true, At: oldTime}
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
	if !a.cloud.Refreshing() || len(a.cloud.Tracks) != 3 || a.cloud.Tracks[0].Name != "Old one" || a.cloud.Count != 3 || a.cloud.At != oldTime {
		t.Fatal("first refresh page replaced the published snapshot")
	}
	refreshPost(t, posts)
	if a.cloud.Failure == nil || len(a.trackSuggestions("Old two")) != 1 || len(a.cloud.Tracks) != 3 {
		t.Fatal("refresh failure discarded known tracks")
	}
	a.load(a.route)
	if a.cloud.Loading() {
		t.Fatal("reopening after failure restarted refresh instead of retaining its retry offset")
	}
	a.loadCloud(viewmodel.CloudNextPage)
	refreshPost(t, posts)
	for _, want := range []int{0, 1, 1} {
		if got := wait(t, calls, "refresh request"); got != want {
			t.Fatalf("offset %d, want %d", got, want)
		}
	}
	if a.cloud.Refreshing() || a.cloud.Failure != nil || a.cloud.More || a.cloud.Loading() || a.cloud.Count != 2 || a.cloud.At != now || len(a.cloud.Tracks) != 2 || a.cloud.Tracks[0].Name != "New one" || a.cloud.Tracks[1].ID != 4 {
		t.Fatalf("refresh not committed: %+v", a.cloud)
	}
	if len(a.trackSuggestions("Old two")) != 0 || len(a.trackSuggestions("New four")) != 1 {
		t.Fatal("search did not switch to the committed snapshot")
	}
	if len(a.pl.Queue()) != 3 || a.pl.Queue()[0].Name != "Old one" {
		t.Fatal("refresh edited an existing playback queue")
	}
}
