//go:build linux

package mixvol

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const (
	// commandTimeout bounds every call to a mixer command. A mixer whose daemon
	// has gone away answers nothing at all, and the app must not stay stuck
	// behind it while it waits.
	commandTimeout = 2 * time.Second

	// pollInterval is how often the mixer is read for a volume the app did not
	// set itself, which is as quickly as somebody moving the system's slider
	// expects the app to follow.
	pollInterval = time.Second

	// reportThreshold is how far the mixer's volume has to move from the one
	// the app was last told before it is told again, since the tools round what
	// they read and write and rounding is not somebody moving the slider.
	reportThreshold = 0.01
)

// A mixerBackend is a pair of command line tools that can drive the system's
// mixer, staying on this side of cgo and of a new dependency.
type mixerBackend interface {
	// find names the stream this process plays. The system keeps a volume per
	// stream, so this only finds anything once the app plays.
	find(ctx context.Context) (id int, ok bool)
	// read is the volume the mixer holds for the stream, an amplitude from 0
	// to 1.
	read(ctx context.Context, id int) (volume float64, ok bool)
	// write sets the volume of the stream and reports whether the mixer has it.
	write(ctx context.Context, id int, volume float64) bool
}

// A pactlBackend drives PulseAudio, or the PulseAudio layer that PipeWire
// serves, through pactl.
type pactlBackend struct{}

func (pactlBackend) find(ctx context.Context) (int, bool) {
	inputs, ok := pactlList(ctx)
	if !ok {
		return 0, false
	}
	input, ok := pactlSinkInputFor(inputs, os.Getpid())
	return input.Index, ok
}

func (pactlBackend) read(ctx context.Context, id int) (float64, bool) {
	inputs, ok := pactlList(ctx)
	if !ok {
		return 0, false
	}
	input, ok := pactlSinkInputByIndex(inputs, id)
	return input.Volume, ok
}

func (pactlBackend) write(ctx context.Context, id int, volume float64) bool {
	percent := strconv.Itoa(percentOf(volume)) + "%"
	return run(ctx, "pactl", "set-sink-input-volume", strconv.Itoa(id), percent)
}

// pactlList is every sink input of the session, since pactl keeps no way to
// read one of them by name. The output holds the app's stream only while it
// plays, which is what the caller looks for.
func pactlList(ctx context.Context) ([]pactlSinkInput, bool) {
	out, ok := output(ctx, "pactl", "--format=json", "list", "sink-inputs")
	if !ok {
		return nil, false
	}
	return pactlSinkInputs(out), true
}

// A pipewireBackend drives PipeWire through its own tools: pw-dump names the
// streams and wpctl reads and sets their volumes.
type pipewireBackend struct{}

func (pipewireBackend) find(ctx context.Context) (int, bool) {
	out, ok := output(ctx, "pw-dump")
	if !ok {
		return 0, false
	}
	return pwDumpStreamID(out, os.Getpid())
}

func (pipewireBackend) read(ctx context.Context, id int) (float64, bool) {
	out, ok := output(ctx, "wpctl", "get-volume", strconv.Itoa(id))
	if !ok {
		return 0, false
	}
	return wpctlVolume(out)
}

func (pipewireBackend) write(ctx context.Context, id int, volume float64) bool {
	return run(ctx, "wpctl", "set-volume", strconv.Itoa(id), strconv.FormatFloat(volume, 'f', 2, 64))
}

// output runs a mixer command and reads what it prints. A tool that cannot be
// run, a non-zero exit and a timeout all mean the same thing to the app: it
// knows no stream, and keeps applying its own volume.
func output(ctx context.Context, name string, args ...string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

// run runs a mixer command that only acts, waiting for it to finish so that the
// caller knows the mixer holds the volume once it returns.
func run(ctx context.Context, name string, args ...string) bool {
	_, ok := output(ctx, name, args...)
	return ok
}

// A linuxLink is the app's volume in this Linux's mixer: it writes the volume
// the app sets into the stream the app plays, and reports back the volume the
// system is set to, whoever set it.
type linuxLink struct {
	backend mixerBackend
	changed func(State)

	// ctx ends every command still running, and the poll below, at close.
	ctx    context.Context
	cancel context.CancelFunc

	// writeMu keeps two writes from being in the mixer at once, which could
	// leave it holding either of them, and both the poll and the app write.
	writeMu sync.Mutex

	// mu guards the fields below, which the app's own goroutine and the poll
	// share. It is never held while a command runs, so a slow mixer does not
	// hold up the app.
	mu        sync.Mutex
	id        int     // the stream being followed, valid while attached
	attached  bool    // whether the app knows of a stream at all
	wanted    float64 // the volume the app last set
	wantedSet bool    // whether the app has set a volume at all
	pending   bool    // whether wanted still has to reach the stream
	told      State   // what the app was last told
	toldSet   bool    // whether the app was told anything yet
	closed    bool

	stop chan struct{}
	done chan struct{}
}

// setVolume writes the app's volume into the mixer, waiting until the mixer has
// it: the app raises its own gain as soon as this returns, and applying the
// volume twice would make a step of the slider sound like a jump to one end.
func (l *linuxLink) setVolume(volume float64) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	id, attached := l.id, l.attached
	if !attached {
		// The app sets its volume before it plays as well as after, and the
		// system has no stream to write to until it does, so the volume waits
		// for the poll below to find one.
		l.wanted, l.wantedSet, l.pending = volume, true, true
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	l.writeMu.Lock()
	written := l.backend.write(l.ctx, id, volume)
	l.writeMu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.wanted, l.wantedSet, l.pending = volume, true, !written
	if written {
		// Reading this volume back is not news, so it is what the app knows now.
		l.told, l.toldSet = State{Volume: volume, Attached: true}, true
		return
	}
	// The stream the app played has gone, or the mixer refused the write. The
	// next poll looks for the stream again and tells the app that the mixer no
	// longer holds a volume for it.
	l.id, l.attached = 0, false
}

// close stops following the mixer, ending any command still running, and is
// safe to call more than once. Nothing is reported to the app once it returns.
func (l *linuxLink) close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	l.mu.Unlock()

	close(l.stop)
	l.cancel()
	<-l.done
}

// follow reads the mixer once a second until close, which the ticker makes
// rather than a sleep per turn, so that a slow read does not stretch the beat.
func (l *linuxLink) follow() {
	defer close(l.done)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		l.poll()
		select {
		case <-l.stop:
			return
		case <-ticker.C:
		}
	}
}

// poll reports what the mixer holds for the app right now.
func (l *linuxLink) poll() {
	l.mu.Lock()
	id, attached := l.id, l.attached
	l.mu.Unlock()

	if !attached {
		// Listing the whole mixer is the expensive half of this, and PipeWire's
		// pw-dump is the most expensive of all, so the app looks for its stream
		// only while it has none to read, which is also when one may appear.
		found, ok := l.backend.find(l.ctx)
		if !ok {
			l.report(State{})
			return
		}
		l.attach(found)
		return
	}

	volume, ok := l.backend.read(l.ctx, id)
	if !ok {
		l.forget()
		return
	}
	l.report(State{Volume: volume, Attached: true})
}

// attach starts following a stream the mixer has just named, giving it the
// volume the app asked for while it had no stream, and reports what it holds so
// that the app knows the mixer applies the volume from here on.
func (l *linuxLink) attach(id int) {
	l.writeMu.Lock()
	l.mu.Lock()
	wanted, pending := l.wanted, l.pending
	l.mu.Unlock()

	if pending && !l.backend.write(l.ctx, id, wanted) {
		// The mixer did not take the volume the app set, so the app must keep
		// applying it itself; the next poll finds the stream again or finds it
		// gone.
		l.writeMu.Unlock()
		return
	}
	volume, read := l.backend.read(l.ctx, id)
	l.writeMu.Unlock()
	if !read {
		return
	}

	l.mu.Lock()
	l.id, l.attached, l.pending = id, true, false
	l.mu.Unlock()
	l.report(State{Volume: volume, Attached: true})
}

// forget drops the stream being followed, so that the next poll looks for the
// app's stream again, and tells the app that the mixer no longer holds a volume
// for it. The volume the app is at waits for the stream the app plays next,
// since a fresh stream starts at whatever the mixer calls full volume and the
// app has stopped applying its own.
func (l *linuxLink) forget() {
	l.mu.Lock()
	l.id, l.attached = 0, false
	l.pending = l.wantedSet
	l.mu.Unlock()
	l.report(State{})
}

// report tells the app what the mixer holds, unless it was told that already.
// Every report comes from the poll goroutine, so the app hears one change at a
// time and never a change it made itself.
func (l *linuxLink) report(state State) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	// A volume that has moved by less than the threshold is the tools rounding
	// the same setting, not a change the app has to apply.
	known := l.toldSet && l.told.Attached == state.Attached &&
		(!state.Attached || math.Abs(l.told.Volume-state.Volume) <= reportThreshold)
	if !known {
		l.told, l.toldSet = state, true
	}
	l.mu.Unlock()

	if !known {
		l.changed(state)
	}
}

// openLink links the app to the mixer this Linux has, PulseAudio through pactl
// or PipeWire through its own tools, which is also how PipeWire answers when
// it serves PulseAudio's interface but pactl is not installed. Where neither is
// installed, nothing is linked and the app keeps applying its own volume, as it
// does on a system that keeps no volume of its own for a program.
func openLink(changed func(State)) link {
	var backend mixerBackend
	switch {
	case hasCommand("pactl"):
		backend = pactlBackend{}
	case hasCommand("pw-dump") && hasCommand("wpctl"):
		backend = pipewireBackend{}
	default:
		changed(State{Attached: false})
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	l := &linuxLink{
		backend: backend,
		changed: changed,
		ctx:     ctx,
		cancel:  cancel,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go l.follow()
	return l
}

// hasCommand reports whether a mixer tool is on the path, since a mixer the app
// cannot run is one this system does not have, as far as the app is concerned.
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
