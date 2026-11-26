/*
导出完整歌曲副本，按实际音频格式生成文件名并在补全元数据后发布。
*/

// Package download exports songs through the existing transport and format
// detection, enriching an independent copy before publishing it to the user.
package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	audiotag "netease-music-mygo/internal/features/download/tagging"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/util/audioformat"
)

const (
	NameArtistTitle = "artist-title"
	NameTitleArtist = "title-artist"
	NameTitle       = "title"
)

type Options struct {
	Directory, Quality, NameFormat string
	Sidecar                        bool
	Cache                          *player.Cache
	// Progress is called on the worker; the UI marshals it to its own thread.
	Progress func(stage string, received, total int64)
}

type Result struct {
	Path, Quality, Format string
	// Warning names unavailable source assets, rather than fabricating them.
	Warning string
}

var (
	streamURL    = netease.DownloadURL
	trackDetails = netease.SongDetailsContext
	albumDetail  = netease.AlbumDetailContext
	lyricText    = netease.LyricsContext
	coverImage   = netease.FetchContext
)

// Save leaves existing files and cache originals untouched. A staging folder
// in the destination keeps partial bytes invisible and publication on one disk.
func Save(ctx context.Context, tr netease.Track, opts Options) (result Result, err error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if opts.Directory == "" {
		return result, errors.New("请先选择下载目录")
	}
	st, err := streamURL(ctx, tr, opts.Quality)
	if err != nil {
		return result, fmt.Errorf("获取下载地址：%w", err)
	}
	result.Quality = st.Level
	if err := os.MkdirAll(opts.Directory, 0o755); err != nil {
		return result, fmt.Errorf("创建下载目录：%w", err)
	}
	stage, err := os.MkdirTemp(opts.Directory, ".mygo-download-")
	if err != nil {
		return result, fmt.Errorf("创建下载文件：%w", err)
	}
	defer func() {
		if cleanup := os.RemoveAll(stage); cleanup != nil {
			if result.Path == "" {
				err = errors.Join(err, fmt.Errorf("清理下载临时文件：%w", cleanup))
			} else {
				if result.Warning != "" {
					result.Warning += "；"
				}
				result.Warning += "临时文件清理失败：" + cleanup.Error()
			}
		}
	}()
	raw := filepath.Join(stage, "audio")
	f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return result, err
	}
	report := func(state string, n, total int64) {
		if opts.Progress != nil {
			opts.Progress(state, n, total)
		}
	}
	err = opts.Cache.CopyAudio(ctx, tr.ID, st, f, func(n, total int64) { report("正在下载", n, total) })
	if err != nil {
		f.Close()
		return result, fmt.Errorf("下载音频：%w", err)
	}
	kind := audioformat.Probe(f)
	if e := f.Close(); e != nil {
		return result, e
	}
	if kind == "" {
		return result, errors.New("下载内容不是支持的音频文件")
	}
	result.Format = kind
	if tr.Cloud {
		result.Quality = ""
	} // uploads retain their original encoding, independent of the preference
	path := raw + "." + kind
	if err := os.Rename(raw, path); err != nil {
		return result, err
	}
	report("正在写入标签", 0, 0)
	info, err := loadMetadata(ctx, path, tr)
	if err != nil {
		return result, err
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := audiotag.Write(path, info.tags, info.cover, tr.Cloud); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Flush before making the file visible under its final name.
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	var sidecar string
	if opts.Sidecar && info.lyrics != "" {
		sidecar = filepath.Join(stage, "lyrics.lrc")
		if err := os.WriteFile(sidecar, []byte(info.lyrics+"\n"), 0o600); err != nil {
			return result, err
		}
	}
	base := filename(info.track.ArtistNames(), info.track.Name, opts.NameFormat)
	for suffix := 0; ; suffix++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		name := base
		if suffix > 0 {
			name += fmt.Sprintf(" (%d)", suffix+1)
		}
		target := filepath.Join(opts.Directory, name+"."+kind)
		lrcTarget := filepath.Join(opts.Directory, name+".lrc")
		// Publish atomically without replacing an existing file.
		if sidecar != "" {
			if err := publish(sidecar, lrcTarget); errors.Is(err, os.ErrExist) {
				continue
			} else if err != nil {
				return result, err
			}
		}
		if err := publish(path, target); err != nil {
			if sidecar != "" {
				if rollback := os.Rename(lrcTarget, sidecar); rollback != nil {
					return result, fmt.Errorf("恢复歌词临时文件：%w", rollback)
				}
			}
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return result, fmt.Errorf("保存下载文件：%w", err)
		}
		result.Path = target
		break
	}
	result.Warning = info.warning
	return result, nil
}

func filename(artist, title, nameFormat string) string {
	name := strings.TrimSpace(title)
	// Cloud song names can already carry an audio extension, including one
	// different from the detected container. The final path appends it once.
	for {
		name = strings.TrimRight(name, " .")
		ext := filepath.Ext(name)
		if audioformat.Normalize(ext) == "" {
			break
		}
		name = strings.TrimSuffix(name, ext)
	}

	if name == "" {
		name = "未命名歌曲"
	}
	artist = strings.TrimSpace(artist)
	switch {
	case artist == "" || nameFormat == NameTitle:
	case nameFormat == NameTitleArtist:
		name += " - " + artist
	default:
		name = artist + " - " + name
	}

	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	runes := []rune(strings.Trim(name, " ."))
	// Leave room for the extension and collision suffix on all target systems.
	for len(string(runes)) > 180 {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "未命名歌曲"
	}
	return string(runes)
}
