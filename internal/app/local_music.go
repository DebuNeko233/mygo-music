package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/localmedia"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

type localSong struct {
	Track   netease.Track `json:"track"`
	Path    string        `json:"path"`
	Cover   bool          `json:"cover,omitempty"`
	Stamp   int64         `json:"stamp"`
	Size    int64         `json:"size,omitempty"`
	Scanned bool          `json:"scanned,omitempty"`
}

type localCatalog struct {
	LastID  int64       `json:"lastId"`
	Songs   []localSong `json:"songs"`
	Folders []string    `json:"folders,omitempty"`
}

type localMusic struct {
	catalog         localCatalog
	tracks          []netease.Track
	byID            map[int64]int
	missing         map[int64]bool
	path            string
	status          string
	busy            bool
	picking         bool
	check           uint64
	revision        uint64 // searchable catalog or availability changed
	window          *mygo.Window
	temporary       localSong
	temporaryID     int64
	temporaryLyrics keptLyrics
	lyricCache      localLyricCache
	artwork         localArtworkState
	lyricDialog     *localLyricDialog
	pendingFile     string
	openRequest     uint64
}

var localExtensions = []string{"mp3", "flac", "wav", "ogg", "m4a", "aac", "3gp", "3g2"}

func (a *app) initLocalMusic() {
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		a.local.path = filepath.Join(dir, "local-music.json")
	}
	var catalog localCatalog
	if b, err := os.ReadFile(a.local.path); err == nil {
		if json.Unmarshal(b, &catalog) != nil {
			a.local.path, a.local.status = "", "无法读取本地曲库"
			catalog = localCatalog{}
		}
	} else if !os.IsNotExist(err) {
		a.local.path, a.local.status = "", "无法读取本地曲库"
	}
	a.local.replace(catalog)
	if path, lastID, byID := a.local.path, a.local.catalog.LastID, a.local.byID; path != "" && lastID > 0 {
		go pruneLocalLyricCache(path, lastID, byID)
	}
	a.pl.LocalPath = func(id int64) string {
		if t := a.pl.Current(); t != nil && t.Temporary && t.LocalID == id {
			return a.local.temporary.Path
		}
		return a.local.song(id).Path
	}
	a.pl.LocalMissing = a.local.markMissing
	a.pl.LocalLyrics = a.localLyricsLoader
}

// Availability changes invalidate cached suggestions without touching the file-check generation.
func (m *localMusic) setMissing(missing map[int64]bool) {
	m.missing = missing
	m.revision++
}
func (m *localMusic) markMissing(id int64) {
	if m.missing[id] {
		return
	}
	if m.missing == nil {
		m.missing = make(map[int64]bool)
	}
	m.missing[id] = true
	m.revision++
}

func (m *localMusic) replace(catalog localCatalog) {
	m.byID = make(map[int64]int, len(catalog.Songs))
	m.setMissing(make(map[int64]bool))
	m.catalog = catalog
	m.tracks = make([]netease.Track, len(catalog.Songs))
	for i, song := range catalog.Songs {
		m.tracks[i] = song.Track
		m.byID[song.Track.LocalID] = i
		m.catalog.LastID = max(m.catalog.LastID, song.Track.LocalID)
	}
}

func (m *localMusic) song(id int64) localSong {
	if i, ok := m.byID[id]; ok {
		return m.catalog.Songs[i]
	}
	return localSong{}
}

func (a *app) playbackLocalSong(track *netease.Track) localSong {
	if track.Temporary {
		return a.local.temporary
	}
	return a.local.song(track.LocalID)
}

func (a *app) checkLocalFiles() {
	if a.local.busy || len(a.local.catalog.Songs) == 0 {
		return
	}
	a.local.check++
	check, songs := a.local.check, a.local.catalog.Songs
	go func() {
		missing := missingLocalFiles(songs, nil)
		a.post(func() {
			if a.local.check == check {
				a.local.setMissing(missing)
			}
		})
	}()
}

func missingLocalFiles(songs []localSong, present map[string]struct{}) map[int64]bool {
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

func localFilePath(path string) (string, error) {
	full, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(full)
}

func localAudioPath(path string) bool {
	return slices.Contains(localExtensions, strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
}

func inspectLocalSong(path string) (localSong, bool) {
	if !localAudioPath(path) {
		return localSong{}, false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return localSong{}, false
	}
	return inspectLocalSongWithInfo(path, st)
}

func inspectLocalSongWithInfo(path string, st fs.FileInfo) (localSong, bool) {
	duration, err := player.LocalDuration(path)
	if err != nil || duration <= 0 {
		return localSong{}, false
	}
	return readLocalSong(path, st, duration), true
}

func readLocalSong(path string, st fs.FileInfo, duration time.Duration) localSong {
	song := localSong{Path: path, Stamp: st.ModTime().UnixNano(), Size: st.Size()}
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

// changeLocal serializes catalog edits and commits the new index before showing it.
func (a *app) changeLocal(work func(*localCatalog, map[string]struct{}) (bool, string)) {
	if a.local.busy || a.local.path == "" {
		return
	}
	a.local.busy, a.local.status = true, "正在更新本地曲库…"
	a.redrawLocalFolders()
	if a.win != nil {
		a.win.Invalidate()
	}
	a.local.check++
	path, original := a.local.path, a.local.catalog
	catalog := original
	catalog.Songs = slices.Clone(catalog.Songs)
	catalog.Folders = slices.Clone(catalog.Folders)
	go func() {
		present := make(map[string]struct{})
		changed, status := work(&catalog, present)
		if changed {
			if writeSessionJSON(path, catalog) {
				a.local.lyricCache.removeDeleted(path, original.Songs, catalog.Songs)
			} else {
				changed, status = false, "无法保存本地曲库"
			}
		}
		if !changed {
			catalog = original
		}
		missing := missingLocalFiles(catalog.Songs, present)
		a.post(func() {
			a.local.busy, a.local.status = false, status
			var updated []netease.Track
			if changed {
				for _, song := range catalog.Songs {
					old := a.local.song(song.Track.LocalID)
					if old.Path != "" && (old.Path != song.Path || old.Stamp != song.Stamp || old.Size != song.Size) {
						updated = append(updated, song.Track)
					}
				}
				a.local.replace(catalog)
			}
			a.local.setMissing(missing)
			a.pl.UpdateLocalTracks(updated)
			a.redrawLocalFolders()
		})
	}()
}

func (a *app) addLocalFolders(paths []string) {
	if len(paths) == 0 {
		return
	}
	paths = slices.Clone(paths)
	a.changeLocal(func(catalog *localCatalog, present map[string]struct{}) (bool, string) {
		var changed bool
		var invalid int
		for _, path := range paths {
			path, err := localFilePath(path)
			if err != nil {
				invalid++
				continue
			}
			if st, err := os.Stat(path); err != nil || !st.IsDir() {
				invalid++
				continue
			}
			if !slices.Contains(catalog.Folders, path) {
				catalog.Folders = append(catalog.Folders, path)
				changed = true
			}
		}
		if len(catalog.Folders) == 0 {
			return false, "请选择包含可播放音频的文件夹"
		}
		scanned, status := scanLocalFolders(catalog, present)
		if invalid != 0 {
			status += fmt.Sprintf("，%d 个所选项目无法作为文件夹打开", invalid)
		}
		return changed || scanned, status
	})
}

func (a *app) refreshLocalSongs() {
	if a.local.picking || a.local.busy {
		return
	}
	if len(a.local.catalog.Folders) == 0 {
		a.checkLocalFiles()
		return
	}
	a.changeLocal(scanLocalFolders)
}

// scanLocalFolders only reopens audio whose file size or timestamp changed.
func scanLocalFolders(catalog *localCatalog, present map[string]struct{}) (bool, string) {
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
			if !entry.Type().IsRegular() || !localAudioPath(path) {
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
			song, ok := inspectLocalSongWithInfo(path, info)
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

func localPathInFolders(path string, folders []string) bool {
	return slices.ContainsFunc(folders, func(folder string) bool {
		rel, err := filepath.Rel(folder, path)
		return err == nil && filepath.IsLocal(rel)
	})
}

func (a *app) removeLocalFolder(path string) {
	a.changeLocal(func(catalog *localCatalog, _ map[string]struct{}) (bool, string) {
		catalog.Folders = slices.DeleteFunc(catalog.Folders, func(folder string) bool { return folder == path })
		catalog.Songs = slices.DeleteFunc(catalog.Songs, func(song localSong) bool {
			return song.Scanned && !localPathInFolders(song.Path, catalog.Folders)
		})
		return true, "已移除文件夹"
	})
}

func (a *app) relinkLocalSong(id int64, path string) {
	a.changeLocal(func(catalog *localCatalog, present map[string]struct{}) (bool, string) {
		path, err := localFilePath(path)
		if err != nil {
			return false, "无法打开所选文件"
		}
		if slices.ContainsFunc(catalog.Songs, func(song localSong) bool { return song.Path == path && song.Track.LocalID != id }) {
			return false, "所选文件已在曲库中"
		}
		i := slices.IndexFunc(catalog.Songs, func(song localSong) bool { return song.Track.LocalID == id })
		if i < 0 {
			return false, "该歌曲已从曲库移除"
		}
		song, ok := inspectLocalSong(path)
		if !ok {
			return false, "无法打开所选音频"
		}
		song.Track.LocalID = id
		song.Scanned = catalog.Songs[i].Scanned && localPathInFolders(path, catalog.Folders)
		catalog.Songs[i] = song
		present[path] = struct{}{}
		return true, "已重新定位文件"
	})
}

func (a *app) removeLocalSong(id int64) {
	a.changeLocal(func(catalog *localCatalog, _ map[string]struct{}) (bool, string) {
		catalog.Songs = slices.DeleteFunc(catalog.Songs, func(song localSong) bool { return song.Track.LocalID == id })
		return true, "已从曲库移除"
	})
}
