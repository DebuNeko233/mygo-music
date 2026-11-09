package cachemove

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/diskcache"
)

func layout(root string) Layout {
	return Layout{root, filepath.Join(root, "songs"), filepath.Join(root, "browse-v1"), filepath.Join(root, "artwork-v2")}
}

func snapshot(t *testing.T, dir string) {
	t.Helper()
	s := diskcache.New(dir, 1<<20)
	s.Put("7", "resource", []byte("cached"), 0)
	s.Close()
}

func checkSnapshot(t *testing.T, dir string, want string) {
	t.Helper()
	s := diskcache.New(dir, 1<<20)
	defer s.Close()
	if b, _ := s.Read("7", "resource"); string(b) != want {
		t.Fatalf("%s: %q", dir, b)
	}
}

// Songs, browse snapshots and artwork move as one transaction; unrelated
// files and partial downloads remain at their original locations.
func TestMoveLayout(t *testing.T) {
	root := t.TempDir()
	source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
	song(t, source.Songs)
	snapshot(t, source.Browse)
	snapshot(t, source.Artwork)
	write(t, filepath.Join(source.Root, "personal.txt"), []byte("keep"))
	write(t, filepath.Join(source.Browse, ".part-incomplete"), []byte("partial"))
	warning, err := MoveLayout(source, target, true, false, func() error {
		exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), true)
		checkSnapshot(t, layout(target).Browse, "cached")
		checkSnapshot(t, layout(target).Artwork, "cached")
		return nil
	})
	if err != nil || warning != nil {
		t.Fatal(warning, err)
	}
	exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), false)
	exists(t, filepath.Join(target, "songs", "123-exhigh.mp3"), true)
	checkSnapshot(t, source.Browse, "")
	checkSnapshot(t, source.Artwork, "")
	exists(t, filepath.Join(source.Root, "personal.txt"), true)
	exists(t, filepath.Join(source.Browse, ".part-incomplete"), true)
}

// Saving a shared-root move must succeed before any of the three old
// caches are retired, and rollback leaves no published target behind.
func TestMoveLayoutSaveFailure(t *testing.T) {
	root := t.TempDir()
	source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
	song(t, source.Songs)
	snapshot(t, source.Browse)
	snapshot(t, source.Artwork)
	injected := errors.New("save failed")
	_, err := MoveLayout(source, target, true, false, func() error { return injected })
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	exists(t, target, false)
	exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), true)
	checkSnapshot(t, source.Browse, "cached")
	checkSnapshot(t, source.Artwork, "cached")
}

// Legacy custom song folders convert in place, including an identical child
// folder published before a crash; a failed save is safe to retry.
func TestMoveLayoutLegacyRestart(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "custom")
	source := Layout{target, target, filepath.Join(root, "system-browse"), filepath.Join(root, "system-art")}
	song(t, source.Songs)
	snapshot(t, source.Browse)
	snapshot(t, source.Artwork)
	write(t, filepath.Join(target, "personal.txt"), []byte("keep"))
	// Reproduce publication of songs before the layout setting was saved.
	for _, name := range []string{"123-exhigh.mp3", "123-exhigh.json"} {
		if err := os.MkdirAll(filepath.Join(target, "songs"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := copyFile(filepath.Join(target, name), filepath.Join(target, "songs", name)); err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("save failed")
	_, err := MoveLayout(source, target, true, true, func() error { return injected })
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	exists(t, filepath.Join(target, "123-exhigh.mp3"), true)
	exists(t, filepath.Join(target, "songs", "123-exhigh.mp3"), true)
	checkSnapshot(t, source.Browse, "cached")
	warning, err := MoveLayout(source, target, true, true, func() error { return nil })
	if err != nil || warning != nil {
		t.Fatal(warning, err)
	}
	exists(t, filepath.Join(target, "123-exhigh.mp3"), false)
	exists(t, filepath.Join(target, "songs", "123-exhigh.mp3"), true)
	exists(t, filepath.Join(target, "personal.txt"), true)
	checkSnapshot(t, layout(target).Browse, "cached")
	checkSnapshot(t, layout(target).Artwork, "cached")
}

// Conversion refuses a conflicting child directory and never treats its
// personal files as a partially published application cache.
func TestMoveLayoutLegacyConflict(t *testing.T) {
	root := t.TempDir()
	source := layout(filepath.Join(root, "custom"))
	source.Songs = source.Root
	song(t, source.Songs)
	write(t, filepath.Join(source.Root, "songs", "personal.txt"), []byte("keep"))
	_, err := MoveLayout(source, source.Root, true, true, func() error { t.Fatal("saved conflicting conversion"); return nil })
	if err == nil {
		t.Fatal("overwrote conflicting folder")
	}
	exists(t, filepath.Join(source.Root, "123-exhigh.mp3"), true)
	b, err := os.ReadFile(filepath.Join(source.Root, "songs", "personal.txt"))
	if err != nil || string(b) != "keep" {
		t.Fatal("changed personal file")
	}
}

// If a destination child is occupied during staging, rollback removes only
// children already published and preserves the file that caused the conflict.
func TestMoveLayoutPartialPublication(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "new")
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	write(t, first, []byte("audio"))
	write(t, second, []byte("page"))
	files := []cacheFile{{source: first, name: "songs/item"}, {source: second, name: "browse-v1/item"}}
	checks := 0
	_, err := transfer(target, files, []string{"songs", "browse-v1"}, func() error {
		checks++
		if checks == 2 {
			write(t, filepath.Join(target, "browse-v1", "item"), []byte("personal"))
		}
		return nil
	}, func() error { t.Fatal("saved conflicting publication"); return nil }, copyFile)
	if err == nil {
		t.Fatal("accepted occupied destination")
	}
	if b, err := os.ReadFile(filepath.Join(target, "browse-v1", "item")); err != nil || string(b) != "personal" {
		t.Fatal("rollback removed unrelated file")
	}
	exists(t, filepath.Join(target, "songs"), false)
	exists(t, first, true)
	exists(t, second, true)
}

// A complete song kept under an alternate Windows filename follows its
// metadata during migration instead of being silently left behind.
func TestMoveAlternateSongFile(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	song(t, source)
	name := "123-exhigh-1700000000.mp3"
	if err := os.Rename(filepath.Join(source, "123-exhigh.mp3"), filepath.Join(source, name)); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"size": 5, "type": "mp3", "level": "exhigh", "file": name})
	write(t, filepath.Join(source, "123-exhigh.json"), meta)
	warning, err := Move(source, target, true, func() error { return nil })
	if warning != nil || err != nil {
		t.Fatal(warning, err)
	}
	exists(t, filepath.Join(target, name), true)
	exists(t, filepath.Join(source, name), false)
}
