package player

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Include indexing, decoding and spectrum measurement for each supported local format.
func BenchmarkLocalAudioDecode(b *testing.B) {
	for _, name := range []string{"seek.mp3", "seek.flac", "seek.ogg", "seek.aac", "seek.m4a", "seek-alac.m4a", "seek-he.aac", "seek-he.m4a", "seek.3gp", "seek.3g2", "pcm.wav"} {
		b.Run(name, func(b *testing.B) {
			path := filepath.Join("testdata", name)
			if name == "pcm.wav" {
				path = filepath.Join(b.TempDir(), name)
				if err := os.WriteFile(path, makeWAV(1, 16, 8000, func(frame int) int16 { return int16(frame*97%30000 - 15000) }), 0600); err != nil {
					b.Fatal(err)
				}
			}
			path, err := filepath.Abs(path)
			if err != nil {
				b.Fatal(err)
			}
			buf := make([]float32, 4096)
			b.ReportAllocs()
			for b.Loop() {
				_, s, err := openLocal(path)
				if err != nil {
					b.Fatal(err)
				}
				for {
					n, err := s.Read(buf)
					if err == io.EOF {
						break
					}
					if err != nil || n == 0 {
						s.Close()
						b.Fatal(fmt.Errorf("decode: %d samples, %v", n, err))
					}
				}
				s.Close()
			}
		})
	}
}
