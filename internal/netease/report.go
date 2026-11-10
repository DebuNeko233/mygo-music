package netease

import "encoding/json"

// ReportPlay tells NetEase that the listener played a song, which files it
// under what they played lately on every device they are signed in on, and
// counts toward how much they listened.
func ReportPlay(id int64) error {
	logs, err := json.Marshal([]map[string]any{{
		"action": "play",
		"json": map[string]any{
			"download": 0,
			"end":      "playend",
			"id":       id,
			"sourceId": 0,
			"time":     0,
			"type":     "song",
			"wifi":     0,
			"source":   "list",
			"mainsite": 1,
			"content":  "",
		},
	}})
	if err != nil {
		return err
	}
	return eapiOK("/api/feedback/weblog", map[string]any{"logs": string(logs)})
}
