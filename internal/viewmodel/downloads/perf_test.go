/*
基准测量重复下载入队的去重与分配开销，不启动网络任务。
*/

package downloads

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
