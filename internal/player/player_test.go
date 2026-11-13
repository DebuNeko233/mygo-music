package player

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/netease"
)

// Playback events follow real audio transitions: pause and seek preserve the
// session, while repeat-one closes it and starts another at zero.
func TestPlaybackEventsDistinguishPauseSeekAndRepeat(t *testing.T) {
	ch, p := posted()
	defer p.out.Close()
	p.source = PlaySource{Source: "list", ID: 42}
	var events []PlaybackEvent
	p.Playback = func(event PlaybackEvent) { events = append(events, event) }
	p.info = &netease.Stream{Bitrate: 320000, Level: "exhigh"}
	p.begin()
	p.TogglePlay()
	next(t, ch)
	p.TogglePlay()
	p.Seek(90 * time.Second)
	next(t, ch)
	if len(events) != 1 || !events[0].Started {
		t.Fatalf("pause/resume/seek produced %+v", events)
	}
	p.repeat = RepeatOne
	p.ended()
	if len(events) != 3 || events[1].End != Playthrough || !events[2].Started {
		t.Fatalf("repeat produced %+v", events)
	}
	if events[0].Source.Source != "list" || events[0].Source.ID != 42 {
		t.Fatalf("source was lost: %+v", events[0].Source)
	}
	if events[2].Bitrate != 320 || events[2].Level != "exhigh" {
		t.Fatalf("repeat started with default quality: %+v", events[2])
	}
}

// Resuming audio that Restore loaded paused starts the session it deliberately
// deferred; otherwise its entire play was absent from NetEase's reports.
func TestRestoredAudioBeginsItsSessionWhenPlayed(t *testing.T) {
	defer noSongSources(t)()
	data, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	posts := make(chan func(), 16)
	p := New(func(fn func()) { posts <- fn })
	p.Cache = NewCache(t.TempDir(), 1<<20)
	seedCache(t, p.Cache, 1, data)
	p.Cue([]netease.Track{{ID: 1, Duration: 10000}})
	p.loadSkipping(0, 0, false, 250*time.Millisecond)
	waitCachePlayer(t, p, posts)
	defer p.out.Close()
	var events []PlaybackEvent
	p.Playback = func(event PlaybackEvent) { events = append(events, event) }
	p.TogglePlay()
	if len(events) != 1 || !events[0].Started {
		t.Fatalf("resumed audio reported %+v, want one start", events)
	}
	p.TogglePlay()
	if len(events) != 1 {
		t.Fatalf("pausing and resuming created another session: %+v", events)
	}
}

// Listening time follows audible playback, not the seek position or time
// spent paused; otherwise skipping to the end invents a complete listen.
func TestPlaybackListeningTimeExcludesPausesAndSeeks(t *testing.T) {
	ch, p := posted()
	defer p.out.Close()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return clock }
	var events []PlaybackEvent
	p.Playback = func(event PlaybackEvent) { events = append(events, event) }
	p.begin()
	clock = clock.Add(40 * time.Second)
	p.TogglePlay()
	next(t, ch)
	clock = clock.Add(10 * time.Minute)
	p.TogglePlay()
	clock = clock.Add(20 * time.Second)
	p.Seek(90 * time.Second)
	clock = clock.Add(time.Second)
	next(t, ch)
	clock = clock.Add(5 * time.Second)
	p.finish(Interrupted)
	if got := events[1].Played; got != 65*time.Second {
		t.Fatalf("heard %v, want 65 seconds excluding pause and seek", got)
	}
}

// Retrying the same song keeps its session and excludes the backoff and
// subsequent loading from listening time.
func TestPlaybackRetryPreservesHeardTime(t *testing.T) {
	p := &Player{at: -1, post: func(func()) {}}
	p.Cue([]netease.Track{{ID: 1, Duration: 600000}})
	p.SuspendCache()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return clock }
	p.info = &netease.Stream{Bitrate: 128000, Level: "standard"}
	p.begin()
	clock = clock.Add(8 * time.Second)
	p.retry(0, 0, 12*time.Second, netease.ErrUnavailable)
	clock = clock.Add(time.Hour)
	p.loadSkipping(0, 0, true, 12*time.Second)
	p.info = &netease.Stream{Bitrate: 320000, Level: "exhigh"}
	p.begin()
	clock = clock.Add(4 * time.Second)
	p.finish(Interrupted)
	if p.played != 12*time.Second {
		t.Fatalf("retry accumulated %v, want 12 seconds", p.played)
	}
}

// Pausing a load and failing to restart audio both stop listening time even
// though the track remains active for a later resume.
func TestPlaybackStoppedLoadExcludesWallClock(t *testing.T) {
	for _, stop := range []string{"pause", "failure"} {
		t.Run(stop, func(t *testing.T) {
			p := &Player{at: -1, post: func(func()) {}}
			p.Cue([]netease.Track{{ID: 1, Duration: 600000}})
			clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			p.now = func() time.Time { return clock }
			p.begin()
			clock = clock.Add(3 * time.Second)
			if stop == "pause" {
				p.pause()
				p.playing = false
			} else {
				p.tries = loadTries
				p.failed(0, 0, *p.track, 0, netease.ErrUnavailable)
			}
			clock = clock.Add(time.Hour)
			p.finish(Interrupted)
			if p.played != 3*time.Second {
				t.Fatalf("stopped load accumulated %v, want 3 seconds", p.played)
			}
		})
	}
}

// Cache migration pauses the clock but not the session: reloading audio adds
// neither a second Started event nor an Interrupted event, and preserves time.
func TestCacheMigrationPreservesPlaybackSession(t *testing.T) {
	defer noSongSources(t)()
	data, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	posts := make(chan func(), 16)
	p := New(func(fn func()) { posts <- fn })
	p.Cache = NewCache(t.TempDir(), 1<<20)
	seedCache(t, p.Cache, 1, data)
	p.Cue([]netease.Track{{ID: 1, Duration: 10000}})
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return clock }
	var events []PlaybackEvent
	p.Playback = func(event PlaybackEvent) { events = append(events, event) }
	p.loadSkipping(0, 0, true, 250*time.Millisecond)
	waitCachePlayer(t, p, posts)
	defer func() {
		p.out.Close()
		p.stream.Close()
	}()
	clock = clock.Add(5 * time.Second)
	position := p.Position()
	resume := p.SuspendCache()
	if !p.Playing() {
		t.Fatal("cache migration cleared desired playback state")
	}
	clock = clock.Add(time.Hour)
	p.Cache = NewCache(t.TempDir(), 1<<20)
	seedCache(t, p.Cache, 1, data)
	resume()
	waitCachePlayer(t, p, posts)
	clock = clock.Add(2 * time.Second)
	p.finish(Interrupted)
	if len(events) != 2 || !events[0].Started || events[1].End != Interrupted {
		t.Fatalf("cache migration split the session: %+v", events)
	}
	if events[1].Played != 7*time.Second || events[1].Position < position {
		t.Fatalf("cache migration lost played time or position: %+v", events[1])
	}
}

// Radio seeds retain an existing source, radio conversions have FM context,
// and delayed feed tracks use FM without inventing an ID.
func TestRadioPlaySources(t *testing.T) {
	p := New(func(func()) {})
	p.SuspendCache()
	seed := PlaySource{Source: "list", ID: 42}
	p.PlayQueueFrom([]netease.Track{{ID: 1}}, 0, seed)
	var refill func([]netease.Track)
	p.PlayRadio("personal", []netease.Track{{ID: 1}}, func(got func([]netease.Track)) {
		refill = got
	})
	if p.source != seed {
		t.Fatalf("seed source changed to %+v", p.source)
	}
	p.awaiting = true
	p.at = len(p.order) - 1
	p.refill()
	refill([]netease.Track{{ID: 2}})
	if p.source.Source != "fm" || p.source.ID != 0 {
		t.Fatalf("feed source %+v, want FM without ID", p.source)
	}
	p.source = PlaySource{}
	p.RadioFrom("personal", nil, func(func([]netease.Track)) {})
	if p.source.Source != "fm" || p.source.ID != 0 {
		t.Fatalf("radio conversion source %+v", p.source)
	}
}

func TestPlayEmptyQueue(t *testing.T) {
	p := &Player{at: -1}
	p.PlayQueue(nil, 0) // an empty playlist's play button
	if p.Current() != nil {
		t.Error("playing from an empty queue")
	}
}

// silence is a long song of nothing.
type silence struct{}

func (silence) SampleRate() int               { return 48000 }
func (silence) Channels() int                 { return 2 }
func (silence) Len() int64                    { return 48000 * 600 }
func (silence) SetPosition(int64) error       { return nil }
func (silence) Read(b []float32) (int, error) { clear(b); return len(b), nil }

// posted collects what the player posts to the main thread, for the test
// to run there.
func posted() (chan func(), *Player) {
	ch := make(chan func(), 16)
	p := New(func(fn func()) { ch <- fn })
	p.Cue([]netease.Track{{ID: 1, Name: "一", Duration: 600000}})
	out := audio.NewPlayer(silence{})
	out.SetVolume(0)
	out.Play()
	p.Attach(out)
	p.fade.attach(out, 1)
	return ch, p
}

func next(t *testing.T, ch chan func()) {
	t.Helper()
	select {
	case fn := <-ch:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("nothing posted")
	}
}

// Pausing fades out before the music stops; playing again fades it in.
func TestPauseFades(t *testing.T) {
	ch, p := posted()
	defer p.out.Close()
	p.TogglePlay()
	if p.Playing() || !p.out.Playing() {
		t.Fatal("paused at once, or not at all")
	}
	next(t, ch)
	if p.out.Playing() {
		t.Fatal("still playing after fading out")
	}
	if !p.fade.silent() {
		t.Error("faded out to something")
	}
	p.TogglePlay()
	if !p.Playing() || !p.out.Playing() {
		t.Fatal("did not play again")
	}
	// Where the fade ends, rather than when a sleep says it is over: the
	// level rises to the one it plays at by then.
	p.fade.mu.Lock()
	level := p.fade.level(p.fade.start.Add(p.fade.dur))
	long := p.fade.dur > fadeIn
	p.fade.mu.Unlock()
	if level != 1 {
		t.Errorf("faded in to %v", level)
	}
	if long {
		t.Error("the fade in takes longer than it may")
	}
}

// A seek playing pauses, which fades out over a few milliseconds, and
// plays again once there, its position the one asked for meanwhile.
func TestSeekWithoutClick(t *testing.T) {
	ch, p := posted()
	defer p.out.Close()
	p.Seek(90 * time.Second)
	if p.out.Playing() {
		t.Error("jumped while playing")
	}
	if p.Position() != 90*time.Second {
		t.Errorf("position %v while seeking", p.Position())
	}
	next(t, ch)
	if !p.out.Playing() || p.out.Position() < 90*time.Second {
		t.Errorf("after the seek: playing %v at %v", p.out.Playing(), p.out.Position())
	}
}

// Seeks one after the other, as the progress bar asks when let go, play
// on from the last.
func TestSeekTwicePlaysOn(t *testing.T) {
	ch, p := posted()
	defer p.out.Close()
	p.Seek(90 * time.Second)
	p.Seek(95 * time.Second)
	next(t, ch)
	for len(ch) > 0 {
		next(t, ch)
	}
	// Both jumps are posted: the first is overtaken by the second, which is
	// the one that plays on from where the bar was let go.
	deadline := time.After(2 * time.Second)
	for p.seeking {
		select {
		case fn := <-ch:
			fn()
		case <-deadline:
			t.Fatal("the seek did not play on")
		}
	}
	if !p.out.Playing() || p.out.Position() < 95*time.Second {
		t.Errorf("after two seeks: playing %v at %v", p.out.Playing(), p.out.Position())
	}
}

// A radio asks for more as its queue nears its end, and adds what is not
// in it already.
func TestRadioRefills(t *testing.T) {
	p := &Player{at: -1}
	p.Cue([]netease.Track{{ID: 1}, {ID: 2}})
	asked := 0
	p.AttachRadio("fm:DEFAULT", func(got func([]netease.Track)) {
		asked++
		got([]netease.Track{{ID: 2}, {ID: 3}})
	})
	if asked != 1 || len(p.Queue()) != 3 || p.Queue()[2].ID != 3 {
		t.Fatalf("asked %d, queue %v", asked, p.Queue())
	}
	if p.Radio() != "fm:DEFAULT" {
		t.Errorf("radio %q", p.Radio())
	}
	// Playing something else ends the radio.
	p.endRadio()
	if p.Radio() != "" || p.feed != nil {
		t.Error("radio still on")
	}
}

func TestNormGain(t *testing.T) {
	for _, c := range []struct {
		st   *netease.Stream
		want float64
	}{
		{nil, 1},
		{&netease.Stream{}, 1},
		{&netease.Stream{Gain: -6.6, Peak: 1}, math.Pow(10, -2.6/20)},
		// Quiet, it is lifted, but not past its peak.
		{&netease.Stream{Gain: 3, Peak: 0.8}, 1.25},
		{&netease.Stream{Gain: -4, Peak: 0}, 1},
	} {
		if g := normGain(c.st); math.Abs(g-c.want) > 1e-9 {
			t.Errorf("%+v: %v, want %v", c.st, g, c.want)
		}
	}
}

// The cache gives back what it kept, at the level asked for or a better
// one, and drops what was played longest ago once over its limit.
func TestCache(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 400)
	store := func(id int64, st *netease.Stream, n int) {
		f := c.part(st)
		f.Write(make([]byte, n))
		f.Close()
		c.keep(id, st.Level, st, f.Name(), int64(n))
	}
	store(1, &netease.Stream{Level: "lossless", Type: "flac", Gain: -3}, 100)
	dl, st, ok := c.lookup(1, "exhigh")
	if !ok || dl.size != 100 || st.Type != "flac" || st.Gain != -3 {
		t.Fatalf("lookup: %v %+v", ok, st)
	}
	dl.Close()
	if _, _, ok := c.lookup(1, "hires"); ok {
		t.Error("found a lossless song for Hi-Res")
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "1-lossless.flac"), old, old)
	store(2, &netease.Stream{Level: "exhigh", Type: "mp3"}, 100)
	store(3, &netease.Stream{Level: "exhigh", Type: "mp3"}, 100)
	store(4, &netease.Stream{Level: "exhigh", Type: "mp3"}, 100)
	store(5, &netease.Stream{Level: "exhigh", Type: "mp3"}, 100)
	if _, _, ok := c.lookup(1, "standard"); ok {
		t.Error("kept the song played longest ago")
	}
	if dl, _, ok := c.lookup(5, "standard"); !ok {
		t.Error("dropped the song just kept")
	} else {
		dl.Close()
	}
	if n := c.Size(); n > 400 {
		t.Errorf("takes %d of 400", n)
	}
	c.Clear()
	if n := c.Size(); n != 0 {
		t.Errorf("cleared to %d", n)
	}
}

// A song NetEase serves below the level asked for is kept where it was
// served, and asking for that level again finds it: asked for lossless and
// served ex-high, it was downloaded again on every play.
func TestCacheServedBelowAsked(t *testing.T) {
	c := NewCache(t.TempDir(), 400)
	st := &netease.Stream{Level: "exhigh", Type: "mp3", Size: 100}
	f := c.part(st)
	f.Write(make([]byte, 100))
	f.Close()
	if _, ok := c.keep(1, "lossless", st, f.Name(), 100); !ok {
		t.Fatal("not kept")
	}
	dl, got, ok := c.lookup(1, "lossless")
	if !ok {
		t.Fatal("kept at ex-high for a lossless ask: not found")
	}
	defer dl.Close()
	if got.Level != "exhigh" || got.Size != 100 {
		t.Errorf("lookup: %+v", got)
	}
}

// Leaving a radio keeps the song playing and those before it, drops those
// it queued after, and asks it for no more.
func TestEndRadio(t *testing.T) {
	p := &Player{at: -1}
	p.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}})
	p.at, p.track = 1, &p.queue[1]
	asked := 0
	p.AttachRadio("heart", func(got func([]netease.Track)) { asked++ })
	p.EndRadio()
	if p.Radio() != "" || p.Current().ID != 2 {
		t.Fatalf("radio %q, current %v", p.Radio(), p.Current())
	}
	if q := p.Queue(); len(q) != 2 || q[0].ID != 1 || q[1].ID != 2 {
		t.Fatalf("queue %v", q)
	}
	p.refill()
	if asked != 0 {
		t.Error("asked the radio after leaving it")
	}
}

// The player leaves the listener's volume to the system's mixer once the
// mixer applies it, mixing only the track's gain then.
func TestSystemVolumeLeavesTheVolumeOut(t *testing.T) {
	p := &Player{at: -1, volume: 0.5, gain: 1}
	p.applyGain()
	if g := p.fade.gain; math.Abs(g-0.5) > 1e-9 {
		t.Errorf("gain %v, want the volume the slider shows", g)
	}
	p.SetSystemVolume(true)
	if g := p.fade.gain; math.Abs(g-1) > 1e-9 {
		t.Errorf("gain %v, want the track's alone", g)
	}
	// Turned down while the mixer holds the volume: it mixes the track all the same.
	p.SetVolume(0.2)
	if g := p.fade.gain; math.Abs(g-1) > 1e-9 {
		t.Errorf("gain %v, want the track's alone while the mixer applies the volume", g)
	}
	// The mixer gone, the app applies the volume again.
	p.SetSystemVolume(false)
	if g := p.fade.gain; math.Abs(g-0.2) > 1e-9 {
		t.Errorf("gain %v, want the volume the slider shows again", g)
	}
}
