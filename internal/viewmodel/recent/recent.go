/*
独立加载最近播放的各类记录，管理离线快照、刷新时机与账号隔离。
*/

package recent

import (
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

const recentLimit = 300

// RecentServices connects existing record APIs, cache and an asynchronous owner-thread dispatcher.
// Valid scopes all results to the requesting login; CacheCurrent validates the disk-read lifetime.
type RecentServices struct {
	SignedIn     bool
	Post         func(func())
	Valid        func() bool
	Now          func() time.Time
	Cache        func() diskcache.Snapshot
	CacheCurrent func(diskcache.Snapshot) bool
	Songs        func(int) ([]netease.Play, error)
	Playlists    func(int) ([]netease.Played[netease.Playlist], error)
	Albums       func(int) ([]netease.Played[netease.AlbumProfile], error)
	Podcasts     func(int) ([]netease.Played[netease.Podcast], error)
	Voices       func(int) ([]netease.Played[netease.Voice], error)
}

// Recent is what the app has of the listener's recent plays.
type Recent struct {
	Selected        int
	AlbumItems      []netease.AlbumProfile
	generation      uint64
	Tabs            [RecentTabCount]RecentTab
	snapshotChecked bool
	ReadingSnapshot bool
	snapshotDirty   bool
	Songs           []netease.Play
	// The rows use tracks drawn from the record when it arrives.
	Tracks    []netease.Track
	Playlists []netease.Played[netease.Playlist]
	Albums    []netease.Played[netease.AlbumProfile]
	Podcasts  []netease.Played[netease.Podcast]
	Voices    []netease.Played[netease.Voice]
}

type RecentTab struct {
	Loading bool
	Shown   bool
	Failure error
	At      time.Time // when the last request finished
}

func (d *Recent) Loading() bool {
	if d.ReadingSnapshot {
		return true
	}
	for _, tab := range d.Tabs {
		if tab.Loading {
			return true
		}
	}
	return false
}

// SetSongs takes the songs of the record, and the tracks its rows list.
func (d *Recent) SetSongs(songs []netease.Play) {
	d.Songs = songs
	d.Tracks = make([]netease.Track, len(songs))
	for i := range songs {
		d.Tracks[i] = songs[i].Track
	}
}

// RecentSnapshot is what the page keeps of the records between runs.
type RecentSnapshot struct {
	Songs     []netease.Play
	Playlists []netease.Played[netease.Playlist]
	Albums    []netease.Played[netease.AlbumProfile]
	Podcasts  []netease.Played[netease.Podcast]
	Voices    []netease.Played[netease.Voice]
	At        time.Time
	Tabs      uint8 // one bit per available tab; legacy snapshots have no mask
}

// Load refreshes the five records concurrently. Each tab can display
// its own result as soon as it arrives.
func (d *Recent) Load(s RecentServices) {
	if !s.SignedIn || d.ReadingSnapshot {
		return
	}
	if !d.snapshotChecked {
		d.snapshotChecked = true
		if req := s.Cache(); req.Store != nil {
			d.ReadingSnapshot = true
			gen := d.generation
			go func() {
				var kept RecentSnapshot
				at := req.Read(&kept)
				s.Post(func() {
					if d.generation != gen || s.Valid != nil && !s.Valid() {
						return
					}
					d.ReadingSnapshot = false
					if !at.IsZero() && (s.CacheCurrent == nil || s.CacheCurrent(req)) {
						d.SetSongs(kept.Songs)
						d.Playlists = kept.Playlists
						d.SetAlbums(kept.Albums)
						d.Podcasts, d.Voices = kept.Podcasts, kept.Voices
						for i := range d.Tabs {
							d.Tabs[i].Shown = kept.Tabs == 0 || kept.Tabs&(1<<i) != 0
						}
					}
					d.Load(s)
				})
			}()
			return
		}
	}
	now := s.Now()
	for i, tab := range d.Tabs {
		if !tab.Loading && viewmodel.ListDue(tab.At, now) {
			d.LoadTab(i, s)
		}
	}
}

func (d *Recent) LoadTab(tab int, s RecentServices) {
	if !s.SignedIn || tab < 0 || tab >= RecentTabCount || d.ReadingSnapshot || d.Tabs[tab].Loading {
		return
	}
	switch tab {
	case RecentSongs:
		fetchRecent(d, s, tab, s.Songs, d.SetSongs)
	case RecentPlaylists:
		fetchRecent(d, s, tab, s.Playlists, func(v []netease.Played[netease.Playlist]) { d.Playlists = v })
	case RecentAlbums:
		fetchRecent(d, s, tab, s.Albums, d.SetAlbums)
	case RecentPodcasts:
		fetchRecent(d, s, tab, s.Podcasts, func(v []netease.Played[netease.Podcast]) { d.Podcasts = v })
	case RecentVoices:
		fetchRecent(d, s, tab, s.Voices, func(v []netease.Played[netease.Voice]) { d.Voices = v })
	}
}

func fetchRecent[T any](d *Recent, s RecentServices, tab int, fetch func(int) ([]T, error), apply func([]T)) {
	d.Tabs[tab].Loading = true
	gen, req := d.generation, s.Cache()
	post, valid, now := s.Post, s.Valid, s.Now
	go func() {
		items, err := fetch(recentLimit)
		post(func() {
			if d.generation != gen || valid != nil && !valid() {
				return
			}
			state := &d.Tabs[tab]
			state.Loading, state.Failure, state.At = false, err, now()
			if err == nil {
				apply(items)
				state.Shown = true
				d.snapshotDirty = true
			}
			// Coalesce the concurrent replies into one snapshot. A failure
			// preserves any previously available records for that tab.
			if d.snapshotDirty && !d.Loading() {
				d.snapshotDirty = false
				if req.Store != nil {
					d.keep(req, state.At)
				}
			}
		})
	}()
}

func (d *Recent) keep(req diskcache.Snapshot, at time.Time) {
	var tabs uint8
	for i, tab := range d.Tabs {
		if tab.Shown {
			tabs |= 1 << i
		}
	}
	req.Keep(diskcache.Encode(RecentSnapshot{
		Songs:     d.Songs,
		Playlists: d.Playlists,
		Albums:    d.Albums,
		Podcasts:  d.Podcasts,
		Voices:    d.Voices,
		At:        at,
		Tabs:      tabs,
	}))
}

// What the recent page lists, in the order its tabs are in.
const (
	RecentSongs = iota
	RecentPlaylists
	RecentAlbums
	RecentPodcasts
	RecentVoices
	RecentTabCount
)

// Reset retires account-owned jobs/data while preserving the listener's selected tab.
func (d *Recent) Reset() {
	selected, gen := d.Selected, d.generation+1
	*d = Recent{Selected: selected, generation: gen}
}

// NeedsLoad is the cheap view-entry guard; it creates no service closures on idle frames.
func (d *Recent) NeedsLoad(now time.Time) bool {
	if d.ReadingSnapshot {
		return false
	}
	if !d.snapshotChecked {
		return true
	}
	for _, tab := range d.Tabs {
		if !tab.Loading && viewmodel.ListDue(tab.At, now) {
			return true
		}
	}
	return false
}

// SetAlbums publishes album records and the stable input consumed by the existing grid.
func (d *Recent) SetAlbums(albums []netease.Played[netease.AlbumProfile]) {
	d.Albums = albums
	d.AlbumItems = make([]netease.AlbumProfile, len(albums))
	for i := range albums {
		d.AlbumItems[i] = albums[i].Item
	}
}
