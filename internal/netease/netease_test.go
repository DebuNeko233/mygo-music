package netease

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// An HTTP error is not a successful API response even if its body says 200.
func TestHTTPErrorIsNotAnAPIResult(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader(`{"code":200}`)), Header: make(http.Header)}, nil
	})}
	var out any
	if err := do(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/api/test"}, Header: make(http.Header)}, &out); err == nil {
		t.Fatal("accepted an HTTP failure as a result")
	}
}

// A playlist longer than a thousand songs fills in every song it lists by
// id: its page used to stop at the thousand NetEase lists in full.
func TestPlaylistDetailFillsEveryTrack(t *testing.T) {
	const listed, total = 1000, 1200
	var tracks []Track
	var trackIDs []TrackID
	for i := 1; i <= total; i++ {
		trackIDs = append(trackIDs, TrackID{ID: int64(i)})
		if i <= listed {
			tracks = append(tracks, Track{ID: int64(i)})
		}
	}
	detail, err := json.Marshal(map[string]any{"code": 200, "playlist": map[string]any{
		"id": 7, "name": "长歌单", "trackCount": total, "tracks": tracks, "trackIds": trackIDs,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var rest []Track
	for i := listed + 1; i <= total; i++ {
		rest = append(rest, Track{ID: int64(i)})
	}
	songs, err := json.Marshal(map[string]any{"code": 200, "songs": rest})
	if err != nil {
		t.Fatal(err)
	}
	answer(t, func(req *http.Request) string {
		if strings.HasSuffix(req.URL.Path, "/playlist/detail") {
			return string(detail)
		}
		return string(songs)
	})

	pl, err := PlaylistDetail(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Tracks) != total {
		t.Fatalf("%d tracks, and the playlist lists %d", len(pl.Tracks), total)
	}
	for i, tr := range pl.Tracks {
		if tr.ID != int64(i+1) {
			t.Fatalf("track %d is %d, and the playlist keeps its order", i, tr.ID)
		}
	}
}

// A playlist whose songs come by id keeps them where it has them, past the
// thousand it lists in full, and keeps a song it holds twice twice.
func TestPlaylistDetailKeepsItsOrder(t *testing.T) {
	listed := []Track{{ID: 5}, {ID: 9}, {ID: 5}}
	trackIDs := []TrackID{{ID: 5}, {ID: 6}, {ID: 9}, {ID: 5}}
	detail, err := json.Marshal(map[string]any{"code": 200, "playlist": map[string]any{
		"id": 7, "name": "长歌单", "trackCount": len(trackIDs), "tracks": listed, "trackIds": trackIDs,
	}})
	if err != nil {
		t.Fatal(err)
	}
	answer(t, func(req *http.Request) string {
		if strings.HasSuffix(req.URL.Path, "/playlist/detail") {
			return string(detail)
		}
		return `{"code":200,"songs":[{"id":6,"name":"six"}]}`
	})

	pl, err := PlaylistDetail(7)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, t := range pl.Tracks {
		ids = append(ids, t.ID)
	}
	if want := []int64{5, 6, 9, 5}; !slices.Equal(ids, want) {
		t.Errorf("tracks %v, want %v", ids, want)
	}
}

// The songs a playlist lists by id are the ones it did not list in full, in
// its own order.
func TestMissingTracksFollowsPlaylistOrder(t *testing.T) {
	var trackIDs []TrackID
	for _, id := range []int64{5, 6, 7, 8} {
		trackIDs = append(trackIDs, TrackID{ID: id})
	}
	got := missingTracks([]Track{{ID: 5}, {ID: 6}}, trackIDs)
	want := []int64{7, 8}
	if !slices.Equal(got, want) {
		t.Errorf("missing %v, want %v", got, want)
	}
}

func TestPlayableFollowsPrivilege(t *testing.T) {
	tracks := []Track{{ID: 1, Fee: 1}, {ID: 2, Fee: 8}, {ID: 3, Fee: 8}, {ID: 4, Fee: 1}}
	if tracks[0].Playable() || !tracks[1].Playable() {
		t.Fatal("without privileges, VIP songs do not play and free ones do")
	}
	withPrivileges(tracks, []Privilege{
		{ID: 1, Pl: 320000},           // VIP song, the listener subscribes
		{ID: 2, Pl: 0},                // nothing to play
		{ID: 3, St: -200, Pl: 128000}, // taken down
	})
	want := []bool{true, false, false, false}
	for i, tr := range tracks {
		if tr.Playable() != want[i] {
			t.Errorf("track %d: playable %v", tr.ID, tr.Playable())
		}
	}
}
