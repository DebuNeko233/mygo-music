package mixvol

import (
	"math"
	"testing"
)

// pactlListed is what `pactl --format=json list sink-inputs` prints for a
// session with three streams: two of other programs, one of the app's own
// between them, whose channels differ so that a mean is not its first channel.
const pactlListed = `[
  {
    "index": 203,
    "driver": "protocol-native.c",
    "sink": 0,
    "sample_spec": "s16le 1ch 44100Hz",
    "corked": false,
    "mute": false,
    "volume": {
      "mono": { "value": 16384, "value_percent": "25%", "db": "-12.04 dB" }
    },
    "properties": {
      "application.name": "other-player",
      "application.process.binary": "other-player",
      "application.process.id": "2210",
      "media.name": "Some Track"
    },
    "state": "RUNNING"
  },
  {
    "index": 271,
    "driver": "protocol-native.c",
    "sink": 0,
    "sample_spec": "s16le 2ch 44100Hz",
    "corked": false,
    "mute": false,
    "volume": {
      "front-left": { "value": 65536, "value_percent": "100%", "db": "0.00 dB" },
      "front-right": { "value": 32768, "value_percent": "50%", "db": "-6.02 dB" }
    },
    "properties": {
      "application.name": "netease-music-mygo",
      "application.process.binary": "mygo-cloudmusic",
      "application.process.id": "4242",
      "media.name": "Some Track"
    },
    "state": "RUNNING"
  },
  {
    "index": 288,
    "driver": "protocol-native.c",
    "sink": 0,
    "sample_spec": "s16le 2ch 48000Hz",
    "corked": true,
    "mute": false,
    "volume": {
      "front-left": { "value": 32768, "value_percent": "50%", "db": "-6.02 dB" },
      "front-right": { "value": 32768, "value_percent": "50%", "db": "-6.02 dB" }
    },
    "properties": {
      "application.name": "browser",
      "application.process.id": 7777
    },
    "state": "CORKED"
  }
]`

// pwDumped is the shape `pw-dump` prints, cut down to the objects the app reads:
// a stream node of pw-play's kind, which names its process only through the
// client object it belongs to, beside nodes that name it themselves.
const pwDumped = `[
  { "id": 0, "type": "PipeWire:Interface:Core", "version": 4 },
  { "id": 30, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "factory.name": "support.null-audio-sink", "node.name": "Dummy-Driver" } } },
  { "id": 35, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "media.class": "Audio/Sink", "node.name": "auto_null" } } },
  { "id": 33, "type": "PipeWire:Interface:Client", "version": 3,
    "info": { "props": { "application.name": "WirePlumber", "application.process.id": 1983 } } },
  { "id": 48, "type": "PipeWire:Interface:Client", "version": 3,
    "info": { "props": { "application.name": "pw-cat", "application.process.id": 4242 } } },
  { "id": 49, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "application.name": "pw-play", "client.id": 48,
      "media.class": "Stream/Output/Audio", "node.name": "pw-play" } } },
  { "id": 55, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "application.process.id": 7777,
      "media.class": "Stream/Output/Audio", "node.name": "other-player" } } },
  { "id": 57, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "client.id": 48, "media.class": "Stream/Input/Audio", "node.name": "capture" } } },
  { "id": 62, "type": "PipeWire:Interface:Node", "version": 3,
    "info": { "props": { "application.process.id": "5555",
      "media.class": "Stream/Output/Audio", "node.name": "string-pid" } } }
]`

func TestPactlStreamOf(t *testing.T) {
	// pactl lists the whole session's streams, so the app's own has to be told
	// apart by the process that plays it, and its volume is the mean of its
	// channels rather than one channel's.
	tests := []struct {
		name       string
		listed     string
		pid        int
		wantIndex  int
		wantVolume float64
		want       bool
	}{
		{
			name:       "the app's own stream among others",
			listed:     pactlListed,
			pid:        4242,
			wantIndex:  271,
			wantVolume: 0.75,
			want:       true,
		},
		{
			name:   "a process that plays nothing",
			listed: pactlListed,
			pid:    1,
			want:   false,
		},
		{
			name:   "output pactl did not print",
			listed: "Failure: Connection refused\n",
			pid:    4242,
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream, ok := pactlSinkInputFor(pactlSinkInputs([]byte(test.listed)), test.pid)
			if ok != test.want {
				t.Fatalf("found %v, want %v", ok, test.want)
			}
			if stream.Index != test.wantIndex {
				t.Errorf("index %d, want %d", stream.Index, test.wantIndex)
			}
			if math.Abs(stream.Volume-test.wantVolume) > 1e-9 {
				t.Errorf("volume %v, want %v", stream.Volume, test.wantVolume)
			}
		})
	}
}

func TestPactlSinkInputByIndex(t *testing.T) {
	// A stream that has ended is no longer listed, so the index the link keeps
	// has to stop resolving instead of picking whichever stream comes next.
	inputs := pactlSinkInputs([]byte(pactlListed))

	if stream, ok := pactlSinkInputByIndex(inputs, 271); !ok || math.Abs(stream.Volume-0.75) > 1e-9 {
		t.Errorf("cached stream read as (%v, %v), want (0.75, true)", stream.Volume, ok)
	}
	if _, ok := pactlSinkInputByIndex(inputs, 999); ok {
		t.Error("a stream that is not listed resolved")
	}
}

func TestPwDumpStreamID(t *testing.T) {
	// A pw-play stream names its process only on the client object it belongs
	// to, while other programs name it on the node, and a capture stream of the
	// same process must be left alone.
	tests := []struct {
		name   string
		dumped string
		pid    int
		wantID int
		wantOK bool
	}{
		{name: "process named by the client object", dumped: pwDumped, pid: 4242, wantID: 49, wantOK: true},
		{name: "process named by the node as a number", dumped: pwDumped, pid: 7777, wantID: 55, wantOK: true},
		{name: "process named by the node as a string", dumped: pwDumped, pid: 5555, wantID: 62, wantOK: true},
		{name: "a process that plays nothing", dumped: pwDumped, pid: 9999, wantOK: false},
		{name: "output pw-dump did not print", dumped: "error: no session\n", pid: 4242, wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, ok := pwDumpStreamID([]byte(test.dumped), test.pid)
			if ok != test.wantOK || id != test.wantID {
				t.Fatalf("found (%d, %v), want (%d, %v)", id, ok, test.wantID, test.wantOK)
			}
		})
	}
}

func TestWpctlVolume(t *testing.T) {
	// wpctl marks a muted stream after its volume and prints nothing at all for
	// a stream that has ended, which must read as no volume rather than as one.
	tests := []struct {
		name   string
		out    string
		want   float64
		wantOK bool
	}{
		{name: "plain", out: "Volume: 0.42\n", want: 0.42, wantOK: true},
		{name: "muted", out: "Volume: 0.42 [MUTED]\n", want: 0.42, wantOK: true},
		{name: "full", out: "Volume: 1.00\n", want: 1, wantOK: true},
		{name: "above full volume", out: "Volume: 1.50\n", want: 1, wantOK: true},
		{name: "stream gone", out: "", wantOK: false},
		{name: "something else entirely", out: "Node '49' not found\n", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			volume, ok := wpctlVolume([]byte(test.out))
			if ok != test.wantOK || math.Abs(volume-test.want) > 1e-9 {
				t.Fatalf("read (%v, %v), want (%v, %v)", volume, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestPercentOf(t *testing.T) {
	// The percent pactl is given decides where the ends of the slider land, and
	// a volume rounded off the end would leave the app full while it believes
	// it is quiet or the other way round.
	tests := []struct {
		volume float64
		want   int
	}{
		{volume: 0, want: 0},
		{volume: 0.004, want: 0},
		{volume: 0.005, want: 1},
		{volume: 0.5, want: 50},
		{volume: 0.995, want: 100},
		{volume: 1, want: 100},
		{volume: 1.5, want: 100},
		{volume: -1, want: 0},
	}

	for _, test := range tests {
		if got := percentOf(test.volume); got != test.want {
			t.Errorf("percentOf(%v) = %d, want %d", test.volume, got, test.want)
		}
	}
}
