// Package testutil shares synchronization helpers between feature tests.
package testutil

import (
	"testing"
	"time"
)

// Wait bounds a worker response without changing the model's dispatcher.
func Wait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: " + what)
		var zero T
		return zero
	}
}
