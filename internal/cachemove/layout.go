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
// The caller suspends all stores first. A failed copy/save retains every old
// file; only a saved layout may retire its old files, including across disks.
func MoveLayout(source Layout, target string, migrate, shared bool, save func() error) (error, error) {
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
	validate := func() error { return Validate(source.Root, target) }
	if shared {
		validate = func() error { return nil }
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
			reused, err = matches(group.source, destination, names)
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
	return transfer(target, files, publications, validate, save, copyFile)
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
		return false, fmt.Errorf("目标缓存路径不是独立目录：%s", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	if len(entries) != len(names) {
		return false, fmt.Errorf("目标缓存目录已有其他文件：%s", target)
	}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !wanted[entry.Name()] {
			return false, fmt.Errorf("目标缓存目录已有其他文件：%s", target)
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
			return false, fmt.Errorf("目标缓存与旧文件不同：%s", entry.Name())
		}
	}
	return true, nil
}

// transfer stages files on the destination volume. Whole-root moves publish
// once; legacy conversion publishes only managed child folders. Rollback
// removes only files this transaction published, preserving unrelated data.
func transfer(target string, files []cacheFile, publications []string, validate func() error, save func() error, copy func(string, string) error) (error, error) {
	if err := validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".song-cache-move-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	for _, name := range publications {
		if err := os.MkdirAll(filepath.Join(stage, name), 0o700); err != nil {
			return nil, err
		}
	}
	for _, file := range files {
		if file.reused {
			continue
		}
		to := filepath.Join(stage, file.name)
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return nil, err
		}
		if err := copy(file.source, to); err != nil {
			return nil, err
		}
	}
	if err := validate(); err != nil {
		return nil, err
	}
	var published []string
	rollback := func() {
		owned := func(name string) bool {
			for _, root := range published {
				if root == "" || filepath.Dir(name) == root || name == root {
					return true
				}
			}
			return false
		}
		for _, file := range files {
			if !file.reused && owned(file.name) {
				os.Remove(filepath.Join(target, file.name))
			}
		}
		for _, name := range []string{"songs", "browse-v1", ArtworkDir} {
			if owned(name) {
				os.Remove(filepath.Join(target, name))
			}
		}
		for _, name := range published {
			os.Remove(filepath.Join(target, name))
		}
	}
	for _, name := range publications {
		to := filepath.Join(target, name)
		if name != "" {
			if err := os.MkdirAll(target, 0o700); err != nil {
				rollback()
				return nil, err
			}
		}
		if err := os.Remove(to); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollback()
			return nil, err
		}
		if err := os.Rename(filepath.Join(stage, name), to); err != nil {
			rollback()
			return nil, err
		}
		published = append(published, name)
	}
	if err := save(); err != nil {
		rollback()
		return nil, err
	}
	var warning error
	for _, file := range files {
		if err := os.Remove(file.source); err != nil {
			warning = errors.Join(warning, err)
		}
	}
	return warning, nil
}
