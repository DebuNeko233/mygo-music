package netease

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// An unacknowledged NCBL upload is a failed report, even when the server
// responds with HTTP 200.

// The desktop BI logger sends separate PLV and PLD encrypted uploads and
// accepts the logger's encrypted multipart payload.
func TestDawnUploadAcknowledgesEachRecord(t *testing.T) {
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "clientlog3.music.163.com" || req.URL.Path != "/api/clientlog/encrypt/upload" {
			t.Fatalf("unexpected upload URL %s", req.URL)
		}
		_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		form := multipart.NewReader(req.Body, params["boundary"])
		part, err := form.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() != "file" || !strings.HasPrefix(part.FileName(), "op_") {
			t.Fatalf("invalid log upload file: %s %s", part.FormName(), part.FileName())
		}
		payload, err := io.ReadAll(part)
		if err != nil || string(payload[:4]) != "NCBL" {
			t.Fatalf("invalid NCBL payload: %v", err)
		}
		body := `{"code":200,"data":{"successfiles":["` + part.FileName() + `"]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	source := PlaySource{Source: "list", ID: 3778678}
	if err := ReportDawnStart(Track{ID: 7, Duration: 100000}, 320, "exhigh", source); err != nil {
		t.Fatal(err)
	}
	if err := ReportDawnEnd(Track{ID: 7, Duration: 100000}, 35*time.Second, "playend", 320, "exhigh", source); err != nil {
		t.Fatal(err)
	}
}

func TestDawnUploadRequiresFilenameAcknowledgement(t *testing.T) {
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"successfiles":[]}}`)), Header: make(http.Header)}, nil
	})
	if err := ReportDawnStart(Track{ID: 7}, 320, "exhigh", PlaySource{}); err == nil {
		t.Fatal("an unacknowledged upload was treated as sent")
	}
}
