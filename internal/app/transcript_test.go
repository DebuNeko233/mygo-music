package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netease-music-mygo/internal/speech"
)

// The transcript kept is the one written last: the writes share a temporary
// file and land in any order, and one landing after a newer one put back
// what had been written since.
func TestTranscriptKeepsTheLatestWrite(t *testing.T) {
	isolateUserData(t)
	a := newTestApp()
	a.captions.program = 7
	a.captions.tr = &transcript{
		Model:    speech.ModelName,
		Spans:    []span{{0, time.Minute}},
		Segments: []speech.Segment{{Start: 0, End: time.Second, Text: "一"}},
	}
	first := a.transcriptWrite()
	// The episode goes on being written down before the first write lands.
	a.captions.tr.Segments = append(a.captions.tr.Segments, speech.Segment{Start: time.Second, End: 2 * time.Second, Text: "二"})
	second := a.transcriptWrite()
	if first == nil || second == nil {
		t.Fatal("nothing to write")
	}
	second()
	first()

	path, err := transcriptPath(7)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept transcript
	if err := json.Unmarshal(b, &kept); err != nil {
		t.Fatalf("the transcript is not readable: %v", err)
	}
	if len(kept.Segments) != 2 {
		t.Errorf("kept %d segments, wanted the two of the later write", len(kept.Segments))
	}
	if parts, _ := filepath.Glob(path + "*.tmp"); len(parts) != 0 {
		t.Errorf("left %v", parts)
	}
}
