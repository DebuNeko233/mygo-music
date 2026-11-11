package netease

import (
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// sentLog is a request to the weblog: where it went, and what it sealed.
type sentLog struct {
	host   string
	path   string
	sealed string
}

// captureLog answers the next weblog request with code 200, and keeps it.
func captureLog(t *testing.T) *sentLog {
	t.Helper()
	sent := &sentLog{}
	answer(t, func(req *http.Request) string {
		// The desktop transport identifies this product separately from its eapi headers.
		if got := req.Header.Get("MG-Product-Name"); got != "music" {
			t.Errorf("MG-Product-Name is %q, want music", got)
		}
		sent.host = req.URL.Host
		sent.path = req.URL.Path
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if form, err := url.ParseQuery(string(body)); err == nil {
			sent.sealed = form.Get("params")
		}
		return `{"code":200}`
	})
	return sent
}

// log unseals the one log the request carried.
func (s *sentLog) log(t *testing.T) (action string, fields map[string]any) {
	t.Helper()
	if want := "clientlogdep.music.163.com"; s.host != want {
		t.Fatalf("posted to host %q, want %q", s.host, want)
	}
	if want := "/api/feedback/weblog"; s.path != want {
		t.Fatalf("posted to %q, want %q", s.path, want)
	}
	var data struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal([]byte(unseal(t, s.sealed)), &data); err != nil {
		t.Fatalf("logs: %v", err)
	}
	var logs []struct {
		Action string         `json:"action"`
		JSON   map[string]any `json:"json"`
	}
	if err := json.Unmarshal([]byte(data.Logs), &logs); err != nil {
		t.Fatalf("logs %q: %v", data.Logs, err)
	}
	if len(logs) != 1 {
		t.Fatalf("%d logs, want one", len(logs))
	}
	return logs[0].Action, logs[0].JSON
}

// A song that begins playing is announced as it starts, which NetEase's
// players do as one song gives way to another, before what becomes of that
// play is known. The app announced nothing.
func TestReportPlayStartSendsAStartLog(t *testing.T) {
	sent := captureLog(t)
	const id = 3448738711
	if err := ReportPlayStart(id, PlaySource{Source: "list", ID: 42, Alg: "itembased"}); err != nil {
		t.Fatal(err)
	}
	action, fields := sent.log(t)
	if action != "startplay" {
		t.Fatalf("logged %q, want a startplay", action)
	}
	if got := fields["id"]; got != float64(id) {
		t.Errorf("announces song %v, want %d", got, id)
	}
	if fields["type"] != "song" {
		t.Errorf("type is %v, want song", fields["type"])
	}
	if _, ok := fields["time"]; ok {
		t.Errorf("a startplay carries no time, sent %v", fields["time"])
	}
	if fields["mainsite"] != "1" || fields["mainsiteWeb"] != "1" || fields["source"] != "list" || fields["sourceId"] != "42" || fields["alg"] != "itembased" {
		t.Errorf("startplay fields %+v", fields)
	}
}

// Unknown attribution is omitted instead of sending empty source fields.
func TestReportPlayStartOmitsUnknownSource(t *testing.T) {
	sent := captureLog(t)
	if err := ReportPlayStart(7, PlaySource{}); err != nil {
		t.Fatal(err)
	}
	_, fields := sent.log(t)
	for _, key := range []string{"source", "sourceId", "alg"} {
		if _, ok := fields[key]; ok {
			t.Errorf("unknown %s was sent as %v", key, fields[key])
		}
	}
}

// A song the listener leaves is reported to the weblog NetEase's players
// report to, with the position it reached and how it ended: NetEase files
// songs there under what they played lately, with how long they heard them,
// and the app reported nothing.
func TestReportPlaySendsAPlayLog(t *testing.T) {
	sent := captureLog(t)
	const id = 3448738711
	if err := ReportPlay(id, 92*time.Second, MovedOn, PlaySource{}); err != nil {
		t.Fatal(err)
	}
	action, fields := sent.log(t)
	if action != "play" || fields["type"] != "song" || fields["end"] != MovedOn {
		t.Fatalf("logged %q %+v, want a song the listener moved on from", action, fields)
	}
	if got := fields["id"]; got != float64(id) {
		t.Errorf("log is of song %v, want %d", got, id)
	}
	if got := fields["time"]; got != float64(92) {
		t.Errorf("log plays 92 seconds of the song, sent %v", got)
	}
	if got := fields["mainsite"]; got != "1" {
		t.Errorf("mainsite is %#v, want the string \"1\"", got)
	}
}

// unseal opens an eapi request as the client seals it, and returns the JSON
// it carries.
func unseal(t *testing.T, sealed string) string {
	t.Helper()
	raw, err := hex.DecodeString(sealed)
	if err != nil || len(raw)%aes.BlockSize != 0 {
		t.Fatalf("sealed %q is not whole blocks: %v", sealed, err)
	}
	block, err := aes.NewCipher([]byte(eapiKey))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], raw[i:i+aes.BlockSize])
	}
	if pad := int(out[len(out)-1]); pad >= 1 && pad <= aes.BlockSize {
		out = out[:len(out)-pad]
	}
	parts := strings.Split(string(out), "-36cd479b6b5-")
	if len(parts) != 3 {
		t.Fatalf("sealed %q holds no path, JSON and digest", string(out))
	}
	return parts[1]
}
