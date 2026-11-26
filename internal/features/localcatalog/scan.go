/*
扫描音乐目录和文件变更，仅重新读取大小或时间戳改变的音频元数据。
*/

package localcatalog

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"netease-music-mygo/internal/features/localmedia"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/utils/audioformat"
)

var extensions = audioformat.Extensions()

func MissingFiles(songs []Song, present map[string]struct{}) map[int64]bool {
	missing := make(map[int64]bool)
	for _, song := range songs {
		if _, scanned := present[song.Path]; scanned {
			continue
		}
		if st, err := os.Stat(song.Path); err != nil || !st.Mode().IsRegular() {
			missing[song.Track.LocalID] = true
		}
	}
	return missing
}

func filePath(path string) (string, error) {
	full, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(full)
}

func audioPath(path string) bool {
	return slices.Contains(extensions, strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
}

func Inspect(path string) (Song, bool) {
	if !audioPath(path) {
		return Song{}, false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return Song{}, false
	}
	return inspectSongWithInfo(path, st)
}

func inspectSongWithInfo(path string, st fs.FileInfo) (Song, bool) {
	duration, err := player.LocalDuration(path)
	if err != nil || duration <= 0 {
		return Song{}, false
	}
	return readSong(path, st, duration), true
}

func readSong(path string, st fs.FileInfo, duration time.Duration) Song {
	song := Song{Path: path, Stamp: st.ModTime().UnixNano(), Size: st.Size()}
	song.Track = netease.Track{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Duration: duration.Milliseconds()}
	if meta := localmedia.Tags(path); meta != nil {
		if title := strings.TrimSpace(meta.Title()); title != "" {
			song.Track.Name = title
		}
		if artist := strings.TrimSpace(meta.Artist()); artist != "" {
			song.Track.Artists = []netease.Artist{{Name: artist}}
		}
		song.Track.Album.Name = strings.TrimSpace(meta.Album())
		song.Cover = meta.Picture() != nil
	}
	return song
}

// Scan only reopens audio whose file size or timestamp changed.
func Scan(catalog *Catalog, present map[string]struct{}) (bool, string) {
	known := make(map[string]int, len(catalog.Songs))
	for i, song := range catalog.Songs {
		known[song.Path] = i
	}
	var added, updated, failed int
	for _, folder := range catalog.Folders {
		if st, err := os.Stat(folder); err != nil || !st.IsDir() {
			failed++
			continue
		}
		_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				failed++
				return nil
			}
			if !entry.Type().IsRegular() || !audioPath(path) {
				return nil
			}
			if _, scanned := present[path]; scanned {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				failed++
				return nil
			}
			present[path] = struct{}{}
			i, exists := known[path]
			if exists && catalog.Songs[i].Stamp == info.ModTime().UnixNano() && catalog.Songs[i].Size == info.Size() {
				return nil
			}
			song, ok := inspectSongWithInfo(path, info)
			if !ok {
				failed++
				return nil
			}
			if exists {
				song.Track.LocalID = catalog.Songs[i].Track.LocalID
				song.Scanned = catalog.Songs[i].Scanned
				catalog.Songs[i] = song
				updated++
			} else {
				catalog.LastID++
				song.Track.LocalID, song.Scanned = catalog.LastID, true
				catalog.Songs = append(catalog.Songs, song)
				added++
			}
			return nil
		})
	}
	status := fmt.Sprintf("扫描完成，新增 %d 首，更新 %d 首", added, updated)
	if failed != 0 {
		status += fmt.Sprintf("，%d 个文件或文件夹无法读取", failed)
	}
	return added+updated != 0, status
}

func pathInFolders(path string, folders []string) bool {
	return slices.ContainsFunc(folders, func(folder string) bool {
		rel, err := filepath.Rel(folder, path)
		return err == nil && filepath.IsLocal(rel)
	})
}
