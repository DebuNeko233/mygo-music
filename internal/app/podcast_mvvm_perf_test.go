/*
测量播客视图和已加载数据访问开销，并验证空闲帧预算。
*/

package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/netease"
)

func podcastPerfApp(detail bool) *app {
	a := catalogApp()
	a.nav.Current = route{Page: pagePodcasts}
	if detail {
		a.nav.Current = route{Page: pagePodcast, ID: 5}
		d := a.podcasts.Pages.Get(5)
		d.Programs = make([]netease.Program, 300)
		for i := range d.Programs {
			d.Programs[i] = netease.Program{ID: int64(i + 1), Name: fmt.Sprintf("节目 %03d", i+1), Duration: 600000}
			d.Programs[i].MainSong.ID = int64(i + 100)
		}
	}
	return a
}

// Podcast home and long episode lists keep their published data while rebuilding views.
func BenchmarkPodcastMVVMFrame(b *testing.B) {
	for _, detail := range []bool{false, true} {
		b.Run(fmt.Sprint(detail), func(b *testing.B) {
			a := podcastPerfApp(detail)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// Reopening loaded podcast pages must not start requests or allocate service closures.
func BenchmarkPodcastMVVMLoaded(b *testing.B) {
	a := podcastPerfApp(true)
	b.ReportAllocs()
	for b.Loop() {
		a.loadPodcast(5)
		a.loadPodcastTab()
	}
}

// An idle podcast home or detail sleeps without scheduling extra UI builds.
func TestPodcastMVVMIdleFrameBudget(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(fmt.Sprint(detail), func(t *testing.T) {
			a := podcastPerfApp(detail)
			builds := 0
			tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			before := builds
			tt.Advance(2 * time.Second)
			if builds != before {
				t.Fatalf("idle podcast requested %d extra builds", builds-before)
			}
		})
	}
}
