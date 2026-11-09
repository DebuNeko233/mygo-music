package cachemove

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"netease-music-mygo/internal/diskcache"
)

// ArtworkDir separates the shared JSONL format from older development caches.
const ArtworkDir = "artwork-v2"

// ErrConflict preserves existing destination files instead of merging or overwriting them.
var ErrConflict = errors.New("目标缓存已有不同内容")

// Layout names all managed caches. Root can also hold unrelated files; they
// are never copied or removed. Legacy layouts may keep these folders apart.
type Layout struct {
	Root, Songs, Browse, Artwork string
}

type cacheFile struct {
	source, name string
	reused       bool
}

var entryName = regexp.MustCompile(`^[a-f0-9]{64}-[a-f0-9]{64}\.entry$`)

// MoveLayout publishes a new root. shared publishes only managed children,
// for the system cache root or an in-place legacy conversion.
// The caller freezes writers first. A failed copy/save retains every old file;
// save rebinds snapshot readers before originals retire, including across disks.
func MoveLayout(source Layout, target string, migrate, shared bool, save func() error) (error, error) {
	return MoveLayoutWithOptions(source, target, migrate, shared, save, Options{})
}

// MoveLayoutWithOptions adds cancellation, progress and crash recovery to the
// same transaction. Read-only stores may remain open until save commits.
func MoveLayoutWithOptions(source Layout, target string, migrate, shared bool, save func() error, options Options) (error, error) {
	if err := Recover(options.Journal); err != nil {
		return nil, err
	}
	var err error
	if shared {
		target, err = resolve(target)
	} else {
		_, target, err = Paths(source.Root, target)
	}
	if err != nil {
		return nil, err
	}
	var files []cacheFile
	publications := []string{""}
	if shared {
		publications = nil
	}
	groups := []struct{ name, source string }{{"songs", source.Songs}, {"browse-v1", source.Browse}, {ArtworkDir, source.Artwork}}
	validate := func(stage string) error { return validateTarget(source.Root, target, stage) }
	if shared {
		validate = func(string) error { return nil }
		same, err := SameDirectory(source.Root, target)
		if err != nil {
			return nil, err
		}
		if !same {
			if _, _, err := Paths(source.Root, target); err != nil {
				return nil, err
			}
		}
	}
	for _, group := range groups {
		destination := filepath.Join(target, group.name)
		if shared {
			same, err := SameDirectory(group.source, destination)
			if err != nil {
				return nil, err
			}
			if same {
				continue
			}
		} else if _, _, err := Paths(group.source, target); err != nil {
			return nil, err
		}
		var names []string
		var err error
		if migrate {
			if group.name == "songs" {
				names, err = songFiles(group.source)
			} else {
				names, err = entryFiles(group.source)
				if err == nil && group.name == ArtworkDir {
					info, statErr := os.Lstat(filepath.Join(group.source, diskcache.IndexFile))
					if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
						return nil, statErr
					}
					if statErr == nil && info.Mode().IsRegular() {
						names = append(names, diskcache.IndexFile)
					}
				}
			}
			if err != nil {
				return nil, err
			}
		}
		reused := false
		if shared {
			// A crash after publishing a folder but before saving settings can
			// be resumed only when every file still matches its original.
			if migrate {
				reused, err = matches(group.source, destination, names)
			} else {
				// Switching adopts the destination as-is, including caches
				// left there before a previous switch to a custom directory.
				var info os.FileInfo
				info, err = os.Lstat(destination)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				} else if err == nil {
					if !info.IsDir() {
						err = fmt.Errorf("目标缓存路径不是独立目录：%s", destination)
					} else {
						reused = true
					}
				}
			}
			if err != nil {
				return nil, err
			}
			if !reused {
				publications = append(publications, group.name)
			}
		}
		for _, name := range names {
			files = append(files, cacheFile{filepath.Join(group.source, name), filepath.Join(group.name, name), reused})
		}
	}
	return transferWithOptions(target, files, publications, validate, save, nil, options)
}

func entryFiles(dir string) ([]string, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("缓存路径不是独立目录：%s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && entryName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// matches also rejects unrelated destination files, rather than merging them.
func matches(source, target string, names []string) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w：%s", ErrConflict, target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	if len(entries) != len(names) {
		return false, fmt.Errorf("%w：%s", ErrConflict, target)
	}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !wanted[entry.Name()] {
			return false, fmt.Errorf("%w：%s", ErrConflict, target)
		}
		var sums [][]byte
		for _, dir := range []string{source, target} {
			f, err := os.Open(filepath.Join(dir, entry.Name()))
			if err != nil {
				return false, err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return false, err
			}
			sums = append(sums, h.Sum(nil))
		}
		if !bytes.Equal(sums[0], sums[1]) {
			return false, fmt.Errorf("%w：%s", ErrConflict, entry.Name())
		}
	}
	return true, nil
}
