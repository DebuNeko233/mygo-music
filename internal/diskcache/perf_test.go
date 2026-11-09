package diskcache

import (
	"fmt"
	"testing"
)

// Metadata updates should not rescan every image file while the cache has headroom.
func BenchmarkIndexedMetadataUpdate(b *testing.B) {
	for _, files := range []int{0, 512} {
		b.Run(fmt.Sprint(files), func(b *testing.B) {
			s := NewIndexed(b.TempDir(), 64<<20, "requests")
			b.Cleanup(s.Close)
			for i := range files {
				s.Put("content", fmt.Sprint(i), make([]byte, 128), 0)
			}
			value := []byte(`{"Content":"shared","Expires":"2030-01-01T00:00:00Z"}`)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.Put("requests", "visible-cover", value, 0)
			}
		})
	}
}
