// Package mixvol links the app's own volume to the system's mixer, so that
// the volume it shows is the volume the system shows for it: the slider the
// Windows volume mixer gives this app, PulseAudio's or PipeWire's volume for
// the stream that plays. Where the system keeps no such volume, or keeps it
// out of reach, nothing is linked and the app's own gain carries the volume,
// as it did before.
package mixvol

import "sync"

// A State is what the system's mixer holds for the app.
type State struct {
	// Volume is the app's volume there, an amplitude from 0 to 1.
	Volume float64
	// Attached reports whether the system holds a volume for the app at all:
	// while it does, the mixer applies the volume, and the app must not apply
	// it again in its own gain.
	Attached bool
}

// A Mixer is the app's own volume in the system's mixer.
type Mixer struct {
	link      link
	queue     chan float64
	quit      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// Open links the app to the system's mixer, calling changed on a goroutine of
// its own with what the mixer holds, at once and whenever it changes, until
// Close. The volume the mixer holds for the app starts out known only once
// the app plays, since the system keeps a volume for a stream, not for a
// program.
func Open(changed func(State)) *Mixer {
	return newMixer(openLink(changed))
}

// newMixer serializes writes off the caller's thread and coalesces stale ones.
func newMixer(link link) *Mixer {
	m := &Mixer{link: link}
	if link != nil {
		m.queue, m.quit, m.done = make(chan float64, 1), make(chan struct{}), make(chan struct{})
		go m.writeVolumes()
	}
	return m
}

// SetVolume queues the app's volume in the system mixer without waiting for a
// device command. While a write is stuck, only the latest volume is retained.
func (m *Mixer) SetVolume(v float64) {
	if m == nil || m.link == nil {
		return
	}
	v = min(max(v, 0), 1)
	select {
	case m.queue <- v:
		return
	case <-m.done:
		return
	default:
	}
	select {
	case <-m.queue:
	case <-m.done:
		return
	default:
	}
	select {
	case m.queue <- v:
	case <-m.done:
	}
}

// writeVolumes owns all link writes. Taking the newest queued value first
// avoids an obsolete intermediate volume after a slow device recovers.
func (m *Mixer) writeVolumes() {
	defer close(m.done)
	for {
		select {
		case v := <-m.queue:
			select {
			case v = <-m.queue:
			default:
			}
			m.link.setVolume(v)
		case <-m.quit:
			return
		}
	}
}

// Close stops following the mixer and rejects any later writes.
func (m *Mixer) Close() {
	if m == nil || m.link == nil {
		return
	}
	m.closeOnce.Do(func() {
		close(m.quit)
		<-m.done
		m.link.close()
	})
}

// A link follows the system's volume for the app, per system.
type link interface {
	// setVolume sets the app's volume in the mixer, waiting until it holds it.
	setVolume(v float64)
	close()
}
