package player

import (
	"cmp"
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// asker says where a track streams from, at a level.
type asker func(id int64, level string) (*netease.Stream, error)

// songURL is where a track streams from: what the API says, or what the
// tests serve. A load takes it here, on the main thread, as its goroutine
// outlives the call that started it.
var songURL asker = netease.SongURL

// lyricsOf is the words of a track: what the API says, or what the tests
// serve. Taken on the main thread for the same reason as songURL.
var lyricsOf = netease.Lyrics

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// PlayEnd says why a play session ended, without inferring it from position.
type PlayEnd string

const (
	MovedOn     PlayEnd = "ui"
	Playthrough PlayEnd = "playend"
	Interrupted PlayEnd = "interrupt"
	Exception   PlayEnd = "exception"
)

// PlaySource identifies the list a queue came from. Unknown fields stay empty
// and are omitted from play logs.
type PlaySource struct {
	Source string
	ID     int64
	Alg    string
}

// PlaybackEvent describes an actual playback session. Started follows the
// first successful audio start, including each repeat; End is empty for it.
type PlaybackEvent struct {
	Track    netease.Track
	Source   PlaySource
	Started  bool
	End      PlayEnd
	Position time.Duration
	Played   time.Duration
	Bitrate  int // kilobits per second, zero if unknown
	Level    string
}

// Player plays a queue of tracks. Its methods run on the main thread;
// loading happens on goroutines, whose results post brings back there.
type Player struct {
	// Failure causes survive a connection change; their display text does not.
	offlineAudioError, offlineLyricsError bool
	retrying                              bool
	lyricsErr                             string
	post                                  func(func())
	// Quality returns the stream level to load, as the settings say.
	Quality func() string
	// Normalize reports whether to even out the loudness of tracks, as
	// the settings say.
	Normalize func() bool
	// Cache keeps the songs played, if set.
	Cache *Cache
	// FetchLyrics optionally supplies persistent lyrics; captured per load.
	FetchLyrics func(int64) (string, string, string, error)
	// Changed, if set, hears that what plays, where it is or whether it
	// plays changed.
	Changed func()
	// Playback, if set, hears when a song actually starts and why its play
	// session ends. Pausing and seeking do not end that session.
	Playback func(PlaybackEvent)

	queue   []netease.Track
	order   []int // the queue's indices in play order: shuffled or not
	at      int   // where in order the current track is, -1 for none
	step    int   // the latest move in play order: 1 on, -1 back, 0 undirected
	shuffle bool
	repeat  RepeatMode
	volume  float64
	// systemVolume is set while the system's mixer holds the listener's
	// volume: what plays then carries only the track's gain and the fades,
	// as the mixer applies the volume itself.
	systemVolume bool

	// A radio goes on with the tracks its feed gives, as the queue nears
	// its end, for as long as nothing else is played.
	radio    string
	feed     func(got func([]netease.Track))
	radioGen int  // counts radios, to drop what an old one gives
	feeding  bool // the feed is asked
	awaiting bool // at the queue's end, for what the feed gives

	track      *netease.Track
	source     PlaySource
	active     bool
	heardSince time.Time
	played     time.Duration
	now        func() time.Time
	out        *audio.Player
	stream     stream
	info       *netease.Stream // what the current track streams
	fade       fader
	gain       float64 // the current track's normalizing gain
	loading    bool
	playing    bool // what the listener wants: false while paused
	err        string
	gen        int // counts loads, to drop the results of stale ones
	// cacheChanging is set while the song cache's folder moves: nothing
	// loads meanwhile, and what plays is let go of.
	cacheChanging bool

	// ahead is the next track's download, fetched while this one plays,
	// so that it starts at once when this one ends: nil without one.
	ahead *ahead
	// tries counts the times the current track was tried again, as its load
	// failed, or its download was cut off: it starts over with another
	// track, so that a song that keeps breaking off is left where it is.
	tries int

	lyrics                            []lyrics.Line
	lyricsLoaded                      bool
	lyricRevision                     uint64
	lyricBusy, coverBusy, coverLoaded bool
	lyricFailure, coverFailure        error

	// pending holds a seek asked for while the track loads.
	pending time.Duration
	// A seek playing fades out, then lands, then fades in: meanwhile the
	// position is where it goes.
	seeking bool
	seekTo  time.Duration
	seekGen int
}

// ahead is a track's download, fetched while the one before it plays: the
// cache keeps it, and it is read from there when it plays.
type ahead struct {
	id    int64 // the track's
	level string
	dl    *download
	info  *netease.Stream
}

func New(post func(func())) *Player {
	p := &Player{post: post, at: -1, volume: 0.8, gain: 1}
	// Music, mixed far enough ahead to play on while macOS keeps the app
	// waiting, as it does while its appearance changes.
	audio.SetLatency(audio.LatencyPlayback)
	// The media keys, and the controls of Control Center.
	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		switch cmd {
		case mygo.MediaPlay, mygo.MediaPause:
			if (cmd == mygo.MediaPlay) != p.playing {
				p.TogglePlay()
			}
		case mygo.MediaNext:
			p.Next()
		case mygo.MediaPrevious:
			p.Prev()
		case mygo.MediaSeek:
			p.Seek(pos)
		}
		p.post(func() {})
	})
	return p
}

// sync shows the system, and Changed, where playback is.
func (p *Player) sync() {
	if p.Changed != nil {
		defer p.Changed()
	}
	if p.track == nil {
		mygo.NowPlaying.Clear()
		return
	}
	state := mygo.PlaybackPaused
	if p.playing && !p.cacheChanging {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, p.Position())
}

// announce shows the system the track, and its cover once loaded.
func (p *Player) announce(t *netease.Track, artwork []byte) {
	state := mygo.PlaybackPaused
	if p.playing && !p.cacheChanging {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    t.Name,
		Artist:   t.ArtistNames(),
		Album:    t.Album.Name,
		Artwork:  artwork,
		Duration: t.Length(),
		Position: p.Position(),
		State:    state,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaPrevious | mygo.MediaSeek,
	})
}

func (p *Player) ended() {
	p.finish(Playthrough)
	if p.repeat == RepeatOne {
		p.out.Seek(0)
		p.out.Play()
		p.begin()
		return
	}
	if p.at+1 >= len(p.order) && p.feed != nil {
		p.awaitFeed()
		return
	}
	if p.at+1 >= len(p.order) && p.repeat == RepeatOff {
		p.playing = false
		p.sync()
		return
	}
	p.Next()
}

// PlayQueue replaces the queue with tracks and plays the one at start.
func (p *Player) PlayQueue(tracks []netease.Track, start int) {
	p.PlayQueueFrom(tracks, start, PlaySource{})
}

// PlayQueueFrom plays a queue and carries where it came from into play logs.
func (p *Player) PlayQueueFrom(tracks []netease.Track, start int, source PlaySource) {
	p.source = source
	p.playQueue(tracks, max(start, 0))
}

// PlayShuffled starts a whole list with any song at its head. Explicitly
// choosing a song through PlayQueue still keeps that song first.
func (p *Player) PlayShuffled(tracks []netease.Track) {
	p.PlayShuffledFrom(tracks, PlaySource{})
}

// PlayShuffledFrom shuffles a queue and carries its source into play logs.
func (p *Player) PlayShuffledFrom(tracks []netease.Track, source PlaySource) {
	if len(tracks) == 0 {
		return
	}
	p.source = source
	p.shuffle = true
	p.playQueue(tracks, -1)
}

func (p *Player) playQueue(tracks []netease.Track, start int) {
	if len(tracks) == 0 {
		return
	}
	p.endRadio()
	p.step = 0
	start = min(start, len(tracks)-1)
	p.queue = append([]netease.Track(nil), tracks...)
	p.order = make([]int, len(p.queue))
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle {
		p.shuffleOrder(start)
		p.load(0)
		return
	}
	p.load(max(start, 0))
}

// PlayRadio plays tracks in their order, then goes on with those feed
// gives as the queue nears its end, until something else is played. The
// radio's name says which it is. feed calls got on the main thread.
func (p *Player) PlayRadio(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if len(tracks) == 0 {
		return
	}
	shuffle := p.shuffle
	p.shuffle = false
	p.PlayQueue(tracks, 0)
	p.shuffle = shuffle
	p.AttachRadio(name, feed)
}

// RadioFrom makes the track playing the first of a radio's queue, which
// goes on with tracks, then with those feed gives: without starting it
// over.
func (p *Player) RadioFrom(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if p.track == nil {
		return
	}
	p.queue, p.order, p.at = []netease.Track{*p.track}, []int{0}, 0
	p.step = 0
	p.appendTracks(tracks)
	if p.shuffle {
		// The current track stays first; the radio's order must agree
		// with the shuffle mode the listener sees.
		p.shuffleOrder(0)
	}
	p.AttachRadio(name, feed)
}

// AttachRadio makes the queue the radio name's, which feed goes on with,
// as after restoring a session that played it.
func (p *Player) AttachRadio(name string, feed func(got func([]netease.Track))) {
	p.endRadio()
	p.radio, p.feed = name, feed
	p.refill()
}

// EndRadio stops the radio playing: the song playing plays on, but the
// songs the radio queued after it go, and it adds no more.
func (p *Player) EndRadio() {
	if p.radio == "" && p.feed == nil {
		return
	}
	awaiting := p.awaiting
	p.endRadio()
	if p.at >= 0 && p.at+1 < len(p.order) {
		kept := make([]netease.Track, p.at+1)
		for i, k := range p.order[:p.at+1] {
			kept[i] = p.queue[k]
		}
		p.queue, p.order = kept, make([]int, len(kept))
		for i := range p.order {
			p.order[i] = i
		}
	}
	if awaiting {
		// At its end, waiting for more: nothing plays on.
		p.playing = false
		p.sync()
	}
}

func (p *Player) endRadio() {
	p.radioGen++
	p.radio, p.feed, p.feeding, p.awaiting = "", nil, false, false
}

// Radio returns the name of the radio playing, or "".
func (p *Player) Radio() string { return p.radio }

// refill asks the radio for more once the queue is near its end.
func (p *Player) refill() {
	if p.feed == nil || p.feeding || p.at < len(p.order)-2 {
		return
	}
	p.feeding = true
	gen := p.radioGen
	p.feed(func(more []netease.Track) {
		if gen != p.radioGen {
			return
		}
		p.feeding = false
		p.appendTracks(more)
		switch {
		case !p.awaiting:
		case p.at+1 < len(p.order):
			p.awaiting = false
			p.step = 1
			p.load(p.at + 1)
		default:
			// The radio has nothing more: it stops at its end.
			p.awaiting, p.playing = false, false
			p.err = "电台暂时没有更多歌曲"
			p.sync()
		}
	})
}

// awaitFeed waits at the end of a radio's queue for what its feed gives.
func (p *Player) awaitFeed() {
	p.awaiting = true
	p.refill()
}

// appendTracks adds tracks to the end of the queue, but those in it.
func (p *Player) appendTracks(tracks []netease.Track) {
	in := make(map[int64]bool, len(p.queue))
	for _, t := range p.queue {
		in[t.ID] = true
	}
	for _, t := range tracks {
		if !in[t.ID] {
			in[t.ID] = true
			p.order = append(p.order, len(p.queue))
			p.queue = append(p.queue, t)
		}
	}
}

// shuffleOrder keeps first at the head, or allows any head when first is -1.
func (p *Player) shuffleOrder(first int) {
	rand.Shuffle(len(p.order), func(i, j int) { p.order[i], p.order[j] = p.order[j], p.order[i] })
	for i, v := range p.order {
		if v == first {
			p.order[0], p.order[i] = p.order[i], p.order[0]
		}
	}
}

func (p *Player) ToggleShuffle() {
	p.shuffle = !p.shuffle
	if len(p.order) == 0 {
		return
	}
	cur := p.order[max(p.at, 0)]
	if p.shuffle {
		p.shuffleOrder(cur)
		p.at = 0
	} else {
		for i := range p.order {
			p.order[i] = i
		}
		p.at = cur
	}
}

func (p *Player) CycleRepeat() { p.repeat = (p.repeat + 1) % 3 }

// Current returns the track playing or loading, or nil.
func (p *Player) Current() *netease.Track { return p.track }

// Queue returns the tracks queued, in the order they were given.
func (p *Player) Queue() []netease.Track { return p.queue }

// Playing reports what the listener wants: false while paused.
func (p *Player) Playing() bool { return p.playing }

// Loading reports whether the current track is still loading.
func (p *Player) Loading() bool { return p.loading || p.awaiting }

// Ready reports whether the current track has loaded, to play.
func (p *Player) Ready() bool { return p.out != nil }

// Err returns why the current track did not play, or "".
func (p *Player) Err() string {
	if p.offlineAudioError && !netease.Offline() {
		return "播放中断"
	}
	return p.err
}

// PlaybackRetryable offers an explicit restart after reconnecting; a system
// event alone never starts audio or moves the listener's saved position.
func (p *Player) PlaybackRetryable() bool {
	return p.offlineAudioError && !netease.Offline() && !p.Loading() && !p.cacheChanging
}

// Stream returns what the current track streams, once loaded, or nil.
func (p *Player) Stream() *netease.Stream { return p.info }

func (p *Player) Shuffle() bool      { return p.shuffle }
func (p *Player) Repeat() RepeatMode { return p.repeat }

// Lyrics returns the current track's lyrics, and whether they have loaded.
func (p *Player) Lyrics() ([]lyrics.Line, bool) { return p.lyrics, p.lyricsLoaded }

// Downloaded returns the share of the current track downloaded so far, or
// false before it has started to.
func (p *Player) Downloaded() (float64, bool) {
	if p.stream == nil {
		return 0, false
	}
	dl, _ := p.stream.source()
	return dl.Progress(), true
}

// Audio is the current track's sound decoded apart from what plays, as
// OpenAudio opens it: interleaved stereo at its rate, read as it
// downloads.
type Audio interface {
	SampleRate() int
	Channels() int
	Read(dst []float32) (int, error)
	// SetPosition goes to a frame, of SampleRate a second.
	SetPosition(frame int64) error
	Close() error
}

// OpenAudio opens the current track's sound again, for decoding apart
// from what plays, as for transcribing an episode. It reads the same
// download, waiting for what has not come in, and fails once the track
// changes. It works on any goroutine once opened; close it when done.
func (p *Player) OpenAudio() (Audio, error) {
	if p.stream == nil || p.track == nil {
		return nil, errors.New("player: nothing loaded")
	}
	dl, _ := p.stream.source()
	return openShared(dl, p.track.Duration)
}

// PlayOrder returns the queue's indices in play order, and where the
// current track is among them, -1 for none.
func (p *Player) PlayOrder() ([]int, int) { return p.order, p.at }

// StepDirection is the latest move in play order: 1 forward, -1 backward,
// or 0 for a replacement or reselecting the same entry. Record it before
// loading, while the old position is known; song IDs can repeat, and
// two-song steps are ambiguous.
func (p *Player) StepDirection() int { return p.step }

func (p *Player) Next() {
	if len(p.order) == 0 {
		return
	}
	at := p.at + 1
	if at >= len(p.order) {
		if p.feed != nil {
			p.finish(MovedOn)
			p.stop()
			p.awaitFeed()
			return
		}
		at = 0
	}
	p.step = 1
	p.load(at)
}

// Prev plays the track before this one, however far this one has played:
// once a few seconds in, going back to its start only kept the listener
// pressing twice.
func (p *Player) Prev() {
	if len(p.order) == 0 {
		return
	}
	at := p.at - 1
	if at < 0 {
		at = len(p.order) - 1
	}
	p.step = -1
	p.load(at)
}

// Jump plays the queue's track i.
func (p *Player) Jump(i int) {
	for k, v := range p.order {
		if v == i {
			p.step = cmp.Compare(k, p.at)
			p.load(k)
			return
		}
	}
}

// TogglePlay pauses, fading out, or plays, fading in.
func (p *Player) TogglePlay() {
	if p.cacheChanging {
		// What is wanted is kept, and heard once the folder has moved.
		p.playing = !p.playing
		p.sync()
		return
	}
	if p.out == nil {
		switch {
		case p.loading:
			// It plays once loaded, or not.
			p.playing = !p.playing
			p.sync()
		case len(p.order) > 0:
			p.tries = 0
			p.loadSkipping(max(p.at, 0), 0, true, p.pending)
		}
		return
	}
	out := p.out
	if p.playing {
		p.pause()
		p.playing = false
		p.fade.fade(0, fadeOut, func() {
			p.post(func() {
				if p.out == out && !p.playing {
					out.Pause()
					p.sync()
				}
			})
		})
		p.sync()
		return
	}
	p.playing = true
	if !p.seeking && !out.Playing() {
		if out.Position() >= out.Duration()-time.Second {
			out.Seek(0)
			p.pause()
		}
		out.Play()
		// Restore deliberately loads paused and therefore has no session yet.
		p.begin()
	}
	p.fade.fade(1, fadeIn, nil)
	// A session picked up again was loaded paused: its next track may
	// still be fetched while this one plays.
	p.prefetch(p.at + 1)
	p.sync()
}

// seekGap is how long a seek waits for what plays to fade out, which the
// audio player does over 5 ms, as it pauses: within the next buffer or
// two it sends the system.
const seekGap = 30 * time.Millisecond

// Seek moves to t. Playing, it pauses and plays again around the jump,
// which the audio player fades over a few milliseconds, rather than
// cutting from one sound to the other with a click.
func (p *Player) Seek(t time.Duration) {
	if p.out == nil {
		p.pending = t
		return
	}
	out := p.out
	// Paused, it jumps at once; but a seek on its way paused it only for
	// the jump, and this one takes its place, playing on from there.
	if !out.Playing() && !p.seeking {
		out.Seek(t)
		p.sync()
		return
	}
	p.seekGen++
	gen := p.seekGen
	p.pause()
	p.seeking, p.seekTo = true, t
	out.Pause()
	time.AfterFunc(seekGap, func() {
		p.post(func() {
			if gen != p.seekGen || p.out != out {
				return
			}
			p.seeking = false
			out.Seek(t)
			if p.playing || !p.fade.silent() {
				out.Play()
				p.resume()
			}
			p.sync()
		})
	})
	p.sync()
}

func (p *Player) SetVolume(v float64) {
	p.volume = v
	p.applyGain()
}

// SetSystemVolume tells the player whether the system's mixer applies the
// listener's volume. It does once the system holds a volume for the app, as
// the Windows mixer and PulseAudio do: the player then leaves the volume out
// of what it mixes, keeping only the track's gain and the fades, so that it
// plays as loud as the mixer shows rather than at both volumes together.
func (p *Player) SetSystemVolume(on bool) {
	p.systemVolume = on
	p.applyGain()
}

// applyGain sets the volume of what plays: the listener's, and the track's
// gain, if normalizing. The listener's volume is the amplitude itself, the
// number the slider shows, which is the number the system's mixer shows for
// the app as well.
func (p *Player) applyGain() {
	g := 1.0
	if !p.systemVolume {
		g = p.volume
	}
	if p.Normalize != nil && p.Normalize() {
		g *= p.gain
	}
	p.fade.setGain(g)
}

// normPreamp lifts normalized tracks, which NetEase brings to the quiet
// loudness of ReplayGain, nearer to how loud music plays elsewhere.
const normPreamp = 4

// normGain is the gain that brings a track as loud as others, as NetEase
// measured it, but never so loud that its peaks would clip.
func normGain(st *netease.Stream) float64 {
	if st == nil || st.Gain == 0 && st.Peak == 0 {
		return 1
	}
	g := math.Pow(10, (st.Gain+normPreamp)/20)
	if st.Peak > 0 {
		g = min(g, 1/st.Peak)
	}
	return g
}

func (p *Player) Position() time.Duration {
	switch {
	case p.out == nil:
		return p.pending
	case p.seeking:
		return p.seekTo
	}
	return p.out.Position()
}

func (p *Player) Duration() time.Duration {
	if p.track == nil {
		return 0
	}
	return p.track.Length()
}

// Levels returns the spectrum of what plays now.
func (p *Player) Levels() ([Bands]float32, bool) {
	if p.stream == nil || p.out == nil || !p.playing {
		return [Bands]float32{}, false
	}
	_, spec := p.stream.source()
	frame := int64(p.Position().Seconds() * float64(p.stream.SampleRate()))
	return spec.at(frame)
}

func (p *Player) stop() {
	if out := p.out; out != nil {
		// Paused first, it fades out over a few milliseconds, rather than
		// stopping with a click.
		out.Pause()
		time.AfterFunc(100*time.Millisecond, out.Close)
	}
	p.out, p.stream, p.info = nil, nil, nil
	p.seeking = false
	p.fade.attach(nil, 1)
	p.gen++ // Invalidate all pending callbacks.
	// Invalidated metadata callbacks cannot clear their own busy flags later.
	p.lyricBusy, p.coverBusy = false, false
}

// begin starts or resumes a play session only after audio is ready to be heard.
func (p *Player) begin() {
	if p.track == nil {
		return
	}
	p.resume()
	if p.active {
		return
	}
	p.active = true
	p.played = 0
	source := p.source
	if source.Alg == "" {
		source.Alg = p.track.Alg
	}
	p.emit(PlaybackEvent{Track: *p.track, Source: source, Started: true})
}

// heardTime excludes pauses, seeks and loading from the reported listening time.
func (p *Player) heardTime() time.Duration {
	heard := p.played
	if !p.heardSince.IsZero() {
		heard += p.time().Sub(p.heardSince)
	}
	return heard
}

func (p *Player) time() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *Player) resume() {
	p.heardSince = p.time()
}

func (p *Player) pause() {
	if !p.active {
		return
	}
	p.played = p.heardTime()
	p.heardSince = time.Time{}
}

// finish closes the current session with the transition's actual cause.
func (p *Player) finish(end PlayEnd) {
	if !p.active || p.track == nil {
		return
	}
	p.pause()
	p.active = false
	source := p.source
	if source.Alg == "" {
		source.Alg = p.track.Alg
	}
	bitrate, level := 0, ""
	if p.info != nil {
		bitrate, level = p.info.Bitrate/1000, p.info.Level
	}
	p.emit(PlaybackEvent{Track: *p.track, Source: source, End: end,
		Position: p.Position(), Played: p.played, Bitrate: bitrate, Level: level})
}

func (p *Player) emit(event PlaybackEvent) {
	if p.Playback != nil {
		p.Playback(event)
	}
}

// Finish closes an active session without moving its position, as on exit.
func (p *Player) Finish() { p.finish(Interrupted) }

// load plays the track at order[at], skipping those that cannot play.
func (p *Player) load(at int) {
	p.finish(MovedOn)
	p.tries = 0
	p.loadSkipping(at, 0, true, 0)
}

// loadSkipping loads the track at order[at], to play or not, from from,
// skipping those that cannot play: skipped of them so far.
func (p *Player) loadSkipping(at, skipped int, play bool, from time.Duration) {
	t := p.queue[p.order[at]]
	p.stop()
	p.gen++
	gen := p.gen
	// What was fetched ahead plays at once, unless it is of another level
	// than the one asked for now.
	level := p.level()
	ahead := p.takeAhead(t.ID, level)
	ask := songURL
	p.at = at
	p.track = &t
	p.loading, p.playing, p.err = true, play, ""
	p.offlineAudioError, p.offlineLyricsError = false, false
	p.retrying = false
	p.lyricsErr = ""
	p.awaiting = false
	p.lyrics, p.lyricsLoaded = nil, false
	p.lyricBusy, p.coverBusy, p.coverLoaded = false, false, false
	p.lyricFailure, p.coverFailure = nil, nil
	p.pending = from
	p.announce(&t, nil)
	if p.cacheChanging {
		// The folder moves: what is chosen is loaded once it has.
		return
	}
	p.refill()

	p.RetryMetadata()

	go func() {
		info, s, err := p.open(t, level, ahead, ask)
		p.post(func() {
			if gen != p.gen {
				if s != nil {
					s.Close()
				}
				return
			}
			p.loading = false
			if err != nil {
				p.failed(at, skipped, t, from, err)
				return
			}
			p.stream, p.info = s, info
			p.gain = normGain(info)
			p.out = audio.NewPlayer(s)
			// From its middle, as a session picked up again, it fades in.
			lvl := 1.0
			if p.pending > 0 {
				lvl = 0
			}
			p.fade.attach(p.out, lvl)
			p.applyGain()
			p.out.OnEnd(func() {
				p.post(func() {
					if gen == p.gen {
						p.cutOff()
					}
				})
			})
			if p.pending > 0 {
				p.out.Seek(p.pending)
				p.pending = 0
			}
			if p.playing {
				p.out.Play()
				p.begin()
				p.fade.fade(1, fadeIn, nil)
			}
			// The one after it is fetched while this one plays, so that
			// it starts at once when this one ends.
			p.prefetch(at + 1)
			p.sync()
		})
	}()
}

// failed handles a track that could not be loaded: tried again if it may
// come now, skipped if it cannot play at all, and else left where it is
// for the listener to play again.
func (p *Player) failed(at, skipped int, t netease.Track, from time.Duration, err error) {
	p.offlineAudioError = errors.Is(err, netease.ErrOffline)
	if errors.Is(err, netease.ErrOffline) {
		p.loading, p.playing = false, false
		p.err = "未缓存"
		p.sync()
		return
	}
	if errors.Is(err, netease.ErrNotLoggedIn) {
		// Asked for again it would be turned down again, and the song
		// plays once the listener is signed in: nothing is skipped.
		p.err = "登录已过期，请重新登录"
		p.playing = false
		p.sync()
		return
	}
	if errors.Is(err, netease.ErrRiskControl) {
		if p.tries < riskTries {
			p.retry(at, skipped, from, err)
			return
		}
		p.err = "网易云正在校验网络，请稍后重试"
		p.playing = false
		p.sync()
		return
	}
	if !errors.Is(err, netease.ErrUnavailable) {
		// What did not come now may come again: a stream URL gone stale,
		// or a connection dropped, is asked for again a moment later.
		if p.tries < loadTries {
			p.retry(at, skipped, from, err)
			return
		}
		p.err = "加载失败，请重试"
		p.playing = false
		p.sync()
		return
	}
	if t.ProgramID != 0 {
		p.err = "这期节目需要购买才能收听"
	} else {
		p.err = "这首歌需要 VIP 才能播放"
	}
	// Skip what cannot play, unless nothing in the queue can.
	if skipped+1 < len(p.order) {
		next := at + 1
		if next >= len(p.order) {
			next = 0
		}
		p.tries = 0
		p.step = 1
		p.loadSkipping(next, skipped+1, p.playing, 0)
		return
	}
	p.playing = false
	p.sync()
}

// loadTries is how many times a track is tried before it is left for the
// listener to play again: a network down for a moment is back within them,
// where two tries a second apart left the listener with a song that would
// have played had it been asked for once more.
const loadTries = 5

// riskTries keeps a prolonged account or network refusal from becoming a
// continuous stream of requests. The other gateway was already tried by
// SongURL before this retry.
const riskTries = 1

// lyricTries is how many times the words of a song are asked for before they
// are given up on: a request that fails is not a song without words.
const lyricTries = 3

// retry loads the track at at again in a moment, still loading as it
// waits: a stream URL gone stale, or a connection dropped, may work once
// more.
func (p *Player) retry(at, skipped int, from time.Duration, err error) {
	p.tries++
	p.retrying = true
	p.loading = true
	gen, tries := p.gen, p.tries
	time.AfterFunc(loadGap(tries, err), func() {
		p.post(func() {
			if gen == p.gen {
				p.loadSkipping(at, skipped, p.playing, from)
			}
		})
	})
	p.sync()
}

// retryGap is how long the try after n tries waits: a moment longer each
// time, for a network that may be back by then.
func retryGap(n int) time.Duration { return (400 << (n - 1)) * time.Millisecond }

// loadGap gives NetEase's risk check six seconds between immediate tries,
// rather than the shorter gaps used for an ordinary connection failure.
func loadGap(n int, err error) time.Duration {
	if errors.Is(err, netease.ErrRiskControl) {
		return 6 * time.Second
	}
	return retryGap(n)
}

// prefetchGap gives a background risk-check retry ten seconds, without
// changing the backoff for other failures.
func prefetchGap(n int, err error) time.Duration {
	if errors.Is(err, netease.ErrRiskControl) {
		return 10 * time.Second
	}
	return retryGap(n)
}

// cutOff runs as what plays comes to its end: the song's, or the
// download's, cut off, which is played again from where it stopped.
func (p *Player) cutOff() {
	if p.stream == nil || p.stream.cut() == nil {
		p.ended()
		return
	}
	err := p.stream.cut()
	p.offlineAudioError = errors.Is(err, netease.ErrOffline)
	dl, _ := p.stream.source()
	dl.discard()
	if p.tries < loadTries && !errors.Is(err, netease.ErrOffline) {
		p.retry(p.at, 0, p.Position(), err)
		return
	}
	// It cannot be had: stop where it stopped, for the listener to play
	// again.
	p.err = "播放中断，请重试"
	if errors.Is(err, netease.ErrOffline) {
		p.err = "播放中断"
	}
	var decoded *decodeError
	if errors.As(err, &decoded) && !p.offlineAudioError {
		p.err = "音频解码失败，请重试或切换音质"
	}
	p.playing = false
	p.pending = p.Position()
	p.finish(Exception)
	p.stop()
	p.sync()
}

// prefetch fetches the track at order[at] once this one has come in whole,
// into the cache, so that it starts at once when it is played: what it
// cannot fetch is asked for again while this one still plays, and once more
// when it is due.
func (p *Player) prefetch(at int) {
	if at < 0 || at >= len(p.order) || !p.playing {
		p.dropAhead()
		return
	}
	t := p.queue[p.order[at]]
	level := p.level()
	if a := p.ahead; a != nil && a.id == t.ID && a.level == level {
		return // fetched already, or coming in
	}
	p.dropAhead()
	// What plays comes in first: fetching the one after it sooner would
	// take the bytes of the song being played, which is what stutters.
	now := p.stream
	gen, ask := p.gen, songURL
	cache := p.Cache // Capture cache reference on main thread.
	go p.prefetching(gen, t, level, now, ask, cache, 0)
}

// prefetchTries is how many times the track after this one is fetched ahead
// before it is left for when it is due. Risk checks are limited too: an
// account-level refusal can last hours, so retrying until the song ends
// would only keep it active. An ordinary network that is out for a moment
// is back within them, where one fetch that came in badly left the listener
// waiting on the next song for what one more ask would have fetched.
const prefetchTries = 3

// prefetching fetches t ahead, try counting the fetches: it waits for the
// song that plays to come in first, and for the one it fetches to come in
// whole, so that what did not come is asked for again while this one still
// plays, rather than when the next one is due.
func (p *Player) prefetching(gen int, t netease.Track, level string, now stream, ask asker, cache *Cache, try int) {
	if netease.Offline() {
		return
	}
	if now != nil {
		if playing, _ := now.source(); playing != nil {
			playing.waitFor(playing.size)
			if playing.Err() != nil {
				// Let go of, not come in whole: it is not this one's turn
				// to be fetched ahead.
				return
			}
		}
	}
	info, err := ask(t.ID, level)
	if err != nil {
		if !errors.Is(err, netease.ErrUnavailable) && !errors.Is(err, netease.ErrNotLoggedIn) && !errors.Is(err, netease.ErrOffline) {
			// A URL gone stale, or a network down for a moment: asked for
			// again in a moment, as it may be back by then.
			prefetchLater(p, gen, t, level, ask, nil, try, err)
		}
		return
	}
	if !cache.Holds(info) {
		// Not a song the cache would keep: fetching it now would spend its
		// bytes for nothing, as it would come in vain.
		return
	}
	dl := cache.download(t.ID, level, info)
	p.post(func() {
		if gen != p.gen || p.ahead != nil {
			dl.Close()
			return
		}
		p.ahead = &ahead{id: t.ID, level: level, dl: dl, info: info}
	})
	// Waited for, so that what came in not at all is fetched again while
	// this one still plays: left to itself, it was played as if it were
	// fetched ahead, and the next song could not be had.
	// An unknown size still has a terminal result to wait for; skipping
	// this wait used to give an unknown-size prefetch no retry at all.
	dl.waitFor(math.MaxInt64)
	if dl.Err() != nil {
		prefetchLater(p, gen, t, level, ask, dl, try, dl.Err())
	}
}

// prefetchLater asks for the track fetched ahead again in a moment, as try
// says: what came in badly is let go of first, so that the fetch that
// follows is the one the player keeps.
func prefetchLater(p *Player, gen int, t netease.Track, level string, ask asker, failed *download, try int, err error) {
	if errors.Is(err, netease.ErrOffline) || try >= prefetchTries || (errors.Is(err, netease.ErrRiskControl) && try >= riskTries) {
		return
	}
	gap := prefetchGap(max(try, 1), err)
	time.AfterFunc(gap, func() {
		p.post(func() {
			if gen != p.gen {
				return // what plays moved on
			}
			if failed != nil {
				if p.ahead == nil || p.ahead.dl != failed {
					// Taken, as it is due, or dropped: it tries for itself.
					return
				}
				p.ahead = nil
				failed.Close()
			}
			go p.prefetching(gen, t, level, nil, ask, p.Cache, try+1)
		})
	})
}

// takeAhead returns the track fetched ahead, if it plays as id at level,
// and lets go of what was fetched for another track, or another level.
func (p *Player) takeAhead(id int64, level string) *ahead {
	a := p.ahead
	p.ahead = nil
	if a == nil {
		return nil
	}
	if a.id != id || a.level != level {
		a.dl.Close()
		return nil
	}
	return a
}

// dropAhead lets go of the track fetched ahead, if any.
func (p *Player) dropAhead() {
	a := p.ahead
	p.ahead = nil
	if a != nil {
		a.dl.Close()
	}
}

// level returns the stream level to load, as the settings say.
func (p *Player) level() string {
	if p.Quality != nil {
		return p.Quality()
	}
	return "exhigh"
}

// open opens track t's audio at level: from what was fetched ahead of it,
// kept in the cache, or else streamed into it, and kept once it has all
// come in. ask is where its URL comes from, taken on the main thread.
func (p *Player) open(t netease.Track, level string, ahead *ahead, ask asker) (*netease.Stream, stream, error) {
	dl, info, err := p.fetch(t, level, ahead, ask)
	if err != nil {
		return nil, nil, err
	}
	s, err := openStream(dl, t.Duration)
	if err != nil {
		dl.discard()
		dl.Close()
		return nil, nil, err
	}
	return info, s, nil
}

// fetch returns the download of track t at level, and what it streams: the
// one fetched ahead, the one the cache keeps, or one started now.
func (p *Player) fetch(t netease.Track, level string, ahead *ahead, ask asker) (*download, *netease.Stream, error) {
	if ahead != nil {
		if ahead.dl.Err() == nil {
			return ahead.dl, ahead.info, nil
		}
		// Cut off before it played: asked for again, from its start.
		ahead.dl.Close()
	}
	dl, info, ok := p.Cache.lookup(t.ID, level)
	if ok {
		return dl, info, nil
	}
	if netease.Offline() {
		return nil, nil, netease.ErrOffline
	}
	info, err := ask(t.ID, level)
	if err != nil {
		return nil, nil, err
	}
	return p.Cache.download(t.ID, level, info), info, nil
}

// Session is where playback was left, to pick up again.
type Session struct {
	Queue    []netease.Track `json:"queue"`
	Order    []int           `json:"order"`
	At       int             `json:"at"`
	Shuffle  bool            `json:"shuffle"`
	Repeat   RepeatMode      `json:"repeat"`
	Position int64           `json:"position"` // milliseconds
	Radio    string          `json:"radio,omitempty"`
}

// Session returns where playback is, or false with nothing queued.
func (p *Player) Session() (Session, bool) {
	if p.track == nil || p.at < 0 {
		return Session{}, false
	}
	return Session{
		Queue:    p.queue,
		Order:    p.order,
		At:       p.at,
		Shuffle:  p.shuffle,
		Repeat:   p.repeat,
		Position: p.Position().Milliseconds(),
		Radio:    p.radio,
	}, true
}

// Restore picks up a session where it was left, paused, and loads its
// track to play at once when asked.
func (p *Player) Restore(s Session) {
	n := len(s.Queue)
	if n == 0 || len(s.Order) != n || s.At < 0 || s.At >= n {
		return
	}
	seen := make([]bool, n)
	for _, i := range s.Order {
		if i < 0 || i >= n || seen[i] {
			return
		}
		seen[i] = true
	}
	p.queue, p.order = s.Queue, s.Order
	p.step = 0
	p.shuffle, p.repeat = s.Shuffle, s.Repeat%3
	p.tries = 0
	p.loadSkipping(s.At, 0, false, time.Duration(s.Position)*time.Millisecond)
}
