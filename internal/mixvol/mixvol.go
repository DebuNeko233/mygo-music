// Package mixvol links the app's own volume to the system's mixer, so that
// the volume it shows is the volume the system shows for it: the slider the
// Windows volume mixer gives this app, PulseAudio's or PipeWire's volume for
// the stream that plays. Where the system keeps no such volume, or keeps it
// out of reach, nothing is linked and the app's own gain carries the volume,
// as it did before.
package mixvol

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
	link link
}

// Open links the app to the system's mixer, calling changed on a goroutine of
// its own with what the mixer holds, at once and whenever it changes, until
// Close. The volume the mixer holds for the app starts out known only once
// the app plays, since the system keeps a volume for a stream, not for a
// program.
func Open(changed func(State)) *Mixer {
	return &Mixer{link: openLink(changed)}
}

// SetVolume sets the app's volume in the mixer, an amplitude from 0 to 1,
// waiting until the mixer has it, so that the app may then stop applying the
// volume itself without what plays jumping.
func (m *Mixer) SetVolume(v float64) {
	if m == nil || m.link == nil {
		return
	}
	m.link.setVolume(min(max(v, 0), 1))
}

// Close stops following the mixer.
func (m *Mixer) Close() {
	if m == nil || m.link == nil {
		return
	}
	m.link.close()
}

// A link follows the system's volume for the app, per system.
type link interface {
	// setVolume sets the app's volume in the mixer, waiting until it holds it.
	setVolume(v float64)
	close()
}
