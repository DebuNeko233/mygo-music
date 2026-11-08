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

// A late result from one headless app must not enter a newer app's dispatcher.
func TestPostDispatchBelongsToApp(t *testing.T) {
	a, b := newTestApp(), newTestApp()
	first := testPosts(a)
	release, done := make(chan struct{}), make(chan struct{})
	seen := ""
	go func() { <-release; a.post(func() { seen += "a" }); close(done) }()
	second := testPosts(b)
	b.post(func() { seen += "b" })
	close(release)
	<-done
	refreshPost(t, second)
	if seen != "b" {
		t.Fatal("old app entered the new dispatcher", seen)
	}
	refreshPost(t, first)
	if seen != "ba" {
		t.Fatal("old app lost its own result", seen)
	}
}
