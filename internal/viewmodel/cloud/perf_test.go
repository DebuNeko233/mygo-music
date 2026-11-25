/*
基准测量大型网盘分页合并，检查重复扫描与复制开销。
*/

package cloud

import (
	"fmt"
	"testing"

	"netease-music-mygo/internal/netease"
)

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
