package localcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/userdata"
)

type LyricMatch struct {
	SongID int64       `json:"songId"`
	Lyrics lyrics.Text `json:"lyrics"`
}

func lyricDir(catalogPath string) string {
	return filepath.Join(filepath.Dir(catalogPath), "local-lyrics")
}

func LyricFile(catalogPath string, id int64) string {
	return filepath.Join(lyricDir(catalogPath), strconv.FormatInt(id, 10)+".json")
}

type LyricCache struct {
	mu      sync.Mutex
	deleted map[int64]struct{}
}

func (c *LyricCache) Save(path string, localID int64, match LyricMatch) error {
	if path == "" {
		return os.ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, removed := c.deleted[localID]; removed {
		return os.ErrNotExist
	}
	return userdata.Write(path, diskcache.Encode(match))
}

// Only a committed catalog edit may remove the matched lyrics. A late
// network result cannot recreate their file after the track is removed.
func (c *LyricCache) RemoveDeleted(path string, before, after []Song) {
	if len(after) >= len(before) {
		return
	}
	kept := make(map[int64]bool, len(after))
	for _, song := range after {
		kept[song.Track.LocalID] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted == nil {
		c.deleted = make(map[int64]struct{})
	}
	for _, song := range before {
		if id := song.Track.LocalID; !kept[id] {
			c.deleted[id] = struct{}{}
			_ = os.Remove(LyricFile(path, id))
		}
	}
}

// Clean files left behind by older versions. IDs allocated after this
// startup snapshot are above lastID and still belong to the live catalog.
func PruneLyrics(path string, lastID int64, byID map[int64]int) {
	dir := lyricDir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		stem, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		id, err := strconv.ParseInt(stem, 10, 64)
		if err != nil || id <= 0 || id > lastID || stem != strconv.FormatInt(id, 10) {
			continue
		}
		if _, kept := byID[id]; !kept {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// LoadLyrics reads a confirmed match captured for one local file.
func LoadLyrics(path string) (string, string, string) {
	var match LyricMatch
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &match) == nil && match.SongID > 0 {
		return match.Lyrics.Original, match.Lyrics.Translation, match.Lyrics.Words
	}
	return "", "", ""
}
