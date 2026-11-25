/*
验证 macOS 文件时间戳更新失败时仍按最近读取记录保护缓存。
*/

package diskcache

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A recent snapshot read is protected from eviction even when the filesystem rejects its timestamp update.
func TestSnapshotEvictionUsesLastReadWhenTimestampUpdateFails(t *testing.T) {
	s := New(t.TempDir(), 400)
	defer s.Close()
	payload := bytes.Repeat([]byte("a"), 100)
	s.Put("a", "played", payload, s.Epoch())
	s.Put("a", "other", payload, s.Epoch())
	for i, key := range []string{"played", "other"} {
		at := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(filepath.Join(s.dir, name("a", key)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	recent := filepath.Join(s.dir, name("a", "played"))
	if err := unix.Chflags(recent, unix.UF_IMMUTABLE); err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			t.Skip("temporary filesystem does not support immutable files")
		}
		t.Fatal(err)
	}
	unlock := func() {
		if err := unix.Chflags(recent, 0); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	}
	t.Cleanup(unlock)

	if got, _ := s.Read("a", "played"); !bytes.Equal(got, payload) {
		t.Fatal("could not read the immutable snapshot")
	}
	info, err := os.Stat(recent)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(time.Unix(1, 0)) {
		t.Fatal("fixture allowed the disk timestamp to change")
	}
	unlock()

	s.Put("a", "new", payload, s.Epoch())
	if got, _ := s.Read("a", "played"); !bytes.Equal(got, payload) {
		t.Fatal("evicted the more recently read snapshot")
	}
	if got, _ := s.Read("a", "other"); got != nil {
		t.Fatal("retained the least recently read snapshot")
	}
}
