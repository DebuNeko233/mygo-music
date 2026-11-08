package diskcache

import "testing"

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
