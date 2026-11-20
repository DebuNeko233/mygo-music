package mixvol

import (
	"testing"
	"time"
)

type blockingLink struct {
	started chan struct{}
	release chan struct{}
}

func (l *blockingLink) setVolume(float64) {
	close(l.started)
	<-l.release
}
func (*blockingLink) close() {}

// Volume changes return while a mixer write is stuck instead of freezing their UI caller.
func TestSetVolumeDoesNotWaitForTheMixer(t *testing.T) {
	l := &blockingLink{started: make(chan struct{}), release: make(chan struct{})}
	m := newMixer(l)
	defer close(l.release)
	done := make(chan struct{})
	go func() {
		m.SetVolume(0.5)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetVolume waited for the mixer write")
	}
	<-l.started
}
