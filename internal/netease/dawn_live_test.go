package netease

import (
	"os"
	"testing"
	"time"
)

// Only a real increase in the listener's server-side count proves that the
// desktop records were counted; an upload acknowledgement alone does not.
func TestDawnLive(t *testing.T) {
	if os.Getenv("LIVE") == "" || os.Getenv("NM_SESSION") == "" {
		t.Skip()
	}
	useSession(t, os.Getenv("NM_SESSION"))
	track := Track{ID: 1892257906, Duration: 191000, Fee: 8, Name: "ベイビーヘイロー"}
	before, err := livePlayCount(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReportDawnStart(track, 320, "exhigh", PlaySource{Source: "list", ID: 3778678}); err != nil {
		t.Fatal(err)
	}
	if err := ReportDawnEnd(track, 35*time.Second, Playthrough, 320, "exhigh", PlaySource{Source: "list", ID: 3778678}); err != nil {
		t.Fatal(err)
	}
	// The count is maintained asynchronously after the upload acknowledgement.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		after, err := livePlayCount(track.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("listening count: %d -> %d", before, after)
		if after > before {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("listening count did not increase from %d", before)
}
