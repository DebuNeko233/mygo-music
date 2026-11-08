package app

import (
	"sync"
	"testing"
)

// Concurrent loads finishing before window creation used to race and lose queued callbacks.
func TestEarlyPostsAreNotLost(t *testing.T) {
	a := newTestApp()
	const count = 64
	var workers sync.WaitGroup
	start := make(chan struct{})
	seen := make([]int, count)
	for i := range count {
		workers.Go(func() {
			<-start
			a.post(func() { seen[i]++ })
		})
	}
	close(start)
	workers.Wait()
	// Draining and callbacks remain on the UI thread; draining twice must
	// neither replay an old result nor deadlock if a callback posts again.
	a.post(func() { a.post(func() { seen[0]++ }) })
	a.flushKept()
	a.flushKept()
	for i, got := range seen {
		want := 1
		if i == 0 {
			want++
		}
		if got != want {
			t.Fatalf("callback %d ran %d times, want %d", i, got, want)
		}
	}
}
