package netease

import (
	"encoding/json"
	"math"
	"time"
)

// The ends of a play, as NetEase's own players name them: the song played to
// its end, the listener went on to another, and playback stopped in it.
const (
	Playthrough = "playend"
	MovedOn     = "ui"
	Interrupted = "interrupt"
)

// ReportPlay tells NetEase which song the listener heard, for how long, and
// how they left it, which files it under what they played lately on every
// device they are signed in on and counts toward how much they listened. The
// players NetEase ships post this log to the same weblog when a song ends,
// with the position the song reached; the app posted none.
func ReportPlay(id int64, played time.Duration, end string) error {
	logs, err := json.Marshal([]map[string]any{{
		"action": "play",
		"json": map[string]any{
			"download":    0,
			"end":         end,
			"id":          id,
			"time":        int64(math.Round(played.Seconds())),
			"type":        "song",
			"wifi":        0,
			"mainsite":    "1",
			"mainsiteWeb": "1",
		},
	}})
	if err != nil {
		return err
	}
	return eapiOK("/api/feedback/weblog", map[string]any{"logs": string(logs)})
}
