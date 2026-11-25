/*
验证关闭本地 M4A 流时同时释放其 HE-AAC 解码器。
*/

package player

import (
	"os"
	"path/filepath"
	"testing"
)

// Closing an M4A stream must destroy its HE-AAC codec. MyGo's MP4 decoder has
// no Close method, so this checks the ownership transfer at its codec factory.
func TestLocalM4AClosesHEAACCodec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.m4a")
	if err := os.WriteFile(path, testAudio(t, "seek-he.m4a"), 0600); err != nil {
		t.Fatal(err)
	}
	_, stream, err := openLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	local := stream.(*decoderStream)
	owned, ok := local.closer.(*ownedAACCodec)
	if !ok {
		t.Fatal("M4A codec ownership was not transferred to its stream")
	}
	codec, ok := owned.Codec.(*faadCodec)
	if !ok {
		t.Fatalf("expected HE-AAC fallback, got %T", owned.Codec)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if codec.dec != nil || owned.close != nil {
		t.Fatal("HE-AAC decoder survived stream close")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}
