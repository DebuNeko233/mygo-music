package netease

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"sync"
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
const playLogDomain = "https://clientlog.music.163.com"

// The recent-play weblog currently accepts the macOS eapi identity. Keep it
// scoped to this endpoint: other desktop requests must retain their own identity.
var playLogClient = map[string]string{
	"os":      "osx",
	"appver":  "3.1.10.5100",
	"osver":   "15.5",
	"channel": "netease",
}

// SnapshotPlayReports redirects the queued reports of the old login into a
// private jar until the next login. Reporting functions then authenticate with
// the session that created them, not whichever account the global jar holds.
func SnapshotPlayReports() func() {
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(siteURL, httpClient.Jar.Cookies(siteURL))
	reportJarMu.Lock()
	old := reportCookieJar
	reportCookieJar = func() http.CookieJar { return jar }
	reportJarMu.Unlock()
	return func() {
		reportJarMu.Lock()
		reportCookieJar = old
		reportJarMu.Unlock()
	}
}

var (
	reportJarMu     sync.RWMutex
	reportCookieJar = func() http.CookieJar { return httpClient.Jar }
)

func playCookieJar() http.CookieJar {
	reportJarMu.RLock()
	defer reportJarMu.RUnlock()
	return reportCookieJar()
}

func sendLog(action string, data map[string]any) error {
	// Every desktop log entry carries its own creation time in milliseconds.
	data["logtime"] = time.Now().UnixMilli()
	logs, err := json.Marshal([]map[string]any{{"action": action, "json": data}})
	if err != nil {
		return err
	}
	const path = "/api/feedback/weblog"
	endpoint := playLogDomain + "/eapi/feedback/weblog"
	return eapiOKEndpoint(endpoint, path, map[string]any{"logs": string(logs)})
}
