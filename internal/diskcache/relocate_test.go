package diskcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Moving drains accepted snapshots, rejects old request writes, and directs
// subsequent reads/writes to the new path without recreating the old one.
func TestSuspendRelocatesStore(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	s := New(source, 1<<20)
	defer s.Close()
	epoch := s.Epoch()
	write := s.Prepare("7", "accepted", epoch)
	go write([]byte("kept"))
	resume, suspendErr := s.Suspend()
	if suspendErr != nil {
		t.Fatal(suspendErr)
	}
	s.Put("7", "during", []byte("skip"), s.Epoch())
	if err := os.Rename(source, target); err != nil {
		resume("")
		t.Fatal(err)
	}
	resume(target)
	s.Put("7", "stale", []byte("skip"), epoch)
	s.Put("7", "fresh", []byte("new"), s.Epoch())
	for key, want := range map[string]string{"accepted": "kept", "during": "", "stale": "", "fresh": "new"} {
		if got, _ := s.Read("7", key); string(got) != want {
			t.Fatalf("%s: %q", key, got)
		}
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("recreated old store", err)
	}
}

// A long relocation must leave old snapshots readable instead of blocking startup behind song copies.
func TestSuspendKeepsSnapshotsReadable(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	defer s.Close()
	s.Put("7", "page", []byte("cached"), 0)
	resume, err := s.Suspend()
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan string, 1)
	go func() { b, _ := s.Read("7", "page"); read <- string(b) }()
	finished := false
	select {
	case b := <-read:
		finished = true
		if b != "cached" {
			t.Error("lost readable source", b)
		}
	case <-time.After(2 * time.Second):
		t.Error("relocation blocked a cached page read")
	}
	resume("")
	if !finished {
		<-read
	}
}

// Reading one ordinary snapshot must not exclude an independent content reader.
func TestContentReadsShareIO(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	defer s.Close()
	s.Put("content", "cover", []byte("cached"), 0)
	s.io.RLock()
	read := make(chan string, 1)
	go func() { b, _ := s.Read("content", "cover"); read <- string(b) }()
	finished := false
	select {
	case b := <-read:
		finished = true
		if b != "cached" {
			t.Error("lost independent content", b)
		}
	case <-time.After(2 * time.Second):
		t.Error("content read needed an exclusive I/O lock")
	}
	s.io.RUnlock()
	if !finished {
		<-read
	}
}

// Logging out while a directory is suspended still clears that account
// once the store resumes at its new path.
func TestSuspendPreservesAccountClear(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	s := New(source, 1<<20)
	defer s.Close()
	s.Put("7", "page", []byte("private"), 0)
	s.Put("8", "page", []byte("other"), 0)
	resume, suspendErr := s.Suspend()
	if suspendErr != nil {
		t.Fatal(suspendErr)
	}
	done := make(chan struct{})
	s.Clear("7", func() { close(done) })
	if err := os.Rename(source, target); err != nil {
		resume("")
		t.Fatal(err)
	}
	resume(target)
	<-done
	if b, _ := s.Read("7", "page"); b != nil {
		t.Fatal("logout retained moved private data")
	}
	if b, _ := s.Read("8", "page"); string(b) != "other" {
		t.Fatal("cleared another account")
	}
}
