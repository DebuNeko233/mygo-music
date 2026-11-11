package netease

import (
	"os"
	"testing"
	"time"
)

// Explicit live verification compares the server's recent-play list before and
// after a startplay report. It requires LIVE and NM_SESSION and never prints credentials.
func TestPlayReportVerificationLive(t *testing.T) {
	if os.Getenv("LIVE") == "" || os.Getenv("NM_SESSION") == "" {
		t.Skip()
	}
	useSession(t, os.Getenv("NM_SESSION"))
	before, err := RecentPlays(5)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range before {
		t.Logf("before %d %s %s", p.Track.ID, p.At.Format(time.RFC3339), p.Device)
	}
	if err := ReportPlayStart(347230, PlaySource{Source: "list", ID: 3778678}); err != nil {
		t.Fatal(err)
	}
	if err := ReportPlay(347230, 20*time.Second, Playthrough, PlaySource{Source: "list", ID: 3778678}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	after, err := RecentPlays(5)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range after {
		t.Logf("after %d %s %s", p.Track.ID, p.At.Format(time.RFC3339), p.Device)
	}
}
