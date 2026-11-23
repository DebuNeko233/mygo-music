package viewmodel

import (
	"fmt"
	"testing"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// Re-enqueuing pending songs measures deduplication without network or worker
// activity, including the allocations of requests that are entirely rejected.
func BenchmarkDownloadDuplicateEnqueue(b *testing.B) {
	for _, count := range []int{1, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			opts := download.Options{Directory: "/tmp", Quality: "exhigh"}
			m := Downloads{}
			tracks := make([]netease.Track, count)
			for i := range tracks {
				tracks[i].ID = int64(i + 1)
				m.Jobs = append(m.Jobs, &DownloadJob{Track: tracks[i], Options: opts})
			}
			// Represent an in-flight job without starting I/O in this benchmark.
			m.active = &downloadRun{job: m.Jobs[0], cancel: func() {}}
			b.ReportAllocs()
			for b.Loop() {
				if n := m.Enqueue(tracks, opts, DownloadServices{}); n != 0 {
					b.Fatal("duplicate requests enqueued", n)
				}
			}
		})
	}
}

// Loading a large cloud library should not copy and scan all prior songs on every page.
func BenchmarkCloudMergePages(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			tracks := make([]netease.Track, count)
			for i := range tracks {
				tracks[i].ID = int64(i + 1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var d Cloud
				for start := 0; start < count; start += cloudPageSize {
					d.mergePage(tracks[start:min(start+cloudPageSize, count)], start == 0)
				}
				if len(d.Tracks) != count {
					b.Fatal("lost cloud songs")
				}
			}
		})
	}
}
