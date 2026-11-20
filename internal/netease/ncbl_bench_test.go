package netease

import (
	"bytes"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Independent playback reports must retain the compressed format when the
// shared encoder is used concurrently, including empty bodies.
func TestNCBLZstdReuse(t *testing.T) {
	bodies := [][]byte{nil, []byte("one report"), bytes.Repeat([]byte("many records\n"), 100)}
	want := make([][]byte, len(bodies))
	for i, body := range bodies {
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = encoder.EncodeAll(body, nil)
		encoder.Close()
	}
	var wg sync.WaitGroup
	for range 100 {
		for i, body := range bodies {
			wg.Go(func() {
				got, err := ncblZstd(body)
				if err != nil || !bytes.Equal(got, want[i]) {
					t.Errorf("report %d: compressed output changed: %v", i, err)
				}
			})
		}
	}
	wg.Wait()
}

func BenchmarkNCBLZstd(b *testing.B) {
	body := bytes.Repeat([]byte(`{"resourceId":"123456","type":"song","time":240,"bitrate":320000,"source":"local","sourceId":"0"},`), 8)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ncblZstd(body); err != nil {
			b.Fatal(err)
		}
	}
}
