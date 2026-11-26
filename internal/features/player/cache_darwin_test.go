/*
验证 macOS 文件不可删除或时间戳更新失败时，缓存统计与淘汰仍保持一致。
*/

package player

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func immutableCacheFile(t *testing.T, path string) func() {
	t.Helper()
	if err := unix.Chflags(path, unix.UF_IMMUTABLE); err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			t.Skip("temporary filesystem does not support immutable files")
		}
		t.Fatal(err)
	}
	unlock := func() {
		t.Helper()
		if err := unix.Chflags(path, 0); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	}
	t.Cleanup(unlock)
	return unlock
}

// A failed eviction keeps both its counted bytes and its sidecar, so pruning removes the next available copies.
func TestCacheEvictionStillCountsUndeletableAudio(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	for i, size := range []int{100, 60, 40} {
		id := int64(i + 1)
		seedCache(t, c, id, make([]byte, size))
		at := time.Unix(id, 0)
		path := filepath.Join(c.dir, cacheKey(id, "exhigh")+".mp3")
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(c.dir, "1-exhigh.mp3")
	unlock := immutableCacheFile(t, first)

	c.SetLimit(100)
	if size := c.Size(); size != 100 {
		t.Fatalf("pruning left %d bytes on disk, want 100", size)
	}
	d, info, err := c.lookupOffline(1, "exhigh")
	if err != nil {
		t.Fatal("pruning invalidated the audio it could not delete", err)
	}
	d.Close()
	if info.Size != 100 {
		t.Fatalf("surviving sidecar reports %d bytes", info.Size)
	}
	for id := int64(2); id <= 3; id++ {
		if d, _, err := c.lookupOffline(id, "exhigh"); err == nil {
			d.Close()
			t.Errorf("retained removable copy %d", id)
		}
	}

	unlock()
	c.SetLimit(0)
	if size := c.Size(); size != 0 {
		t.Fatalf("retry left %d bytes on disk", size)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "1-exhigh.json")); !os.IsNotExist(err) {
		t.Fatalf("retry retained the evicted sound's sidecar: %v", err)
	}
}

// A played song stays recent when its disk timestamp cannot be updated.
func TestCacheEvictionUsesLastPlayWhenTimestampUpdateFails(t *testing.T) {
	c := NewCache(t.TempDir(), 1<<20)
	for id := int64(1); id <= 2; id++ {
		seedCache(t, c, id, []byte("audio"))
		at := time.Unix(id, 0)
		path := filepath.Join(c.dir, cacheKey(id, "exhigh")+".mp3")
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(c.dir, "1-exhigh.mp3")
	unlock := immutableCacheFile(t, first)
	d, _, err := c.lookupOffline(1, "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	disk, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if !disk.ModTime().Equal(time.Unix(1, 0)) {
		t.Fatal("fixture allowed the disk timestamp to change")
	}
	unlock()

	c.SetLimit(5)
	d, _, err = c.lookupOffline(1, "exhigh")
	if err != nil {
		t.Fatal("evicted the more recently played audio", err)
	}
	d.Close()
	if _, err := os.Stat(filepath.Join(c.dir, "2-exhigh.mp3")); !os.IsNotExist(err) {
		t.Fatalf("retained the least recently played audio: %v", err)
	}
}
