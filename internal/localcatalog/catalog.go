/*
定义并加载本地曲库，维护稳定歌曲身份及查询所需的派生状态。
*/

package localcatalog

import (
	"encoding/json"
	"os"

	"netease-music-mygo/internal/netease"
)

type Song struct {
	Track   netease.Track `json:"track"`
	Path    string        `json:"path"`
	Cover   bool          `json:"cover,omitempty"`
	Stamp   int64         `json:"stamp"`
	Size    int64         `json:"size,omitempty"`
	Scanned bool          `json:"scanned,omitempty"`
}

type Catalog struct {
	LastID  int64    `json:"lastId"`
	Songs   []Song   `json:"songs"`
	Folders []string `json:"folders,omitempty"`
}

// Load reads the existing on-disk schema; an absent file is an empty catalog.
func Load(path string) (Catalog, error) {
	var catalog Catalog
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return catalog, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &catalog)
	}
	return catalog, err
}

// State owns the published catalog and its derived lookup/search data.
// Published snapshots are replaced after persistence; workers never mutate them.
type State struct {
	Catalog  Catalog
	Tracks   []netease.Track
	ByID     map[int64]int
	Missing  map[int64]bool
	Revision uint64
}

// Availability changes invalidate cached suggestions without touching the file-check generation.
func (m *State) SetMissing(missing map[int64]bool) {
	m.Missing = missing
	m.Revision++
}
func (m *State) MarkMissing(id int64) {
	if m.Missing[id] {
		return
	}
	if m.Missing == nil {
		m.Missing = make(map[int64]bool)
	}
	m.Missing[id] = true
	m.Revision++
}

func (m *State) Replace(catalog Catalog) {
	m.ByID = make(map[int64]int, len(catalog.Songs))
	m.SetMissing(make(map[int64]bool))
	m.Catalog = catalog
	m.Tracks = make([]netease.Track, len(catalog.Songs))
	for i, song := range catalog.Songs {
		m.Tracks[i] = song.Track
		m.ByID[song.Track.LocalID] = i
		m.Catalog.LastID = max(m.Catalog.LastID, song.Track.LocalID)
	}
}

func (m *State) Song(id int64) Song {
	if i, ok := m.ByID[id]; ok {
		return m.Catalog.Songs[i]
	}
	return Song{}
}
