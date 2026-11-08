package diskcache

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func indexed(dir string) *Store { return NewIndexed(dir, 1<<20, "requests", "families") }

// Both metadata namespaces share one log, survive restart, and never persist raw request identities.
func TestIndexRestartAndScopeIsolation(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	secret := "https://example.test/cover?token=private\nCookie=MUSIC_U=private"
	s.Put("requests", secret, []byte(`{"content":"one"}`), 0)
	s.Put("families", secret, []byte(`{"content":"two"}`), 0)
	s.Put("content", "one", []byte("image"), 0)
	b, _ := s.Read("requests", secret)
	b[0] = '['
	s.Close()
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatal("metadata still creates individual files", len(files), err)
	}
	log, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil || bytes.Contains(log, []byte("private")) {
		t.Fatal("raw identity persisted", err)
	}
	s = indexed(dir)
	defer s.Close()
	for scope, want := range map[string]string{"requests": `{"content":"one"}`, "families": `{"content":"two"}`, "content": "image"} {
		key := secret
		if scope == "content" {
			key = "one"
		}
		b, at := s.Read(scope, key)
		if string(b) != want || at.IsZero() {
			t.Fatalf("%s: %q", scope, b)
		}
	}
}

// An interrupted append must not swallow the first complete record written after restart.
func TestIndexTruncatedTailRecovery(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	s.Put("requests", "old", []byte(`{"v":1}`), 0)
	s.Close()
	f, err := os.OpenFile(filepath.Join(dir, IndexFile), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(`{"record":`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s = indexed(dir)
	if b, _ := s.Read("requests", "old"); string(b) != `{"v":1}` {
		t.Fatal("lost complete prefix")
	}
	s.Put("requests", "new", []byte(`{"v":2}`), 0)
	s.Close()
	s = indexed(dir)
	defer s.Close()
	for key, want := range map[string]string{"old": `{"v":1}`, "new": `{"v":2}`} {
		if b, _ := s.Read("requests", key); string(b) != want {
			t.Fatal("append joined a torn line", key, string(b))
		}
	}
}

// Skipping a damaged delete would resurrect an older value, so a corrupt log must become a miss.
func TestIndexCorruptionCannotResurrectDeletedEntry(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	s.Put("requests", "gone", []byte(`{"v":1}`), 0)
	s.prepareForget("requests", "gone")()
	s.Put("requests", "keep", []byte(`{"v":2}`), 0)
	s.Close()
	path := filepath.Join(dir, IndexFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	damaged := bytes.Replace(b, []byte(`"op":"del"`), []byte(`"op":"put"`), 1)
	if bytes.Equal(b, damaged) {
		t.Fatal("fixture contains no delete")
	}
	if err := os.WriteFile(path, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	s = indexed(dir)
	for _, key := range []string{"gone", "keep"} {
		if b, _ := s.Read("requests", key); b != nil {
			t.Fatal("served data past a damaged record", key)
		}
	}
	s.Put("requests", "fresh", []byte(`{"v":3}`), 0)
	s.Close()
	s = indexed(dir)
	defer s.Close()
	if b, _ := s.Read("requests", "fresh"); string(b) != `{"v":3}` {
		t.Fatal("could not rebuild after corruption")
	}
	if b, _ := s.Read("requests", "gone"); b != nil {
		t.Fatal("repair restored a deleted entry")
	}
}

// Concurrent updates must survive automatic compaction with the last accepted value for every key.
func TestIndexConcurrentAppendAndCompaction(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	var group sync.WaitGroup
	for worker := range 8 {
		group.Go(func() {
			for revision := range 100 {
				s.Put("requests", fmt.Sprint(worker), []byte(fmt.Sprintf(`{"revision":%d}`, revision)), 0)
			}
		})
	}
	group.Wait()
	s.Close()
	s = indexed(dir)
	defer s.Close()
	for worker := range 8 {
		if b, _ := s.Read("requests", fmt.Sprint(worker)); string(b) != `{"revision":99}` {
			t.Fatalf("lost latest value for %d: %s", worker, b)
		}
	}
	if s.Size() > 1<<20 {
		t.Fatal("index exceeded the cache budget")
	}
}

// Repeated replacements and new keys must not grow the log or in-memory index without bounds.
func TestIndexBoundsAndCompaction(t *testing.T) {
	dir := t.TempDir()
	s := NewIndexed(dir, 64<<10, "requests")
	for revision := range 200 {
		s.Put("requests", "same", []byte(fmt.Sprintf(`{"v":%d}`, revision)), 0)
	}
	s.io.Lock()
	lines := s.index.lines
	s.io.Unlock()
	if lines >= 200 {
		t.Fatal("obsolete replacements were never compacted")
	}
	for key := range 200 {
		s.Put("requests", fmt.Sprint(key), []byte(`{"value":"bounded"}`), 0)
	}
	s.io.Lock()
	live, limit, count := s.index.live, s.index.liveLimit, len(s.index.entries)
	s.io.Unlock()
	if live > limit || count >= 200 {
		t.Fatal("memory index was not bounded", live, limit, count)
	}
	s.Close()
	s = NewIndexed(dir, 64<<10, "requests")
	defer s.Close()
	if b, _ := s.Read("requests", "199"); string(b) != `{"value":"bounded"}` {
		t.Fatal("evicted newest entry")
	}
	if s.Size() > s.index.logLimit {
		t.Fatal("compaction left an oversized log")
	}
}

// Deletion is durable and an older delayed removal cannot delete a replacement or unrelated key.
func TestIndexForgetOrdering(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	s.Put("requests", "a", []byte(`{"v":1}`), 0)
	old := s.Prepare("requests", "a", 0)
	remove := s.prepareForget("requests", "a")
	s.Put("requests", "b", []byte(`{"v":2}`), 0)
	s.Put("requests", "a", []byte(`{"v":3}`), 0)
	remove()
	old([]byte(`{"v":0}`))
	s.prepareForget("requests", "b")()
	s.Close()
	s = indexed(dir)
	defer s.Close()
	if b, _ := s.Read("requests", "a"); string(b) != `{"v":3}` {
		t.Fatal("late delete or write replaced new value")
	}
	if b, _ := s.Read("requests", "b"); b != nil {
		t.Fatal("delete was lost on restart")
	}
}

// Clear must reject old writers and keep buffered records from reappearing through stale flush callbacks.
func TestIndexClearRejectsOldWriterAndTimer(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	old := s.Prepare("requests", "old", 0)
	s.Put("requests", "pending", []byte(`{"v":1}`), 0)
	s.io.Lock()
	batch := s.index.batch
	s.io.Unlock()
	done := make(chan struct{})
	s.Clear("", func() { close(done) })
	<-done
	s.Put("requests", "new", []byte(`{"v":2}`), s.Epoch())
	s.flushIndex(batch)
	old([]byte(`{"v":0}`))
	s.Close()
	s = indexed(dir)
	defer s.Close()
	for _, key := range []string{"old", "pending"} {
		if b, _ := s.Read("requests", key); b != nil {
			t.Fatal("clear was undone", key)
		}
	}
	if b, _ := s.Read("requests", "new"); string(b) != `{"v":2}` {
		t.Fatal("clear rejected new work")
	}
}

// Clearing one indexed namespace must retain the other namespace across restart.
func TestIndexClearScope(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	s.Put("requests", "key", []byte(`{"v":1}`), 0)
	s.Put("families", "key", []byte(`{"v":2}`), 0)
	done := make(chan struct{})
	s.Clear("requests", func() { close(done) })
	<-done
	s.Close()
	s = indexed(dir)
	defer s.Close()
	if b, _ := s.Read("requests", "key"); b != nil {
		t.Fatal("scope clear was lost")
	}
	if b, _ := s.Read("families", "key"); string(b) != `{"v":2}` {
		t.Fatal("scope clear deleted another namespace")
	}
}

// Suspend flushes batches before relocation and drops the old map when switching to an empty directory.
func TestIndexSuspendRelocateOrSwitch(t *testing.T) {
	for _, migrate := range []bool{false, true} {
		t.Run(fmt.Sprint(migrate), func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
			s := indexed(source)
			s.Put("requests", "old", []byte(`{"v":1}`), 0)
			resume, suspendErr := s.Suspend()
			if suspendErr != nil {
				t.Fatal(suspendErr)
			}
			if _, err := os.Stat(filepath.Join(source, IndexFile)); err != nil {
				resume("")
				t.Fatal("suspend did not flush", err)
			}
			var err error
			if migrate {
				err = os.Rename(source, target)
			} else {
				err = os.MkdirAll(target, 0700)
			}
			if err != nil {
				resume("")
				t.Fatal(err)
			}
			resume(target)
			b, _ := s.Read("requests", "old")
			if (b != nil) != migrate {
				t.Fatal("carried wrong in-memory index across switch")
			}
			s.Put("requests", "stale", []byte(`{"v":0}`), 0)
			s.Put("requests", "fresh", []byte(`{"v":2}`), s.Epoch())
			s.Close()
			s = indexed(target)
			defer s.Close()
			if b, _ := s.Read("requests", "stale"); b != nil {
				t.Fatal("old epoch wrote after relocation")
			}
			if b, _ := s.Read("requests", "fresh"); string(b) != `{"v":2}` {
				t.Fatal("new writes missed target")
			}
		})
	}
}

// A completed batch flush must make recent metadata readable by a separate store without closing the writer.
func TestIndexBatchFlushPersistsRecentEntries(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	defer s.Close()
	for key := range 10 {
		s.Put("requests", fmt.Sprint(key), []byte(`{"v":1}`), 0)
	}
	s.io.Lock()
	batch := s.index.batch
	s.io.Unlock()
	s.flushIndex(batch)
	cold := indexed(dir)
	defer cold.Close()
	for key := range 10 {
		if b, _ := cold.Read("requests", fmt.Sprint(key)); string(b) != `{"v":1}` {
			t.Fatal("batch never reached disk", key)
		}
	}
}

// JSON escaping must not produce a persisted line larger than the bounded replay reader accepts.
func TestIndexRejectsOversizedEncodedLine(t *testing.T) {
	dir := t.TempDir()
	s := indexed(dir)
	s.Put("requests", "keep", []byte(`{"v":1}`), 0)
	s.Put("requests", "oversized", []byte(`{"etag":"`+strings.Repeat("<", maxIndexValue/2)+`"}`), 0)
	s.Close()
	s = indexed(dir)
	defer s.Close()
	if b, _ := s.Read("requests", "keep"); string(b) != `{"v":1}` {
		t.Fatal("oversized row broke replay of valid data")
	}
	if b, _ := s.Read("requests", "oversized"); b != nil {
		t.Fatal("persisted a row that exceeds the replay bound")
	}
}

// Unavailable storage must not accumulate an unbounded append queue and must recover when writable again.
func TestIndexWriteFailureKeepsMemoryBounded(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	writer := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(writer)
	dir := filepath.Join(blocked, "artwork")
	s := indexed(dir)
	defer s.Close()
	value := []byte(`{"value":"` + strings.Repeat("x", 512) + `"}`)
	for key := range 200 {
		s.Put("requests", fmt.Sprint(key), value, 0)
	}
	s.io.Lock()
	pending, live, limit := len(s.index.pending), s.index.live, s.index.liveLimit
	s.io.Unlock()
	if pending > indexBatch+maxIndexLine || live > limit {
		t.Fatal("failed writes grew memory without bounds", pending, live)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	s.Put("requests", "recovered", []byte(`{"v":1}`), 0)
	s.Close()
	cold := indexed(dir)
	defer cold.Close()
	if b, _ := cold.Read("requests", "recovered"); string(b) != `{"v":1}` {
		t.Fatal("failed index could not be rebuilt")
	}
	if b, _ := cold.Read("requests", "199"); !bytes.Equal(b, value) {
		t.Fatal("repair lost the latest in-memory data")
	}
}

// Migration must stop on a failed index flush and leave the original store writable for recovery.
func TestIndexSuspendRejectsFlushFailure(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	writer := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(writer)
	dir := filepath.Join(blocked, "artwork")
	s := indexed(dir)
	defer s.Close()
	s.Put("requests", "first", []byte(`{"v":1}`), 0)
	s.Put("requests", "pending", []byte(`{"v":2}`), 0)
	resume, err := s.Suspend()
	if err == nil {
		resume("")
		t.Fatal("migration accepted unflushed metadata")
	}
	if s.moving.Load() {
		t.Fatal("failed suspend left store blocked")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	s.Put("requests", "new", []byte(`{"v":3}`), s.Epoch())
	s.Close()
	cold := indexed(dir)
	defer cold.Close()
	if b, _ := cold.Read("requests", "new"); string(b) != `{"v":3}` {
		t.Fatal("failed suspend prevented recovery")
	}
}
