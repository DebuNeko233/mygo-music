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
