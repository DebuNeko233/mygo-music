//go:build windows

package mixvol

import (
	"testing"
	"time"
)

// TestSetVolumeGivesUpOnAMixerThatNeverAnswers checks that a volume set while
// the mixer answers nothing comes back: set from the goroutine that draws, a
// wait with no end to it froze the window.
func TestSetVolumeGivesUpOnAMixerThatNeverAnswers(t *testing.T) {
	l := &winLink{cmds: make(chan volumeCommand), done: make(chan struct{})}
	// Counted from before the call, since a volume still waiting for the
	// mixer once it returns has to have waited for no longer than allotted.
	start := time.Now()
	done := make(chan struct{})
	go func() {
		l.setVolume(0.5)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(4 * setTimeout):
		t.Fatal("setting a volume waited on a mixer that never answered")
	}
	if waited := time.Since(start); waited > 2*setTimeout {
		t.Errorf("waited %v, for a wait of at most %v", waited, setTimeout)
	}
}
