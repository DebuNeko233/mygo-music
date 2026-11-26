/*
筛选已完整保存的歌曲，并为缓存文件复制提供进度统计。
*/

package move

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

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/utils/audioformat"
)

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
		if json.Unmarshal(data, &stream) != nil || !audioformat.Canonical(stream.Type) || stream.Size < 0 {
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
