/*
测量网盘与下载视图和入队开销，验证功能视图空闲时无需轮询。
*/

package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
	downloadsvm "netease-music-mygo/internal/viewmodel/downloads"
)

// Both sides of the MVVM comparison use these same data and view entry points.
func featurePerfApp(cloud, jobs int) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.preferences.Section = "download"
	a.account.Profile = &netease.Profile{UserID: 1}
	a.nav.Current = route{Page: pageCloud}
	a.cloud.Loaded = true
	a.cloud.Count = cloud
	for i := range cloud {
		a.cloud.Tracks = append(a.cloud.Tracks, cloudTrack(int64(i+1), fmt.Sprintf("Song %05d", i)))
	}
	for i := range jobs {
		a.downloads.Jobs = append(a.downloads.Jobs, &downloadsvm.DownloadJob{
			Track:    netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("Download %05d", i)},
			Finished: true, Result: download.Result{Path: "/tmp/song.flac", Format: "flac"},
		})
	}
	return a
}

// Scrolling a virtual cloud list should scale with visible rows, not the full library.
func BenchmarkCloudViewFrame(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := featurePerfApp(count, 0)
			tt := ui.NewTester(func(c *ui.Context) {
				a.pal = paletteFor(c.Theme())
				a.views.cloud.Build(c)
			}, 1000, 700)
			tt.Frame()
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				a.tracks.List.ScrollTo((i*17)%count+1, ui.Center)
				tt.Frame()
				i++
			}
		})
	}
}

// Download settings frames include both the empty explanation and a populated queue.
func BenchmarkDownloadViewFrame(b *testing.B) {
	for _, count := range []int{0, 100} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := featurePerfApp(0, count)
			tt := ui.NewTester(a.views.settings.Build, 780, 640)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Batch enqueue measures filtering, request snapshots and deduplication while
// one worker is held; it excludes network, tagging and disk throughput.
func BenchmarkDownloadEnqueue(b *testing.B) {
	a := featurePerfApp(0, 0)
	a.settings.DownloadDirectory = b.TempDir()
	posts := testPosts(a)
	old := saveDownload
	started := make(chan struct{}, 1)
	saveDownload = func(ctx context.Context, _ netease.Track, _ download.Options) (download.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		return download.Result{}, ctx.Err()
	}
	a.enqueueDownloads([]netease.Track{{ID: 1}})
	<-started
	b.Cleanup(func() {
		a.quitting = true
		a.downloads.CancelAll()
		select {
		case post := <-posts:
			post()
		case <-time.After(5 * time.Second):
			b.Error("download did not stop")
		}
		saveDownload = old
	})
	tracks := make([]netease.Track, 1000)
	for i := range tracks {
		tracks[i].ID = int64(i + 2)
	}
	b.ReportAllocs()
	for b.Loop() {
		a.downloads.Jobs = a.downloads.Jobs[:1]
		a.enqueueDownloads(tracks)
	}
}

// Loaded cloud and download views remain asleep when no data or input changes.
func TestFeatureViewsRestWithoutPolling(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		t.Run(fmt.Sprint(cloud), func(t *testing.T) {
			a := featurePerfApp(1000, 100)
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) {
				builds++
				if cloud {
					a.pal = paletteFor(c.Theme())
					a.views.cloud.Build(c)
				} else {
					a.views.settings.Build(c)
				}
			}, 1000, 700)
			tt.Frame()
			tt.Advance(time.Second)
			before := builds
			tt.Advance(2 * time.Second)
			if builds != before {
				t.Fatalf("idle view requested %d extra builds", builds-before)
			}
		})
	}
}
