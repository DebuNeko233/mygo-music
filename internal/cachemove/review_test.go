package cachemove

import (
	"context"
	"errors"
	"netease-music-mygo/internal/diskcache"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// A writable mount root works even when its parent cannot hold a staging directory.
func TestMoveStagesInsideExistingTarget(t *testing.T) {
	root := t.TempDir()
	source := layout(filepath.Join(root, "old"))
	parent, target := filepath.Join(root, "mounts"), filepath.Join(root, "mounts", "volume")
	song(t, source.Songs)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(parent, 0o700)
	}
	journal := filepath.Join(root, "data", "move.json")
	observed := false
	warning, err := MoveLayoutWithOptions(source, target, true, false, func() error { return nil }, Options{Journal: journal, Progress: func(copied, total int64) {
		records, err := readJournal(journal)
		if err != nil || len(records) != 1 {
			t.Fatal("stage was not inside the destination", records, err)
		}
		if same, err := SameDirectory(filepath.Dir(records[0].Stage), target); err != nil || !same {
			t.Fatal("stage was not inside target", err)
		}
		observed = true
	}})
	if err != nil || warning != nil || !observed {
		t.Fatal("mount-root relocation failed", warning, err)
	}
	exists(t, target, true)
	exists(t, filepath.Join(target, "songs", "123-exhigh.mp3"), true)
	exists(t, journal, false)
}

// Cancellation during a copy must skip settings publication and leave the source intact.
func TestMoveCancellationRollsBack(t *testing.T) {
	root := t.TempDir()
	source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
	song(t, source.Songs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := false
	journal := filepath.Join(root, "data", "move.json")
	_, err := MoveLayoutWithOptions(source, target, true, false, func() error { saved = true; return nil }, Options{Context: ctx, Journal: journal, Progress: func(copied, total int64) {
		if copied > 0 {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) || saved {
		t.Fatal("cancelled copy committed settings", err, saved)
	}
	exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), true)
	exists(t, target, false)
	exists(t, journal, false)
}

// A process exiting mid-copy leaves an owned stage which the next launch can remove safely.
func TestMoveRecoversAfterProcessExit(t *testing.T) {
	if root := os.Getenv("MYGO_TEST_CRASH_MOVE"); root != "" {
		source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
		MoveLayoutWithOptions(source, target, true, false, func() error { return nil }, Options{Journal: filepath.Join(root, "data", "move.json"), Progress: func(copied, total int64) {
			if copied == total && total > 0 {
				os.Exit(73)
			}
		}})
		os.Exit(74)
	}
	root := t.TempDir()
	source, target := layout(filepath.Join(root, "old")), filepath.Join(root, "new")
	song(t, source.Songs)
	diskSnapshotForCrash(t, source.Browse)
	cmd := exec.Command(os.Args[0], "-test.run=^TestMoveRecoversAfterProcessExit$")
	cmd.Env = append(os.Environ(), "MYGO_TEST_CRASH_MOVE="+root)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatal("child did not exit during the copy", err)
	}
	journal := filepath.Join(root, "data", "move.json")
	records, err := readJournal(journal)
	if err != nil || len(records) != 1 {
		t.Fatal("missing durable stage record", err)
	}
	stage := records[0].Stage
	exists(t, stage, true)
	unrelated := filepath.Join(target, stagePrefix+"personal")
	write(t, filepath.Join(unrelated, "notes.txt"), []byte("keep"))
	if err := Recover(journal); err != nil {
		t.Fatal(err)
	}
	exists(t, stage, false)
	exists(t, journal, false)
	exists(t, filepath.Join(unrelated, "notes.txt"), true)
	exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), true)
}

// A matching ownership record is required before crash cleanup can remove any directory.
func TestMoveRecoveryRejectsMismatchedMarker(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(target, stagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(root, "data", "move.json")
	if err := recordStage(journal, target, stage); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stage, stageMarker), []byte("{}"))
	write(t, filepath.Join(stage, "notes.txt"), []byte("keep"))
	if err := Recover(journal); err == nil {
		t.Fatal("deleted an unverified directory")
	}
	exists(t, filepath.Join(stage, "notes.txt"), true)
	exists(t, journal, true)
}

// Recovery retains disconnected-volume records while a different target can complete a new move.
func TestMoveRecoveryRetainsDisconnectedTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "volume")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(target, stagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(root, "data", "move.json")
	if err := recordStage(journal, target, stage); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, "disconnected")
	if err := os.Rename(target, hidden); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journal); err != nil {
		t.Fatal(err)
	}
	records, err := readJournal(journal)
	if err != nil || len(records) != 1 {
		t.Fatal("lost disconnected-volume cleanup record", err)
	}
	source, next := layout(filepath.Join(root, "old")), filepath.Join(root, "next")
	song(t, source.Songs)
	if _, err := MoveLayoutWithOptions(source, next, true, false, func() error { return nil }, Options{Journal: journal}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(hidden, target); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journal); err != nil {
		t.Fatal(err)
	}
	exists(t, stage, false)
	exists(t, journal, false)
}

func diskSnapshotForCrash(t *testing.T, dir string) {
	t.Helper()
	s := diskcache.New(dir, 1<<20)
	defer s.Close()
	s.Put("7", "large", make([]byte, 256<<10), 0)
}

// An actual volume root must retain its filesystem identity while managed children migrate across devices.
func TestMoveLayoutToVolumeRoot(t *testing.T) {
	target := os.Getenv("CACHE_MOVE_TEST_VOLUME_ROOT")
	if target == "" {
		t.Skip("set CACHE_MOVE_TEST_VOLUME_ROOT to a dedicated empty mounted filesystem")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("volume-root test requires an empty dedicated filesystem")
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	source := layout(t.TempDir())
	song(t, source.Songs)
	snapshot(t, source.Browse)
	snapshot(t, source.Artwork)
	warning, err := MoveLayout(source, target, true, false, func() error { return nil })
	if err != nil || warning != nil {
		t.Fatal("migration to volume root failed", warning, err)
	}
	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("replaced the mounted root", err)
	}
	checkSnapshot(t, layout(target).Browse, "cached")
	// Retire only the test's managed files; do not recursively remove a mount.
	warning, err = MoveLayout(layout(target), source.Root, true, true, func() error { return nil })
	if err != nil || warning != nil {
		t.Fatal("return migration failed", warning, err)
	}
	for _, child := range []string{"songs", "browse-v1", ArtworkDir} {
		if err := os.Remove(filepath.Join(target, child)); err != nil {
			t.Fatal(err)
		}
	}
}

// Publication must not unlink a regular destination file introduced after the emptiness check.
func TestMovePublicationPreservesUnexpectedRegularFile(t *testing.T) {
	root := t.TempDir()
	target, source := filepath.Join(root, "target"), filepath.Join(root, "source")
	write(t, source, []byte("cache"))
	checks := 0
	_, err := transferWithOptions(target, []cacheFile{{source: source, name: "songs/item"}}, []string{"songs"}, func(string) error {
		checks++
		if checks == 2 {
			write(t, filepath.Join(target, "songs"), []byte("personal"))
		}
		return nil
	}, func() error { t.Fatal("saved over an unrelated file"); return nil }, nil, Options{})
	if !errors.Is(err, ErrConflict) {
		t.Fatal("accepted a regular destination file", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "songs")); err != nil || string(b) != "personal" {
		t.Fatal("changed unrelated destination", err)
	}
	exists(t, source, true)
}
