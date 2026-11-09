package mixvol

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// pulseFullVolume is what PulseAudio calls full volume. It reports a stream's
// channels on that scale, so dividing by it gives the 0 to 1 amplitude the app
// works in.
const pulseFullVolume = 65536

// pipewirePlaybackClass is the media class of a node that plays audio, as
// opposed to one that captures it, which is never the app's own stream.
const pipewirePlaybackClass = "Stream/Output/Audio"

// A pactlSinkInput is one of the streams that `pactl list sink-inputs` reports.
type pactlSinkInput struct {
	// Index is what pactl names the stream by when its volume is set.
	Index int
	// PID is the process that plays the stream, which is what tells the app's
	// own stream from the other programs'. It is zero when pactl does not say.
	PID int
	// Volume is the mean of the stream's channel volumes, an amplitude from 0
	// to 1.
	Volume float64
}

// pactlSinkInputs reads the JSON pactl prints for its sink inputs. A stream
// without channels carries no volume the app could follow, so it is left out
// rather than reported as silent.
func pactlSinkInputs(data []byte) []pactlSinkInput {
	var listed []struct {
		Index      int                        `json:"index"`
		Properties map[string]any             `json:"properties"`
		Volume     map[string]json.RawMessage `json:"volume"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil
	}

	inputs := make([]pactlSinkInput, 0, len(listed))
	for _, stream := range listed {
		sum, channels := 0.0, 0
		for _, channel := range stream.Volume {
			var held struct {
				Value float64 `json:"value"`
			}
			if err := json.Unmarshal(channel, &held); err != nil {
				continue
			}
			sum += held.Value
			channels++
		}
		if channels == 0 {
			continue
		}

		pid, _ := wholeNumber(stream.Properties["application.process.id"])
		inputs = append(inputs, pactlSinkInput{
			Index:  stream.Index,
			PID:    pid,
			Volume: clamp01(sum / float64(channels) / pulseFullVolume),
		})
	}
	return inputs
}

// pactlSinkInputFor picks the stream pid plays, which is the app's own.
func pactlSinkInputFor(inputs []pactlSinkInput, pid int) (pactlSinkInput, bool) {
	for _, input := range inputs {
		if input.PID == pid {
			return input, true
		}
	}
	return pactlSinkInput{}, false
}

// pactlSinkInputByIndex finds the stream the link is following by the index it
// cached, which stops finding anything once that stream has ended, since pactl
// only lists the streams that still play.
func pactlSinkInputByIndex(inputs []pactlSinkInput, index int) (pactlSinkInput, bool) {
	for _, input := range inputs {
		if input.Index == index {
			return input, true
		}
	}
	return pactlSinkInput{}, false
}

// A pwDumpObject is one object `pw-dump` prints, read only as far as the app
// has use for it. Some objects carry no info at all, hence the pointer.
type pwDumpObject struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info *struct {
		Props map[string]any `json:"props"`
	} `json:"info"`
}

// pwDumpStreamID finds the node whose id wpctl takes for the stream pid plays.
//
// A node does not always name the process that plays it: a pw-cat or pw-play
// stream leaves application.process.id to the client object it belongs to and
// points there only with client.id, so the clients' processes are read first.
// Nodes that name their process themselves are read as well, since many
// programs set the property on the node.
func pwDumpStreamID(data []byte, pid int) (int, bool) {
	var objects []pwDumpObject
	if err := json.Unmarshal(data, &objects); err != nil {
		return 0, false
	}

	owners := make(map[int]int, len(objects))
	for _, object := range objects {
		if object.Type != "PipeWire:Interface:Client" || object.Info == nil {
			continue
		}
		if owner, ok := wholeNumber(object.Info.Props["application.process.id"]); ok {
			owners[object.ID] = owner
		}
	}

	for _, object := range objects {
		if object.Type != "PipeWire:Interface:Node" || object.Info == nil {
			continue
		}
		if object.Info.Props["media.class"] != pipewirePlaybackClass {
			continue
		}
		if owner, ok := wholeNumber(object.Info.Props["application.process.id"]); ok && owner == pid {
			return object.ID, true
		}
		if client, ok := wholeNumber(object.Info.Props["client.id"]); ok && owners[client] == pid {
			return object.ID, true
		}
	}
	return 0, false
}

// wpctlVolume reads the volume `wpctl get-volume` prints for a node, such as
// "Volume: 0.42" or, for a muted stream, "Volume: 0.42 [MUTED]".
//
// wpctl exits zero with nothing printed when the node has ended, so a volume
// that cannot be read here is also how a finished stream is noticed.
func wpctlVolume(out []byte) (float64, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "Volume:")
	if !ok {
		return 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	volume, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return clamp01(volume), true
}

// percentOf is an amplitude as the whole percent `pactl set-sink-input-volume`
// takes for a volume. Percent is the coarsest step the app's volume goes
// through, so a stream ends up within half a percent of what the app asked for.
func percentOf(volume float64) int {
	return int(math.Round(clamp01(volume) * 100))
}

// clamp01 keeps a volume the mixer reports inside the range the app works in,
// since both PulseAudio and PipeWire allow louder than full volume.
func clamp01(volume float64) float64 {
	return min(max(volume, 0), 1)
}

// wholeNumber reads a whole number that a mixer prints either as a number, as
// pw-dump does, or as a string, as pactl does. Anything else, and anything that
// is not a process id, reads as no number at all.
func wholeNumber(value any) (int, bool) {
	switch held := value.(type) {
	case float64:
		if held > 0 {
			return int(held), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(held)); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}
