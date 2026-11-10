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

// A song the listener leaves is reported to the weblog NetEase's players
// report to, with the position it reached and how it ended: NetEase files
// songs there under what the listener played lately, with how long they
// heard them, and the app reported nothing.
func TestReportPlaySendsAPlayLog(t *testing.T) {
	var path string
	var sealed string
	answer(t, func(req *http.Request) string {
		path = req.URL.Path
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if form, err := url.ParseQuery(string(body)); err == nil {
			sealed = form.Get("params")
		}
		return `{"code":200}`
	})

	const id = 3448738711
	if err := ReportPlay(id, 92*time.Second, MovedOn); err != nil {
		t.Fatal(err)
	}
	if want := "/eapi/feedback/weblog"; path != want {
		t.Fatalf("posted to %q, want %q", path, want)
	}
	var data struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal([]byte(unseal(t, sealed)), &data); err != nil {
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
		t.Fatalf("%d logs, want one play", len(logs))
	}
	play := logs[0]
	if play.Action != "play" || play.JSON["type"] != "song" || play.JSON["end"] != MovedOn {
		t.Fatalf("log %+v, want a song the listener moved on from", play)
	}
	if got := play.JSON["id"]; got != float64(id) {
		t.Errorf("log is of song %v, want %d", got, id)
	}
	if got := play.JSON["time"]; got != float64(92) {
		t.Errorf("log plays 92 seconds of the song, sent %v", got)
	}
	if got := play.JSON["mainsite"]; got != "1" {
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
