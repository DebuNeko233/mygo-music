package lyrics

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// An opt-in, read-only pass through actual cached lyrics exercises both LRC
// fallback and the selected LRC/YRC stream. It checks structural invariants,
// not recognition accuracy; optional candidates are for manual review only.
// No cache filenames, account identifiers or complete songs enter the report.
func TestCreditsCachedLyrics(t *testing.T) {
	dir := os.Getenv("NM_LYRICS_CACHE")
	if dir == "" {
		t.Skip("set NM_LYRICS_CACHE to a browse-v1 directory")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.entry"))
	if err != nil {
		t.Fatal(err)
	}
	type candidate struct {
		Record int
		Stream string
		Index  int
		Text   string
		Kind   LineKind
		Timed  bool
	}
	report := struct {
		Records, WithYRC, LRCLines, PlaybackLines int
		Candidates                                []candidate
	}{}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) < 48 || string(b[:8]) != "MYCACHE1" {
			t.Fatal("invalid cache header")
		}
		sum := sha256.Sum256(b[48:])
		if !bytes.Equal(sum[:], b[16:48]) {
			t.Fatal("cache checksum mismatch")
		}
		var kept struct{ Original, Translation, Words string }
		if json.Unmarshal(b[48:], &kept) != nil || kept.Original == "" && kept.Words == "" {
			continue
		}
		report.Records++
		if kept.Words != "" {
			report.WithYRC++
		}
		for _, stream := range []string{"lrc", "playback"} {
			words := ""
			if stream == "playback" {
				words = kept.Words
			}
			lines, display := BuildDisplay(kept.Original, kept.Translation, words)
			again, rebuilt := BuildDisplay(kept.Original, kept.Translation, words)
			if !reflect.DeepEqual(display.Lines, rebuilt.Lines) || !reflect.DeepEqual(lines, again) {
				t.Fatalf("record %d (%s): classification is not stable", report.Records, stream)
			}
			if stream == "lrc" {
				report.LRCLines += len(display.Lines)
			} else {
				report.PlaybackLines += len(lines)
			}
			for i, l := range display.Lines {
				if l.Kind == Credit && len(l.Words) != 0 {
					t.Fatalf("record %d (%s): credit has word highlights", report.Records, stream)
				}
				if l.Kind != Sung || strings.ContainsAny(l.Text, ":：") {
					report.Candidates = append(report.Candidates, candidate{report.Records, stream, i, l.Text, l.Kind, l.Timed})
				}
			}
			if !reflect.DeepEqual(Quiets(lines, 0, 8*time.Second), Quiets(display.Lines, 0, 8*time.Second)) {
				t.Fatalf("record %d (%s): display and playback quiet intervals differ", report.Records, stream)
			}
		}
	}
	if report.Records == 0 {
		t.Fatal("no lyric payloads found")
	}
	if path := os.Getenv("NM_LYRICS_REPORT"); path != "" {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("read %d lyric records (%d with YRC), %d LRC lines, %d playback lines; %d candidates for review",
		report.Records, report.WithYRC, report.LRCLines, report.PlaybackLines, len(report.Candidates))
}
