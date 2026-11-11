package netease

import (
	"os"
	"testing"
)

func livePlayCount(id int64) (int, error) {
	profile, err := Account()
	if err != nil {
		return 0, err
	}
	var res struct {
		Code    int `json:"code"`
		AllData []struct {
			PlayCount int `json:"playCount"`
			Song      struct {
				ID int64 `json:"id"`
			} `json:"song"`
		} `json:"allData"`
	}
	if err := eapiPost("/api/v1/play/record", map[string]any{"uid": profile.UserID, "type": 0}, &res); err != nil {
		return 0, err
	}
	for _, p := range res.AllData {
		if p.Song.ID == id {
			return p.PlayCount, nil
		}
	}
	return 0, nil
}

func TestPlayCountsLive(t *testing.T) {
	if os.Getenv("LIVE") == "" || os.Getenv("NM_SESSION") == "" {
		t.Skip()
	}
	useSession(t, os.Getenv("NM_SESSION"))
	count, err := livePlayCount(1892257906)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("track 1892257906 listening count: %d", count)
}
