package app

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// Concurrent consumers share one tag read and replacing the audio refreshes the cover.
func TestLocalEmbeddedArtworkSharesTagRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache embeddedArtworkCache
		var reads atomic.Int32
		source := embeddedArtworkKey{path: "song.flac", stamp: 1}
		finish := make(chan struct{})
		read := func(string) []byte {
			reads.Add(1)
			<-finish
			return []byte("cover")
		}
		results := make(chan []byte, 3)
		for range 3 {
			go func() { results <- cache.get(source, read) }()
		}
		synctest.Wait()
		close(finish)
		for range 3 {
			if got := <-results; string(got) != "cover" {
				t.Fatalf("read %q", got)
			}
		}
		if n := reads.Load(); n != 1 {
			t.Fatalf("read tags %d times for the same file", n)
		}
		source.stamp++
		if got := cache.get(source, func(string) []byte { return []byte("replacement") }); string(got) != "replacement" {
			t.Fatalf("read stale cover %q", got)
		}
	})
}
