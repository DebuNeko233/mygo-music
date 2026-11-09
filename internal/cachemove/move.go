package cachemove

import (
	"context"
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
// Audio handles and all writers must stop first. Snapshot readers can use the
// source until save rebinds them; originals are retired only after that commit.
func Move(source, target string, migrate bool, save func() error) (warning error, err error) {
	return move(source, target, migrate, save, copyFile)
}

func move(source, target string, migrate bool, save func() error, copy func(string, string) error) (warning error, err error) {
	source, target, err = Paths(source, target)
	if err != nil {
		return nil, err
	}
	var files []cacheFile
	if migrate {
		names, err := songFiles(source)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			files = append(files, cacheFile{source: filepath.Join(source, name), name: name})
		}
	}
	return transferWithOptions(target, files, []string{""}, func(stage string) error { return validateFlatTarget(source, target, stage) }, save, copy, Options{})
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
		var stream struct {
			netease.Stream
			File string `json:"file"`
		}
		if json.Unmarshal(data, &stream) != nil || (stream.Type != "mp3" && stream.Type != "flac") || stream.Size < 0 {
			continue
		}
		audio := key + "." + stream.Type
		if stream.File != "" {
			suffix := strings.TrimSuffix(strings.TrimPrefix(stream.File, key+"-"), "."+stream.Type)
			if !strings.HasPrefix(stream.File, key+"-") || !strings.HasSuffix(stream.File, "."+stream.Type) {
				continue
			}
			if _, err := strconv.ParseInt(suffix, 10, 64); err != nil {
				continue
			}
			audio = stream.File
		}
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
	return copyProgress(context.Background(), source, target, nil)
}

func copyProgress(ctx context.Context, source, target string, progress func(int64)) (err error) {
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
	var destination io.Writer = out
	if progress != nil {
		destination = &copyWriter{writer: out, progress: progress}
	}
	n, err := io.Copy(destination, &copyReader{ctx: ctx, reader: in})
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

type copyReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *copyReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

type copyWriter struct {
	writer   io.Writer
	progress func(int64)
}

func (w *copyWriter) Write(b []byte) (int, error) {
	n, err := w.writer.Write(b)
	if n > 0 {
		w.progress(int64(n))
	}
	return n, err
}
