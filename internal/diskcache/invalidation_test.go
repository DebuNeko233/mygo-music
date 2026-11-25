/*
验证单项失效和清理拒绝旧写入，同时保留无关或更新的缓存内容。
*/

package diskcache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Forgetting one cover used to discard accepted writes for unrelated covers.
func TestForgetPreservesUnrelatedWrites(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	write := s.Prepare("a", "keep", s.Epoch())
	s.Forget("a", "gone")
	write([]byte("kept"))
	if got, _ := s.Read("a", "keep"); string(got) != "kept" {
		t.Fatal("single-entry removal invalidated another write")
	}
	s.Close()
}

// An accepted old encoder must not resurrect an entry after Forget.
func TestForgetRejectsOldSameKeyWrite(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	write := s.Prepare("a", "cover", s.Epoch())
	s.Forget("a", "cover")
	write([]byte("old"))
	if got, _ := s.Read("a", "cover"); got != nil {
		t.Fatal("old encoder resurrected a forgotten entry")
	}
	s.Close()
}

// A late Forget worker used to delete a newer successfully committed value.
func TestForgetThenPutKeepsNewValue(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	s.Put("a", "cover", []byte("old"), s.Epoch())
	remove := s.prepareForget("a", "cover")
	if got, _ := s.Read("a", "cover"); got != nil {
		t.Fatal("forgotten value remained readable before deletion")
	}
	s.Put("a", "cover", []byte("new"), s.Epoch())
	remove() // Force deletion to run after the replacement commits.
	if got, _ := s.Read("a", "cover"); string(got) != "new" {
		t.Fatal("late removal deleted the replacement")
	}
	s.Close()
}

// Completing one clear used to accept writes while another clear could still delete them.
func TestConcurrentClearAndPut(t *testing.T) {
	s := New(t.TempDir(), 1<<20)
	s.Put("a", "page", []byte("old"), s.Epoch())
	// Reserve both clears before running either worker.
	first := s.prepareClear("a")
	second := s.prepareClear("")
	first()
	write := s.Prepare("a", "page", s.Epoch())
	second()
	if write != nil {
		write([]byte("rejected"))
		t.Fatal("first clear opened the write barrier too early")
	}
	s.Put("a", "page", []byte("new"), s.Epoch())
	if got, _ := s.Read("a", "page"); string(got) != "new" {
		t.Fatal("final clear did not reopen the store")
	}
	s.Close()
}

// A failed unlink used to make forgotten and cleared snapshots readable again.
func TestFailedDeletionKeepsSnapshotsHidden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory permissions require Unix")
	}
	for _, action := range []string{"forget", "scope", "all"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			s := New(dir, 1<<20)
			defer s.Close()
			s.Put("a", "list", []byte("old"), s.Epoch())
			s.Put("b", "list", []byte("other"), s.Epoch())
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o700) })
			switch action {
			case "forget":
				s.prepareForget("a", "list")()
			case "scope":
				s.prepareClear("a")()
			case "all":
				s.prepareClear("")()
			}
			if _, err := os.Stat(filepath.Join(dir, name("a", "list"))); err != nil {
				t.Fatalf("fixture did not retain the stale disk file: %v", err)
			}
			if b, _ := s.Read("a", "list"); b != nil {
				t.Fatalf("read stale snapshot %q after failed deletion", b)
			}
			if b, _ := s.Read("b", "list"); (b == nil) != (action == "all") {
				t.Fatalf("unrelated scope availability after %s: %q", action, b)
			}
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			// An equal new snapshot still has to replace the invalidated file.
			s.PrepareIfChanged("a", "list", s.Epoch())([]byte("old"))
			if b, _ := s.Read("a", "list"); string(b) != "old" {
				t.Fatalf("replacement is unreadable: %q", b)
			}
		})
	}
}
