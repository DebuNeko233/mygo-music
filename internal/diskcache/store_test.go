package diskcache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A successful snapshot survives restart, while another account cannot read it.
func TestSnapshotRestartAndAccountIsolation(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1<<20)
	s.Put("alice", "playlist/1", []byte("songs"), s.Epoch())
	restarted := New(dir, 1<<20)
	b, at := restarted.Read("alice", "playlist/1")
	if string(b) != "songs" || at.IsZero() {
		t.Fatal("lost snapshot on restart")
	}
	if b, _ := restarted.Read("bob", "playlist/1"); b != nil {
		t.Fatal("another account read private data")
	}
}

// Clearing invalidates downloads captured before it, without clearing another account.
func TestClearRejectsLateWrite(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	epoch := s.Epoch()
	s.Put("bob", "list", []byte("b"), epoch)
	s.Put("alice", "list", []byte("a"), epoch)
	done := make(chan struct{})
	s.Clear("alice", func() { close(done) })
	<-done
	s.Put("alice", "list", []byte("late"), epoch)
	if b, _ := s.Read("alice", "list"); b != nil {
		t.Fatal("clear was undone")
	}
	if b, _ := s.Read("bob", "list"); string(b) != "b" {
		t.Fatal("cleared another account")
	}
	s.Put("alice", "list", []byte("new"), s.Epoch())
	if b, _ := s.Read("alice", "list"); string(b) != "new" {
		t.Fatal("cannot write after clear")
	}
}

// A damaged payload and an interrupted write are cache misses, not valid empty data.
func TestCorruptionAndInterruptedWrite(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1<<20)
	s.Put("a", "list", []byte("songs"), 0)
	path := filepath.Join(dir, name("a", "list"))
	b, _ := os.ReadFile(path)
	b[len(b)-1] ^= 1
	os.WriteFile(path, b, 0600)
	if got, _ := s.Read("a", "list"); got != nil {
		t.Fatal("accepted corrupt bytes")
	}
	os.WriteFile(filepath.Join(dir, ".part-interrupted"), []byte("half"), 0600)
	s.Put("a", "list", []byte("repaired"), 0)
	if _, err := os.Stat(filepath.Join(dir, ".part-interrupted")); !os.IsNotExist(err) {
		t.Fatal("left interrupted file")
	}
	if got, _ := s.Read("a", "list"); string(got) != "repaired" {
		t.Fatal("did not recover")
	}
}

// Capacity is enforced on disk, and touching a snapshot protects it from eviction.
func TestCapacityEvictsOlderSnapshots(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 400)
	s.Put("a", "old", bytes.Repeat([]byte{1}, 100), 0)
	s.Put("a", "keep", bytes.Repeat([]byte{2}, 100), 0)
	// Explicit filesystem times avoid depending on clock resolution or sleeps.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == name("a", "old") {
			os.Chtimes(filepath.Join(dir, e.Name()), time.Unix(1, 0), time.Unix(1, 0))
		}
	}
	s.Read("a", "keep")
	s.Put("a", "new", bytes.Repeat([]byte{3}, 100), 0)
	if s.Size() > 400 {
		t.Fatal("cache exceeded limit")
	}
	if got, _ := s.Read("a", "old"); got != nil {
		t.Fatal("retained oldest entry")
	}
	if got, _ := s.Read("a", "keep"); got == nil {
		t.Fatal("dropped recently touched entry")
	}
	if got, _ := s.Read("a", "new"); got == nil {
		t.Fatal("dropped new entry")
	}
}

// A newly committed entry below the hard limit survives even when it exceeds the batch eviction target.
func TestCapacityRetainsLargeNewSnapshot(t *testing.T) {
	s := New(t.TempDir(), 400)
	defer s.Close()
	s.Put("a", "old", bytes.Repeat([]byte{1}, 100), 0)
	s.Put("a", "new", bytes.Repeat([]byte{2}, 300), 0)
	if b, _ := s.Read("a", "new"); len(b) != 300 || s.Size() > 400 {
		t.Fatal("batch eviction discarded a new entry which fits the hard limit")
	}
}

// Tied filesystem timestamps must not make a freshly committed small snapshot the first victim.
func TestCapacityRetainsNewSnapshotWithEqualTimes(t *testing.T) {
	s := New(t.TempDir(), 4000)
	defer s.Close()
	old, fresh := "old", "new"
	if name("a", old) < name("a", fresh) {
		old, fresh = fresh, old
	}
	s.Put("a", old, bytes.Repeat([]byte{1}, 230), 0)
	s.Put("a", fresh, bytes.Repeat([]byte{2}, 100), 0)
	for _, key := range []string{old, fresh} {
		if err := os.Chtimes(filepath.Join(s.dir, name("a", key)), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
	}
	s.io.Lock()
	s.limit, s.spare = 400, 0
	s.prune(0, name("a", fresh))
	s.io.Unlock()
	if b, _ := s.Read("a", fresh); len(b) != 100 {
		t.Fatal("tied timestamps evicted the new snapshot")
	}
	if s.Size() > 400 {
		t.Fatal("cache exceeded limit")
	}
}

// Protecting a large new image must still account for the non-evictable metadata index.
func TestIndexedCapacityIncludesIndexWhenProtectingContent(t *testing.T) {
	s := NewIndexed(t.TempDir(), 16<<10, "requests")
	defer s.Close()
	s.Put("requests", "cover", []byte(`{"content":"large"}`), 0)
	s.Put("content", "large", make([]byte, 16000), 0)
	if s.Size() > 16<<10 {
		t.Fatal("protected image and index exceeded the hard limit", s.Size())
	}
}

// Acceptance order wins even when an old snapshot takes longer to encode.
func TestPreparedWritesKeepAcceptanceOrder(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	old := s.Prepare("a", "list", 0)
	newer := s.Prepare("a", "list", 0)
	newer([]byte("newer"))
	old([]byte("older"))
	b, _ := s.Read("a", "list")
	if string(b) != "newer" {
		t.Fatal("slow old encoder overwrote newer state")
	}
	s.Close()
}

// Moving a store must discard its old headroom estimate before writing into the destination.
func TestRelocationRechecksDiskBudget(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	s := New(source, 400)
	defer s.Close()
	s.Put("a", "small", bytes.Repeat([]byte{1}, 100), 0)
	other := New(target, 400)
	other.Put("a", "old", bytes.Repeat([]byte{2}, 230), 0)
	other.Close()
	resume, err := s.Suspend()
	if err != nil {
		t.Fatal(err)
	}
	resume(target)
	s.Put("a", "new", bytes.Repeat([]byte{3}, 100), s.Epoch())
	if s.Size() > 400 {
		t.Fatal("reused headroom from the old directory", s.Size())
	}
	if b, _ := s.Read("a", "new"); len(b) != 100 {
		t.Fatal("evicted the newly written entry")
	}
}

// Charging full replacements may cause an early scan, but must not evict entries that still fit.
func TestReplacementsKeepValidCapacity(t *testing.T) {
	s := New(t.TempDir(), 1024)
	defer s.Close()
	for i := range 30 {
		s.Put("a", "same", bytes.Repeat([]byte{byte(i)}, 200), 0)
	}
	if s.Size() != headerSize+200 {
		t.Fatal("replacement accounting changed disk usage", s.Size())
	}
	if b, _ := s.Read("a", "same"); len(b) != 200 || b[0] != 29 {
		t.Fatal("replacement was lost")
	}
}
