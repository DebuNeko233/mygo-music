package cachemove

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"netease-music-mygo/internal/netease"
)

// Move takes the songs source keeps to target, when migrate, and saves the
// change with save: the cache moves at once, without a restart, and what is
// whole is copied rather than fetched again.
//
// The songs come into a staging directory on the target's own disk, which is
// published by a rename once it holds them all, so a disk of its own is no
// different from another directory. save is called before the songs of the
// source go: a change that cannot be kept leaves the cache as it was, and
// what it copied is removed. The error returned first is what the new cache
// left behind, when some of the old songs could not be removed.
//
// Nothing must read or write either cache while it runs: the caller stops
// them, which is what makes the removal of the source's songs possible.
func Move(source, target string, migrate bool, save func() error) (warning error, err error) {
	return move(source, target, migrate, save, copyFile)
}

func move(source, target string, migrate bool, save func() error, copy func(string, string) error) (warning error, err error) {
	source, target, err = Paths(source, target)
	if err != nil {
		return nil, err
	}
	if err := Validate(source, target); err != nil {
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
	var names []string
	if migrate {
		names, err = songFiles(source)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if err := copy(filepath.Join(source, name), filepath.Join(stage, name)); err != nil {
				return nil, err
			}
		}
	}
	// Look once more: the listener may have put something in the target while
	// the songs came over. Only an empty directory is removed below, and what
	// is cleaned up from it is what this moved.
	if err := Validate(source, target); err != nil {
		return nil, err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(stage, target); err != nil {
		return nil, err
	}
	if err := save(); err != nil {
		for _, name := range names {
			os.Remove(filepath.Join(target, name))
		}
		os.Remove(target)
		return nil, err
	}
	var cleanup error
	for _, name := range names {
		if err := os.Remove(filepath.Join(source, name)); err != nil {
			cleanup = errors.Join(cleanup, err)
		}
	}
	return cleanup, nil
}

// songFiles names the files of the songs dir keeps whole: a stream's .json
// and the song it describes, of the size it says. What is half fetched, of
// another name, or the listener's stays where it is.
func songFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || !entry.Type().IsRegular() {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		id, level, ok := strings.Cut(key, "-")
		n, err := strconv.ParseInt(id, 10, 64)
		if !ok || err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || !slices.Contains(netease.Levels, level) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var stream netease.Stream
		if json.Unmarshal(data, &stream) != nil || (stream.Type != "mp3" && stream.Type != "flac") || stream.Size < 0 {
			continue
		}
		audio := key + "." + stream.Type
		info, err := os.Lstat(filepath.Join(dir, audio))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() != stream.Size {
			continue
		}
		names = append(names, audio, entry.Name())
	}
	return names, nil
}

// copyFile copies a song, or what is said of it, and gives the copy the day
// of the original: the cache plays what it has not played for longest first.
func copyFile(source, target string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	n, err := io.Copy(out, in)
	if err != nil {
		return err
	}
	if n != info.Size() {
		return io.ErrUnexpectedEOF
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return os.Chtimes(target, info.ModTime(), info.ModTime())
}
