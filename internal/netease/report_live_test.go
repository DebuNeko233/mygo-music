package netease

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// Explicit live verification compares the server's recent-play list before and
// after a startplay report. It requires LIVE and NM_SESSION and never prints credentials.
func TestPlayReportVerificationLive(t *testing.T) {
	if os.Getenv("LIVE") == "" || os.Getenv("NM_SESSION") == "" || os.Getenv("NM_LIVE_MUTATE") == "" {
		t.Skip()
	}
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	useSession(t, os.Getenv("NM_SESSION"))
	before, err := RecentPlays(5)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReportPlayStart(347230, PlaySource{Source: "list", ID: 3778678}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	after, err := RecentPlays(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) == 0 || after[0].Track.ID != 347230 {
		t.Fatalf("startplay was not the newest server-side recent play: %+v", after)
	}
	beforeAt := time.Time{}
	if len(before) != 0 && before[0].Track.ID == 347230 {
		beforeAt = before[0].At
	}
	if !after[0].At.After(beforeAt) {
		t.Fatalf("recent-play timestamp did not advance: before %v, after %v", beforeAt, after[0].At)
	}
}
