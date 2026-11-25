/*
验证完整歌曲迁移、跨卷复制、失败回滚和不安全目标拒绝。
*/

package cachemove

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func song(t *testing.T, dir string) {
	t.Helper()
	meta, _ := json.Marshal(netease.Stream{Size: 5, Type: "mp3", Level: "exhigh"})
	write(t, filepath.Join(dir, "123-exhigh.mp3"), []byte("music"))
	write(t, filepath.Join(dir, "123-exhigh.json"), meta)
}

func exists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if (err == nil) != want || (err != nil && !errors.Is(err, os.ErrNotExist)) {
		t.Fatalf("stat %s: %v, want exists=%v", path, err, want)
	}
}

func copyFile(source, target string) error {
	return copyProgress(context.Background(), source, target, nil)
}

// Whole songs move before the settings are saved, and only what is whole:
// files of the listener's and half-fetched songs stay in the old directory.
func TestMove(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	song(t, layout(source).Songs)
	for _, name := range []string{"personal.mp3", "unfinished.part", "notes.txt", "999-exhigh.json"} {
		write(t, filepath.Join(layout(source).Songs, name), []byte("keep"))
	}
	modified := time.Unix(1700000000, 0)
	if err := os.Chtimes(filepath.Join(layout(source).Songs, "123-exhigh.mp3"), modified, modified); err != nil {
		t.Fatal(err)
	}
	saved := false
	warning, err := MoveLayoutWithOptions(layout(source), target, true, false, func() error {
		exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), true)
		data, err := os.ReadFile(filepath.Join(layout(target).Songs, "123-exhigh.mp3"))
		if err != nil || string(data) != "music" {
			t.Fatalf("target before save: %q, %v", data, err)
		}
		saved = true
		return nil
	}, Options{})
	if err != nil || warning != nil || !saved {
		t.Fatalf("move: %v, %v, saved=%v", err, warning, saved)
	}
	for _, name := range []string{"123-exhigh.mp3", "123-exhigh.json"} {
		exists(t, filepath.Join(layout(source).Songs, name), false)
		exists(t, filepath.Join(layout(target).Songs, name), true)
	}
	for _, name := range []string{"personal.mp3", "unfinished.part", "notes.txt", "999-exhigh.json"} {
		exists(t, filepath.Join(layout(source).Songs, name), true)
		exists(t, filepath.Join(layout(target).Songs, name), false)
	}
	info, err := os.Stat(filepath.Join(layout(target).Songs, "123-exhigh.mp3"))
	if err != nil || !info.ModTime().Equal(modified) {
		t.Fatalf("lost LRU timestamp: %v, %v", info, err)
	}
}

// A copy that fails, or a change that cannot be kept, leaves the old cache
// whole and leaves nothing of the new one behind.
func TestMoveFailure(t *testing.T) {
	for _, failure := range []string{"copy", "save"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
			song(t, layout(source).Songs)
			injected := errors.New("injected failure")
			copies, saves := 0, 0
			copy := func(from, to string) error {
				copies++
				if failure == "copy" && copies == 2 {
					return injected
				}
				return copyFile(from, to)
			}
			list, err := songFiles(layout(source).Songs)
			if err != nil {
				t.Fatal(err)
			}
			var files []cacheFile
			for _, name := range list {
				files = append(files, cacheFile{source: filepath.Join(layout(source).Songs, name), name: filepath.Join("songs", name)})
			}
			_, err = transferWithOptions(target, files, []string{"songs"}, func(string) error {
				return Validate(source, target)
			}, func() error {
				saves++
				if failure == "save" {
					return injected
				}
				return nil
			}, copy, Options{})
			if !errors.Is(err, injected) {
				t.Fatalf("got %v", err)
			}
			if failure == "copy" && saves != 0 {
				t.Fatal("saved a failed migration")
			}
			exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), true)
			exists(t, filepath.Join(layout(source).Songs, "123-exhigh.json"), true)
			exists(t, target, false)
		})
	}
}

// Changing the directory without migrating leaves every old file where it was.
func TestMoveWithoutMigration(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	song(t, layout(source).Songs)
	warning, err := MoveLayoutWithOptions(layout(source), target, false, false, func() error { return nil }, Options{})
	if err != nil || warning != nil {
		t.Fatalf("move: %v %v", err, warning)
	}
	exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), true)
	entries, err := os.ReadDir(layout(target).Songs)
	if err != nil || len(entries) != 0 {
		t.Fatalf("target: %v %v", entries, err)
	}
}

// Overlapping paths, a directory holding files of the listener's, and paths
// that are not directories are refused before anything is saved.
func TestMoveRejectsUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old")
	song(t, layout(source).Songs)
	occupied := filepath.Join(root, "occupied")
	write(t, filepath.Join(layout(occupied).Songs, "personal.txt"), []byte("keep"))
	blocked := filepath.Join(root, "file")
	write(t, blocked, []byte("not a directory"))
	for _, target := range []string{source, filepath.Join(layout(source).Songs, "child"), root, occupied, blocked, filepath.Join(blocked, "child"), "relative"} {
		t.Run(filepath.Base(target), func(t *testing.T) {
			if _, err := MoveLayoutWithOptions(layout(source), target, true, false, func() error { t.Fatal("saved invalid path"); return nil }, Options{}); err == nil {
				t.Fatal("accepted unsafe target")
			}
			exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), true)
			data, err := os.ReadFile(filepath.Join(layout(occupied).Songs, "personal.txt"))
			if err != nil || string(data) != "keep" {
				t.Fatal("changed unrelated target file")
			}
		})
	}
}

// A song whose file is not the size its .json says is not moved, nor removed.
func TestMoveSkipsIncompleteSongs(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	song(t, layout(source).Songs)
	write(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), []byte("shorter than metadata"))
	warning, err := MoveLayoutWithOptions(layout(source), target, true, false, func() error { return nil }, Options{})
	if err != nil || warning != nil {
		t.Fatalf("move: %v %v", err, warning)
	}
	exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), true)
	exists(t, filepath.Join(layout(target).Songs, "123-exhigh.mp3"), false)
}

// A target on another disk takes the same copy-and-publish path: the source
// is never renamed across.
func TestMoveAcrossVolumes(t *testing.T) {
	parent := os.Getenv("CACHE_MOVE_TEST_TARGET")
	if parent == "" {
		t.Skip("set CACHE_MOVE_TEST_TARGET to a temporary directory on a second disk")
	}
	source := t.TempDir()
	// The caller chooses the second volume. VolumeName is empty for Unix
	// mounts, so comparing it would skip every cross-volume macOS test.
	target, err := os.MkdirTemp(parent, "cachemove-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target) })
	song(t, layout(source).Songs)
	warning, err := MoveLayoutWithOptions(layout(source), target, true, false, func() error { return nil }, Options{})
	if err != nil || warning != nil {
		t.Fatalf("cross-volume move: %v %v", err, warning)
	}
	exists(t, filepath.Join(layout(target).Songs, "123-exhigh.mp3"), true)
	exists(t, filepath.Join(layout(source).Songs, "123-exhigh.mp3"), false)
}
