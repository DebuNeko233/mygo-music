//go:build windows

package mixvol

import (
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	// The calls below address COM methods by their position in an interface's
	// vtable, which IUnknown's three methods begin on every interface.
	slotQueryInterface = 0
	slotRelease        = 2

	slotDefaultEndpoint   = 4  // IMMDeviceEnumerator::GetDefaultAudioEndpoint
	slotActivate          = 3  // IMMDevice::Activate
	slotSessionEnumerator = 5  // IAudioSessionManager2::GetSessionEnumerator
	slotSessionCount      = 3  // IAudioSessionEnumerator::GetCount
	slotSession           = 4  // IAudioSessionEnumerator::GetSession
	slotProcessID         = 14 // IAudioSessionControl2::GetProcessId
	slotSetVolume         = 3  // ISimpleAudioVolume::SetMasterVolume
	slotGetVolume         = 4  // ISimpleAudioVolume::GetMasterVolume

	// clsctxAll (CLSCTX_ALL) lets a device activate whichever server it
	// registered for the interfaces asked of it.
	clsctxAll = 23

	// eRender and eConsole ask for the device that plays the sound the user
	// hears, in the role Windows gives ordinary programs.
	eRender  = 0
	eConsole = 0

	// coinitMultithreaded (COINIT_MULTITHREADED) asks for the apartment whose
	// objects may be called from any thread of it, which keeps the end of a
	// call from mattering to the apartment. The link stays on one thread
	// regardless, so that no call is made from a thread the objects did not
	// come from.
	coinitMultithreaded = 0

	// pollInterval is how often the mixer is read. Windows announces a stream
	// starting and ending but not a volume changing, so the volume a user drags
	// in the mixer is found by looking again.
	pollInterval = 500 * time.Millisecond

	// volumeEpsilon is how far a volume read back may sit from the one last
	// written or reported before it counts as someone else's change. The mixer
	// keeps volumes as float32, so a volume this link wrote reads back a hair
	// off the value it wrote.
	volumeEpsilon = 1e-3

	// closeTimeout bounds how long Close waits for the goroutine that holds the
	// mixer's objects: a mixer that has stopped answering must not hold up the
	// app's shutdown.
	closeTimeout = time.Second
)

// A guid is a COM GUID laid out as memory holds one: Data1 as a little-endian
// uint32, Data2 and Data3 as little-endian 16-bit values, then Data4's eight
// bytes in the order they are written.
type guid [16]byte

var (
	// CLSID_MMDeviceEnumerator, the object that hands out audio devices.
	clsidMMDeviceEnumerator = guid{
		0x95, 0x03, 0xDE, 0xBC, 0x2F, 0xE5, 0x7C, 0x46,
		0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E,
	}
	// IID_IMMDeviceEnumerator.
	iidMMDeviceEnumerator = guid{
		0xD2, 0x64, 0x56, 0xA9, 0x14, 0x96, 0x35, 0x4F,
		0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6,
	}
	// IID_IAudioSessionManager2, which a device is activated into to reach the
	// streams that play through it.
	iidAudioSessionManager2 = guid{
		0xA0, 0x99, 0xAA, 0x77, 0xD6, 0x1B, 0x4F, 0x48,
		0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F,
	}
	// IID_IAudioSessionControl2, which names the process a stream belongs to.
	iidAudioSessionControl2 = guid{
		0x88, 0xFF, 0xB7, 0xBF, 0x39, 0x72, 0xC9, 0x4F,
		0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D,
	}
	// IID_ISimpleAudioVolume, the volume the mixer keeps for one stream.
	iidSimpleAudioVolume = guid{
		0x98, 0x54, 0xCE, 0x87, 0xD6, 0x68, 0xE5, 0x44,
		0x92, 0x15, 0x6D, 0xA4, 0x7E, 0xF8, 0x83, 0xD8,
	}
)

var (
	ole32 = syscall.NewLazyDLL("ole32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

// A winLink follows the volume Windows' volume mixer keeps for this app's
// stream.
type winLink struct {
	changed func(State)

	// cmds carries the volumes the app sets to the goroutine that owns the
	// mixer's objects, which is the only goroutine they may be called from.
	cmds chan volumeCommand

	quit chan struct{}
	done chan struct{}
	stop sync.Once

	// The fields below belong to the goroutine run starts.
	mixer    *session // the mixer's stream for this app, while it holds one
	known    float64  // the volume last reported to the app or put in the mixer
	wanted   *float64 // a volume the app set before it had a stream
	reported bool     // whether the app has been told anything yet
	attached bool     // whether the app was last told the mixer holds a volume
}

// A volumeCommand is a volume the app set, and done is closed once the mixer
// holds it.
type volumeCommand struct {
	volume float64
	done   chan struct{}
}

// A session is the stream the mixer holds for this app, and with it the volume
// the mixer keeps for that stream.
type session struct {
	volume unsafe.Pointer // ISimpleAudioVolume
}

// openLink links the app's volume to the one Windows' volume mixer keeps for
// the stream it plays, which is the volume the mixer shows under the app's
// name.
func openLink(changed func(State)) link {
	l := &winLink{
		changed: changed,
		cmds:    make(chan volumeCommand),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go l.run()
	return l
}

// setVolume puts v in the mixer and waits until the mixer holds it, since the
// app raises its own gain to 1.0 as soon as this returns and a volume that
// reached the mixer only afterwards would let the full volume through for a
// moment.
func (l *winLink) setVolume(v float64) {
	cmd := volumeCommand{volume: v, done: make(chan struct{})}
	select {
	case l.cmds <- cmd:
	case <-l.done:
		return
	}
	select {
	case <-cmd.done:
	case <-l.done:
	}
}

// close stops following the mixer, waiting for the goroutine that holds the
// mixer's objects to let go of them so that changed is not called after this
// returns.
func (l *winLink) close() {
	l.stop.Do(func() {
		close(l.quit)
		select {
		case <-l.done:
		case <-time.After(closeTimeout):
		}
	})
}

// run owns the mixer: WASAPI's objects belong to the thread that made them, so
// this goroutine locks itself to one thread and makes every call from there,
// including the calls SetVolume asks for.
func (l *winLink) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(l.done)

	if hr := coInitialize(); hr < 0 {
		// Without COM there is no mixer to follow, and the app's own gain
		// carries the volume as it does on a system without one.
		l.report(State{Attached: false})
		l.serve()
		return
	}
	defer coUninitialize()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// The app may already be playing when it opens the link, so the first look
	// into the mixer happens now rather than a poll from now.
	l.poll()
	for {
		select {
		case <-l.quit:
			// Nothing is announced on the way out: Close promises that
			// changed is not called once it returns.
			if l.mixer != nil {
				l.mixer.release()
			}
			return
		case cmd := <-l.cmds:
			l.put(cmd.volume)
			close(cmd.done)
		case <-ticker.C:
			l.poll()
		}
	}
}

// serve answers the app's volumes while there is no mixer to put them in, so
// that a SetVolume is never left waiting.
func (l *winLink) serve() {
	for {
		select {
		case <-l.quit:
			return
		case cmd := <-l.cmds:
			close(cmd.done)
		}
	}
}

// put sets the app's volume in the mixer. A volume the app sets before it has a
// stream waits for the stream the mixer comes to hold.
func (l *winLink) put(v float64) {
	if l.mixer == nil {
		l.wanted = &v
		return
	}
	if hr := l.mixer.set(v); hr < 0 {
		// The stream ended or the device changed under the link, so the volume
		// is kept for the stream the mixer holds next.
		l.wanted = &v
		l.detach()
		return
	}
	l.known = v
}

// poll looks at the mixer once: it finds the app's stream when the link holds
// none, and reads the volume back from the stream it follows, so that a volume
// the user drags in the mixer reaches the app.
func (l *winLink) poll() {
	if l.mixer == nil {
		s := findSession()
		if s == nil {
			// An app that has played nothing has no stream, so the mixer holds
			// no volume of its own for it.
			l.announce(State{Attached: false})
			return
		}
		l.mixer = s
		if l.wanted != nil {
			v := *l.wanted
			if hr := s.set(v); hr < 0 {
				l.detach()
				return
			}
			l.wanted = nil
			l.known = v
			l.announce(State{Volume: v, Attached: true})
			return
		}
		v, hr := s.get()
		if hr < 0 {
			l.detach()
			return
		}
		l.known = v
		l.announce(State{Volume: v, Attached: true})
		return
	}
	v, hr := l.mixer.get()
	if hr < 0 {
		l.detach()
		return
	}
	// A volume this link wrote itself reads back as it wrote it, so only a
	// volume that moved since is the user's and worth reporting.
	if math.Abs(v-l.known) > volumeEpsilon {
		l.known = v
		l.announce(State{Volume: v, Attached: true})
	}
}

// detach lets go of the stream the link followed, which the mixer no longer
// holds, and hands the volume back to the app's own gain until a stream appears
// again.
func (l *winLink) detach() {
	l.mixer.release()
	l.mixer = nil
	l.announce(State{Attached: false})
}

// announce tells the app what the mixer holds. It speaks up again only when the
// state changes, so that an app that has played nothing is not told the same
// thing every half second.
func (l *winLink) announce(s State) {
	if l.reported && !l.attached && !s.Attached {
		return
	}
	l.reported = true
	l.attached = s.Attached
	l.report(s)
}

// report hands state to the app, which asked to hear it when it opened the
// link.
func (l *winLink) report(s State) {
	if l.changed != nil {
		l.changed(s)
	}
}

// set puts v in the mixer.
func (s *session) set(v float64) int32 {
	return call(s.volume, slotSetVolume, uintptr(math.Float32bits(float32(v))), 0)
}

// get reads the volume the mixer holds for the stream.
func (s *session) get() (float64, int32) {
	var v float32
	hr := call(s.volume, slotGetVolume, uintptr(unsafe.Pointer(&v)))
	return float64(v), hr
}

// release lets go of the stream. The mixer keeps the stream itself alive for as
// long as it plays, so nothing else is needed to follow it again later.
func (s *session) release() {
	release(s.volume)
}

// findSession asks the mixer for this process's stream. A program that has
// played nothing has none, which is why the app's volume can only be read once
// it plays.
func findSession() *session {
	enumerator := newDeviceEnumerator()
	if enumerator == nil {
		return nil
	}
	defer release(enumerator)

	var device unsafe.Pointer
	if call(enumerator, slotDefaultEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&device))) < 0 || device == nil {
		return nil
	}
	defer release(device)

	var manager unsafe.Pointer
	if call(device, slotActivate, uintptr(unsafe.Pointer(&iidAudioSessionManager2)), clsctxAll, 0, uintptr(unsafe.Pointer(&manager))) < 0 || manager == nil {
		return nil
	}
	defer release(manager)

	var sessions unsafe.Pointer
	if call(manager, slotSessionEnumerator, uintptr(unsafe.Pointer(&sessions))) < 0 || sessions == nil {
		return nil
	}
	defer release(sessions)

	var count int32
	if call(sessions, slotSessionCount, uintptr(unsafe.Pointer(&count))) < 0 {
		return nil
	}
	pid := uint32(os.Getpid())
	for i := int32(0); i < count; i++ {
		var control unsafe.Pointer
		if call(sessions, slotSession, uintptr(i), uintptr(unsafe.Pointer(&control))) < 0 || control == nil {
			continue
		}
		volume := volumeOf(control, pid)
		release(control)
		if volume != nil {
			return &session{volume: volume}
		}
	}
	return nil
}

// volumeOf returns the volume of the session control when that session belongs
// to pid, and nil for the sessions other programs and the system's own sounds
// play in.
func volumeOf(control unsafe.Pointer, pid uint32) unsafe.Pointer {
	control2 := queryInterface(control, &iidAudioSessionControl2)
	if control2 == nil {
		return nil
	}
	defer release(control2)
	var owner uint32
	if call(control2, slotProcessID, uintptr(unsafe.Pointer(&owner))) < 0 || owner != pid {
		return nil
	}
	return queryInterface(control2, &iidSimpleAudioVolume)
}

// newDeviceEnumerator makes the object that finds the devices sound leaves the
// machine through.
func newDeviceEnumerator() unsafe.Pointer {
	var enumerator unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if int32(hr) < 0 {
		return nil
	}
	return enumerator
}

// coInitialize joins the multithreaded apartment on the calling thread, which
// the link's goroutine has locked for as long as it lives.
func coInitialize() int32 {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	return int32(hr)
}

// coUninitialize leaves the apartment the calling thread joined.
func coUninitialize() {
	procCoUninitialize.Call()
}

// call invokes the method in the given vtable slot of obj, COM passing the
// object's own pointer ahead of the method's arguments. The HRESULT it returns
// is negative on failure.
func call(obj unsafe.Pointer, slot uintptr, args ...uintptr) int32 {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(obj))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return int32(r)
}

// queryInterface asks obj for the interface with the given id, which is how COM
// hands out the other interfaces an object implements. A nil result is a
// failure and leaves nothing to release.
func queryInterface(obj unsafe.Pointer, iid *guid) unsafe.Pointer {
	var out unsafe.Pointer
	if call(obj, slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) < 0 {
		return nil
	}
	return out
}

// release gives up one reference to a COM object.
func release(obj unsafe.Pointer) {
	if obj != nil {
		call(obj, slotRelease)
	}
}
