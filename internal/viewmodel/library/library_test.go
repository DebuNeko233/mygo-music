package library

import (
	"errors"
	"testing"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

// Reset retires every old operation before it can clear a new pending edit, roll back data or write snapshots.
func TestLibraryResetRejectsRetiredOperations(t *testing.T) {
	commands := map[string]func(*Library, LibraryServices){
		"save-playlist": func(m *Library, s LibraryServices) { m.ToggleSaved(netease.Playlist{ID: 55}, s) },
		"save-album": func(m *Library, s LibraryServices) {
			m.ToggleAlbumSaved(netease.AlbumProfile{Album: netease.Album{ID: 55}}, s)
		},
		"like-song":    func(m *Library, s LibraryServices) { m.ToggleLike(&netease.Track{ID: 1}, s) },
		"like-program": func(m *Library, s LibraryServices) { m.ToggleProgramLike(1, s) },
		"create":       func(m *Library, s LibraryServices) { m.Naming = PlaylistName{Open: true, Name: "old"}; m.SubmitName(s) },
		"rename": func(m *Library, s LibraryServices) {
			m.Naming = PlaylistName{Open: true, Name: "old", Rename: 7}
			m.SubmitName(s)
		},
		"delete":         func(m *Library, s LibraryServices) { m.Deleting = PlaylistDelete{ID: 7}; m.DeleteChosen(s) },
		"remove":         func(m *Library, s LibraryServices) { m.RemoveFrom(7, &netease.Track{ID: 1}, s) },
		"add":            func(m *Library, s LibraryServices) { m.AddTo(7, &netease.Track{ID: 1}, s) },
		"query-program":  func(m *Library, s LibraryServices) { m.AskProgramLikes([]int64{1}, s) },
		"load-playlists": func(m *Library, s LibraryServices) { m.LoadMine(1, s) },
		"load-likes":     func(m *Library, s LibraryServices) { m.LoadLiked(1, s) },
		"load-albums":    func(m *Library, s LibraryServices) { m.LoadAlbums(1, s) },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			var m Library
			m.Reset()
			m.Mine = []netease.Playlist{{ID: 7, Name: "original"}}
			m.Playlists[7] = &netease.Playlist{ID: 7, Tracks: []netease.Track{{ID: 1}}}
			posts := make(chan func(), 4)
			effects := 0
			fail := errors.New("old request failed")
			s := LibraryServices{
				Post: func(fn func()) { posts <- fn }, Cache: func(string) diskcache.Snapshot { return diskcache.Snapshot{} },
				SavePlaylist: func(int64, bool) error { return fail }, SaveAlbum: func(int64, bool) error { return fail }, LikeSong: func(int64, bool) error { return fail }, LikeProgram: func(int64, bool) error { return fail },
				Create: func(string) (*netease.Playlist, error) { return nil, fail }, Rename: func(int64, string) error { return fail }, Delete: func(int64) error { return fail }, Add: func(int64, ...int64) error { return fail }, Remove: func(int64, ...int64) error { return fail },
				ProgramsLiked: func([]int64) (map[int64]bool, error) { return map[int64]bool{1: true}, nil },
				Mine: func(int64) ([]netease.Playlist, []netease.Playlist, error) {
					return []netease.Playlist{{ID: 55}}, nil, nil
				}, Likes: func(int64) ([]int64, error) { return []int64{1}, nil }, Albums: func() ([]netease.AlbumProfile, error) { return nil, nil },
				Persist: func(LibrarySnapshots, ...int64) { effects++ }, Notify: func(string) { effects++ }, LeaveDeleted: func(int64) {},
			}
			command(&m, s)
			completion := testutil.Wait(t, posts, "old library completion")
			m.Reset()
			current := &netease.Playlist{ID: 7, Name: "current"}
			m.Playlists[7] = current
			m.Mine = []netease.Playlist{*current}
			m.SavingPlaylists = map[int64]bool{55: true}
			m.SavingAlbums = map[int64]bool{55: true}
			m.Refresh.Edits = 2
			m.Refresh.Revision = 20
			completion()
			if m.Playlists[7] != current || m.Mine[0].Name != "current" || m.Liked[1] || m.LikedPrograms[1] || !m.SavingPlaylists[55] || !m.SavingAlbums[55] || m.Refresh.Edits != 2 || m.Refresh.Revision != 20 || effects != 0 {
				t.Fatal("retired operation changed the new library lifetime")
			}
		})
	}
}

// An optimistic subscription updates both consumers immediately and rolls back only when its posted failure runs.
func TestLibrarySubscriptionPublishesRollback(t *testing.T) {
	var m Library
	m.Reset()
	detail := &netease.Playlist{ID: 7}
	m.Playlists[7] = detail
	posts := make(chan func(), 1)
	persisted := 0
	s := LibraryServices{Post: func(fn func()) { posts <- fn }, SavePlaylist: func(int64, bool) error { return errors.New("offline") }, Notify: func(string) {}, Persist: func(LibrarySnapshots, ...int64) { persisted++ }}
	m.ToggleSaved(*detail, s)
	completion := testutil.Wait(t, posts, "subscription failure")
	if !detail.Subscribed || len(m.Saved) != 1 || !m.SavingPlaylists[7] || m.Refresh.Edits != 1 || persisted != 0 {
		t.Fatal("worker wrote state before owner publication")
	}
	completion()
	if detail.Subscribed || len(m.Saved) != 0 || m.SavingPlaylists[7] || m.Refresh.Edits != 0 || persisted != 1 {
		t.Fatal("failed subscription was not restored consistently")
	}
}
