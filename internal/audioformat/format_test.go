package audioformat

import (
	"bytes"
	"testing"
)

// Leading ID3 artwork must not obscure the real container when exporting or
// retaining a complete file, and an incomplete tag must not invent a format.
func TestProbeSkipsID3(t *testing.T) {
	for _, prefix := range [][]byte{nil, []byte("ID3\x04\x00\x00\x00\x00\x00\x03abc")} {
		data := append(append([]byte(nil), prefix...), []byte("fLaCpayload")...)
		if kind := Probe(bytes.NewReader(data)); kind != "flac" {
			t.Fatal("leading tag hid the container", kind)
		}
	}
	if kind := Probe(bytes.NewReader([]byte("ID3\x04\x00\x00\x00\x00\x00\x03"))); kind != "" {
		t.Fatal("incomplete tag treated as audio", kind)
	}
}
