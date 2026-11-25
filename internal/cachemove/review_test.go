/*
验证缓存迁移在取消、进程退出和发布冲突后的恢复与文件保护。
*/

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
		t.Cleanup(func() {
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Error(err)
			}
		})
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
	if err := Recover(journal, Layout{}); err != nil {
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
	if _, err := recordStage(journal, target, stage); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stage, stageMarker), []byte("{}"))
	write(t, filepath.Join(stage, "notes.txt"), []byte("keep"))
	if err := Recover(journal, Layout{}); err == nil {
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
	if _, err := recordStage(journal, target, stage); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, "disconnected")
	if err := os.Rename(target, hidden); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journal, Layout{}); err != nil {
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
	if err := Recover(journal, Layout{}); err != nil {
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
	if b, _ := s.Read("7", "large"); len(b) != 256<<10 {
		t.Fatal("could not prepare browse cache")
	}
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
	warning, err := MoveLayoutWithOptions(source, target, true, false, func() error { return nil }, Options{})
	if err != nil || warning != nil {
		t.Fatal("migration to volume root failed", warning, err)
	}
	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("replaced the mounted root", err)
	}
	checkSnapshot(t, layout(target).Browse, "cached")
	// Retire only the test's managed files; do not recursively remove a mount.
	warning, err = MoveLayoutWithOptions(layout(target), source.Root, true, true, func() error { return nil }, Options{})
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

// A restart after publication rolls back an uncommitted target and preserves a committed one.
func TestMoveRecoversAfterPublication(t *testing.T) {
	if root := os.Getenv("MYGO_TEST_PUBLISHED_MOVE"); root != "" {
		source, target := layout(filepath.Join(root, "old")), layout(filepath.Join(root, "new"))
		MoveLayoutWithOptions(source, target.Root, true, false, func() error {
			if os.Getenv("MYGO_TEST_PUBLISHED_COMMIT") == "yes" {
				if err := os.WriteFile(filepath.Join(root, "saved-target"), []byte(target.Root), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			os.Exit(73)
			return nil
		}, Options{Journal: filepath.Join(root, "move.json")})
		t.Fatal("transaction did not reach settings commit")
	}
	for _, phase := range []string{"uncommitted", "committed", "committed-disconnected"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			source, target := layout(filepath.Join(root, "old")), layout(filepath.Join(root, "new"))
			song(t, source.Songs)
			snapshot(t, source.Browse)
			snapshot(t, source.Artwork)
			commit := "no"
			if phase != "uncommitted" {
				commit = "yes"
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestMoveRecoversAfterPublication$")
			cmd.Env = append(os.Environ(), "MYGO_TEST_PUBLISHED_MOVE="+root, "MYGO_TEST_PUBLISHED_COMMIT="+commit)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("child did not exit after publication: %v\n%s", err, output)
			}
			journal := filepath.Join(root, "move.json")
			audio := filepath.Join(target.Songs, "123-exhigh.mp3")
			exists(t, audio, true)
			active := source
			if saved, err := os.ReadFile(filepath.Join(root, "saved-target")); err == nil {
				active = layout(string(saved))
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if phase == "committed-disconnected" {
				offline := filepath.Join(root, "offline")
				if err := os.Rename(target.Root, offline); err != nil {
					t.Fatal(err)
				}
				if err := Recover(journal, active); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(offline, target.Root); err != nil {
					t.Fatal(err)
				}
				active = source // The saved location changed while the volume was away.
			}
			personal := filepath.Join(target.Root, "personal.txt")
			write(t, personal, []byte("keep"))
			if err := Recover(journal, active); err != nil {
				t.Fatal(err)
			}
			exists(t, journal, false)
			exists(t, personal, true)
			if commit == "no" {
				exists(t, audio, false)
				exists(t, filepath.Join(source.Songs, "123-exhigh.mp3"), true)
				checkSnapshot(t, target.Browse, "")
				checkSnapshot(t, target.Artwork, "")
				warning, err := MoveLayoutWithOptions(source, target.Root, true, false, func() error { return nil }, Options{Journal: journal})
				if err != nil || warning != nil {
					t.Fatal("same-target retry failed", warning, err)
				}
			}
			exists(t, audio, true)
			checkSnapshot(t, target.Browse, "cached")
			checkSnapshot(t, target.Artwork, "cached")
		})
	}
}

// A failed journal update is reported and can be retried after the settings commit.
func TestMoveReportsJournalCleanupFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions")
	}
	root := t.TempDir()
	source, target := layout(filepath.Join(root, "old")), layout(filepath.Join(root, "new"))
	journalDir := filepath.Join(root, "data")
	journal := filepath.Join(journalDir, "move.json")
	song(t, source.Songs)
	t.Cleanup(func() {
		if err := os.Chmod(journalDir, 0o700); err != nil {
			t.Error(err)
		}
	})
	warning, err := MoveLayoutWithOptions(source, target.Root, true, false, func() error {
		if err := os.Chmod(journalDir, 0o500); err != nil {
			t.Fatal(err)
		}
		return nil
	}, Options{Journal: journal})
	if err != nil || !errors.Is(warning, os.ErrPermission) {
		t.Fatalf("expected a committed move with a cleanup warning, got %v, %v", warning, err)
	}
	exists(t, journal, true)
	exists(t, filepath.Join(target.Songs, "123-exhigh.mp3"), true)
	if err := os.Chmod(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Recover(journal, target); err != nil {
		t.Fatal(err)
	}
	exists(t, journal, false)
	exists(t, filepath.Join(target.Songs, "123-exhigh.mp3"), true)
}
