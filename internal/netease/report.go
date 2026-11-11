package netease

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// The ends of a play, as NetEase's own players name them.
const (
	Playthrough = "playend"
	MovedOn     = "ui"
	Interrupted = "interrupt"
	Exception   = "exception"
)

// PlaySource says which list supplied a song. Empty fields are omitted.
type PlaySource struct {
	Source string
	ID     int64
	Alg    string
}

// ReportPlayStart announces a song only after its audio actually starts.
func ReportPlayStart(id int64, source PlaySource) error {
	data := playFields(id, source)
	data["mainsite"] = "1"
	data["mainsiteWeb"] = "1"
	return sendLog("startplay", data)
}

// ReportPlay tells NetEase how long the song was heard and why its play ended.
func ReportPlay(id int64, played time.Duration, end string, source PlaySource) error {
	data := playFields(id, source)
	data["download"] = 0
	data["end"] = end
	data["time"] = int64(math.Round(played.Seconds()))
	data["wifi"] = 0
	data["mainsite"] = "1"
	data["mainsiteWeb"] = "1"
	return sendLog("play", data)
}

func playFields(id int64, source PlaySource) map[string]any {
	data := map[string]any{"id": id, "type": "song"}
	if source.Source != "" {
		data["source"] = source.Source
	}
	if source.ID != 0 {
		data["sourceId"] = strconv.FormatInt(source.ID, 10)
	}
	if source.Alg != "" {
		data["alg"] = source.Alg
	}
	return data
}

// sendLog posts to the dedicated weblog host, not the ordinary eapi gateway.
func sendLog(action string, data map[string]any) error {
	// Every desktop log entry carries its own creation time in milliseconds.
	data["logtime"] = time.Now().UnixMilli()
	logs, err := json.Marshal([]map[string]any{{"action": action, "json": data}})
	if err != nil {
		return err
	}
	const path = "/api/feedback/weblog"
	return eapiOKEndpoint("https://clientlogdep.music.163.com"+path, path, map[string]any{"logs": string(logs)})
}
