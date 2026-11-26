/*
验证迁移覆盖所有支持的音频格式及元数据，同时保留用户自有文件。
*/

package move

import (
	"encoding/json"
	"fmt"
	"netease-music-mygo/internal/netease"
	"path/filepath"
	"testing"
)

// Relocating the cache moves every supported canonical audio format and its metadata, while leaving personal files alone.
func TestMoveAllSupportedAudioFormats(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	var names []string
	for i, ext := range []string{"mp3", "flac", "wav", "ogg", "aac", "m4a"} {
		key := fmt.Sprintf("%d-exhigh", i+1)
		meta, _ := json.Marshal(netease.Stream{Type: ext, Size: 5, Level: "exhigh"})
		write(t, filepath.Join(layout(source).Songs, key+"."+ext), []byte("music"))
		write(t, filepath.Join(layout(source).Songs, key+".json"), meta)
		names = append(names, key+"."+ext, key+".json")
	}
	write(t, filepath.Join(layout(source).Songs, "personal.wav"), []byte("keep"))
	warning, err := MoveLayoutWithOptions(layout(source), target, true, false, func() error { return nil }, Options{})
	if err != nil || warning != nil {
		t.Fatalf("move: %v %v", err, warning)
	}
	for _, name := range names {
		exists(t, filepath.Join(layout(source).Songs, name), false)
		exists(t, filepath.Join(layout(target).Songs, name), true)
	}
	exists(t, filepath.Join(layout(source).Songs, "personal.wav"), true)
	exists(t, filepath.Join(layout(target).Songs, "personal.wav"), false)
}
