package viewmodel

import (
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/library"
	"netease-music-mygo/internal/netease"
)

type ListRequest struct {
	At                  time.Time
	Used                uint64
	Busy, Retry, Failed bool
	Failure             error
	Removal             *library.PlaylistRemoval
}

func (r *ListRequest) Stale() { r.At, r.Retry = time.Time{}, true }

type LibraryRefresh struct {
	Edits               int
	Revision            uint64
	Playlists           map[int64]*ListRequest
	PlaylistUse         uint64
	EditedLists         map[int64]struct{}
	Mine, Likes, Albums ListRequest
	FindLiked           bool
	Now                 func() time.Time
}
type PlaylistName struct {
	Open   bool
	Name   string
	Rename int64
	Add    *netease.Track
	Busy   bool
}
type PlaylistDelete struct {
	Open bool
	ID   int64
	Name string
}
type LibrarySnapshots uint8

const (
	SnapshotLists LibrarySnapshots = 1 << iota
	SnapshotAlbums
	SnapshotLikes
)

// Library combines shared collection data with account-scoped requests and edits.
// All commands/results run on the owner thread; only I/O runs in workers.
type Library struct {
	library.State
	SavingPlaylists, SavingAlbums map[int64]bool
	Refresh                       LibraryRefresh
	Naming                        PlaylistName
	Deleting                      PlaylistDelete
	Loading, PlayWhenLoaded       map[int64]bool
	Errors                        map[int64]string
	DirtySnapshots                map[int64]bool
	Dirty                         LibrarySnapshots
	generation                    uint64
}

func (m *Library) Reset() {
	m.generation++
	m.State = library.State{Playlists: map[int64]*netease.Playlist{}}
	m.SavingPlaylists, m.SavingAlbums = nil, nil
	m.Refresh = LibraryRefresh{Now: m.Refresh.Now}
	m.Naming, m.Deleting = PlaylistName{}, PlaylistDelete{}
	m.Loading, m.PlayWhenLoaded, m.Errors = map[int64]bool{}, map[int64]bool{}, map[int64]string{}
	m.DirtySnapshots, m.Dirty = nil, 0
}
func (m *Library) BeginEdit() func() {
	gen := m.generation
	m.Refresh.Edits++
	m.Refresh.Revision++
	return func() {
		if m.generation == gen {
			m.Refresh.Edits--
			m.Refresh.Revision++
		}
	}
}
func (m *Library) Now() time.Time {
	if m.Refresh.Now != nil {
		return m.Refresh.Now()
	}
	return time.Now()
}

// LibraryServices reuses existing API, cache and app-level navigation/playback.
// Post must discard callbacks from a retired account generation.
type LibraryServices struct {
	Post                                                           func(func())
	Cache                                                          func(string) diskcache.Snapshot
	Mine                                                           func(int64) ([]netease.Playlist, []netease.Playlist, error)
	Likes                                                          func(int64) ([]int64, error)
	Albums                                                         func() ([]netease.AlbumProfile, error)
	LikeSong, LikeProgram                                          func(int64, bool) error
	ProgramsLiked                                                  func([]int64) (map[int64]bool, error)
	SavePlaylist, SaveAlbum                                        func(int64, bool) error
	Create                                                         func(string) (*netease.Playlist, error)
	Rename                                                         func(int64, string) error
	Add, Remove                                                    func(int64, ...int64) error
	Delete                                                         func(int64) error
	Persist                                                        func(LibrarySnapshots, ...int64)
	Revalidate, PlaylistChanged, Forget, Touch, Open, LeaveDeleted func(int64)
	Notify                                                         func(string)
	VisibleLikedPlaylist                                           func() int64
}

// withinLifetime rejects results after Reset even when the caller uses a plain
// test dispatcher. The app's Post additionally validates the current account.
func (m *Library) withinLifetime(s LibraryServices) LibraryServices {
	generation, post := m.generation, s.Post
	s.Post = func(update func()) {
		post(func() {
			if m.generation == generation {
				update()
			}
		})
	}
	return s
}
