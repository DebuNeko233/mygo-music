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

// ReportPlayStart tells NetEase that a song began playing, which its players
// send as one song gives way to another, before what becomes of that play is
// known.
func ReportPlayStart(id int64) error {
	return sendLog("startplay", map[string]any{"id": id, "type": "song"})
}

// ReportPlay tells NetEase which song the listener heard, for how long, and
// how they left it, which files it under what they played lately on every
// device they are signed in on and counts toward how much they listened. The
// players NetEase ships post this log when a song ends, with the position the
// song reached; the app posted none.
func ReportPlay(id int64, played time.Duration, end string) error {
	return sendLog("play", map[string]any{
		"download":    0,
		"end":         end,
		"id":          id,
		"time":        int64(math.Round(played.Seconds())),
		"type":        "song",
		"wifi":        0,
		"mainsite":    "1",
		"mainsiteWeb": "1",
	})
}

// sendLog posts one action to the weblog NetEase's players report to.
func sendLog(action string, data map[string]any) error {
	logs, err := json.Marshal([]map[string]any{{"action": action, "json": data}})
	if err != nil {
		return err
	}
	return eapiOK("/api/feedback/weblog", map[string]any{"logs": string(logs)})
}
