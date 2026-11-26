/*
验证并发会话保存保留最新队列，并拒绝与快照不匹配的检查点。
*/

package session

import (
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/utils/userdata"
)

// Multiple queue/order changes can race the disk worker; the final save
// must win and a legacy session must still be readable without a token.
func TestSessionKeepsLatestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	var saver Saver
	legacy := player.Session{Queue: []netease.Track{{ID: 5}}, Order: []int{0}, Position: 2000}
	if !userdata.WriteJSON(path, legacy) {
		t.Fatal("could not create legacy fixture")
	}
	if s, ok := Read(path); !ok || len(s.Queue) != 1 || s.Queue[0].ID != 5 || s.Position != 2000 {
		t.Fatalf("legacy session not restored: %+v", s)
	}
	for i := range 20 {
		s := player.Session{Queue: []netease.Track{{ID: int64(i + 1)}, {ID: 99}}, Order: []int{0, 1}, Position: int64(i * 1000)}
		saver.Save(path, s, uint64(i))
	}
	saver.Wait()
	s, ok := Read(path)
	if !ok || len(s.Queue) != 2 || s.Queue[0].ID != 20 || s.Position != 19000 || s.Shuffle {
		t.Fatalf("latest checkpoint was not saved: %+v", s)
	}
}

// A stale or invalid progress file cannot override the saved queue's selection.
func TestReadRejectsInvalidCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkpoint sessionCheckpoint
	}{
		{"other queue", sessionCheckpoint{Token: "old", At: 0, Position: 8000}},
		{"negative index", sessionCheckpoint{Token: "current", At: -1, Position: 8000}},
		{"past queue", sessionCheckpoint{Token: "current", At: 1, Position: 8000}},
		{"negative position", sessionCheckpoint{Token: "current", At: 0, Position: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.json")
			base := sessionFile{Session: player.Session{Queue: []netease.Track{{ID: 5}}, Order: []int{0}, Position: 2000}, Token: "current"}
			if !userdata.WriteJSON(path, base) || !userdata.WriteJSON(checkpointPath(path), tc.checkpoint) {
				t.Fatal("could not create fixture")
			}
			got, ok := Read(path)
			if !ok || got.At != 0 || got.Position != 2000 || len(got.Queue) != 1 || got.Queue[0].ID != 5 {
				t.Fatalf("invalid checkpoint replaced snapshot: %+v", got)
			}
		})
	}
}
