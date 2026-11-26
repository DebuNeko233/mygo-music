/*
验证缓存容量统计、淘汰和清理覆盖全部支持的音频格式及元数据。
*/

package player

import (
	"encoding/json"
	"netease-music-mygo/internal/features/netease"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Quota eviction and Clear account for the added formats, including their sidecars.
func TestCacheQuotaIncludesAllAudioFormats(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	formats := []string{"mp3", "flac", "wav", "ogg", "aac", "m4a"}
	for i, ext := range formats {
		key := cacheKey(int64(i+1), "exhigh")
		path := filepath.Join(c.dir, key+"."+ext)
		if err := os.WriteFile(path, make([]byte, 100), 0600); err != nil {
			t.Fatal(err)
		}
		metadata, _ := json.Marshal(netease.Stream{Size: 100, Type: ext, Level: "exhigh"})
		if err := os.WriteFile(filepath.Join(c.dir, key+".json"), metadata, 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(1000+i), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if c.Size() != 600 {
		t.Fatalf("size: %d", c.Size())
	}
	c.SetLimit(300)
	if c.Size() > 300 {
		t.Fatal("quota ignored added formats")
	}
	for i, ext := range formats[:3] {
		if _, err := os.Stat(filepath.Join(c.dir, cacheKey(int64(i+1), "exhigh")+"."+ext)); !os.IsNotExist(err) {
			t.Fatalf("LRU file survived: %s %v", ext, err)
		}
	}
	c.Clear()
	entries, err := os.ReadDir(c.dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("clear left sidecars or audio: %v %v", entries, err)
	}
}
