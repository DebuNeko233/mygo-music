package cachemove

import (
	"os"
	"path/filepath"
	"testing"
)

// Migration and switching preserve unrelated target content while adopting missing or empty cache children.
func TestMoveLayoutPreservesUnmanagedTargetContent(t *testing.T) {
	for _, migrate := range []bool{false, true} {
		t.Run(map[bool]string{false: "switch", true: "migrate"}[migrate], func(t *testing.T) {
			root := t.TempDir()
			source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
			song(t, source.Songs)
			personal := map[string]string{".DS_Store": "Finder metadata", "notes.txt": "notes", "other/files.txt": "keep"}
			for name, data := range personal {
				write(t, filepath.Join(target, name), []byte(data))
			}
			for _, name := range []string{"songs", "browse-v1"} {
				if err := os.Mkdir(filepath.Join(target, name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			saved := false
			warning, err := MoveLayout(source, target, migrate, false, func() error { saved = true; return nil })
			if err != nil || warning != nil || !saved {
				t.Fatal("unrelated files blocked relocation", warning, err)
			}
			for name, want := range personal {
				if b, err := os.ReadFile(filepath.Join(target, name)); err != nil || string(b) != want {
					t.Fatal("changed unrelated target data", name, err)
				}
			}
			exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), !migrate)
			exists(t, filepath.Join(target, "songs", "123-exhigh.mp3"), migrate)
		})
	}
}

// Only an occupied, non-directory or linked managed child blocks a destination containing unrelated data.
func TestLayoutDestinationRejectsCacheConflicts(t *testing.T) {
	for _, name := range []string{"songs", "browse-v1", ArtworkDir} {
		for _, kind := range []string{"nonempty", "file", "symlink"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
				write(t, filepath.Join(target, "notes.txt"), []byte("keep"))
				child := filepath.Join(target, name)
				switch kind {
				case "nonempty":
					write(t, filepath.Join(child, "cached"), []byte("existing"))
				case "file":
					write(t, child, []byte("keep"))
				case "symlink":
					other := t.TempDir()
					if err := os.Symlink(other, child); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				}
				if err := Validate(source, target); err == nil {
					t.Fatal("accepted conflicting cache child")
				}
			})
		}
	}
}
