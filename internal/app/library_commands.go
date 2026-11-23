package app

import (
	"slices"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) libraryServices() viewmodel.LibraryServices {
	uid, generation := a.listenerID(), a.account.Generation
	return viewmodel.LibraryServices{
		UserID: uid, Playlist: a.playlistServices(),
		Post: func(update func()) {
			a.post(func() {
				if a.listenerID() == uid && a.account.Generation == generation {
					update()
				}
			})
		},
		Cache: a.cacheRequest, Mine: userPlaylists, Likes: likedIDs, Albums: subscribedAlbums,
		LikeSong: likeSong, LikeProgram: likeProgram, ProgramsLiked: programsLiked,
		SavePlaylist: savePlaylist, SaveAlbum: subscribeAlbum, Create: createPlaylist, Rename: renamePlaylist, Add: addToPlaylist, Remove: removeFromPlaylist, Delete: deletePlaylist,
		Persist: a.persistLibraryChanges, ForgetSnapshot: a.forgetPlaylistSnapshot,
		Open: func(id int64) { a.open(route{page: pagePlaylist, id: id}) }, LeaveDeleted: a.leaveDeletedPlaylist, Notify: func(message string) { a.toast = message },
		VisibleLikedPlaylist: func() int64 {
			if a.route.page == pagePlaylist {
				if pl := a.library.Playlists[a.route.id]; pl != nil && pl.SpecialType == 5 {
					return pl.ID
				}
			}
			return 0
		},
	}
}
func (a *app) requireLibraryLogin() bool {
	if a.account.Profile != nil {
		return true
	}
	a.showMain()
	a.openLogin()
	return false
}
func (a *app) toggleLike(tr *netease.Track) {
	if tr.IsLocal() || tr.Cloud || !a.requireLibraryLogin() {
		return
	}
	a.library.ToggleLike(tr, a.libraryServices())
}
func (a *app) askProgramLikes(ids []int64) {
	if a.account.Profile != nil && a.library.NeedsProgramLikes(ids) {
		a.library.AskProgramLikes(ids, a.libraryServices())
	}
}
func (a *app) toggleSaved(pl netease.Playlist) {
	if a.requireLibraryLogin() {
		a.library.ToggleSaved(pl, a.libraryServices())
	}
}
func (a *app) toggleAlbumSaved(al netease.AlbumProfile) {
	if a.requireLibraryLogin() {
		a.library.ToggleAlbumSaved(al, a.libraryServices())
	}
}
func (a *app) submitName() {
	if a.account.Profile != nil {
		a.library.SubmitName(a.libraryServices())
	}
}
func (a *app) deleteChosen() {
	if a.account.Profile != nil {
		a.library.DeleteChosen(a.libraryServices())
	}
}
func (a *app) addTo(pid int64, tr *netease.Track) {
	if a.account.Profile != nil {
		a.library.AddTo(pid, tr, a.libraryServices())
	}
}
func (a *app) removeFrom(pid int64, tr *netease.Track) {
	a.library.RemoveFrom(pid, tr, a.libraryServices())
}
func (a *app) leaveDeletedPlaylist(id int64) {
	gone := func(r route) bool { return r.page == pagePlaylist && r.id == id }
	a.history = slices.DeleteFunc(a.history, gone)
	a.forward = slices.DeleteFunc(a.forward, gone)
	if gone(a.route) {
		r := route{page: pageHome}
		if n := len(a.history); n > 0 {
			r, a.history = a.history[n-1], a.history[:n-1]
		}
		a.show(r)
	}
}

func (a *app) savable(pl *netease.Playlist) bool {
	return a.account.Profile == nil || pl.Creator.UserID != a.account.Profile.UserID
}

func (a *app) newPlaylist(tr *netease.Track) {
	if a.account.Profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	a.library.Naming = viewmodel.PlaylistName{Open: true, Add: tr}
}

func (a *app) startRename(pl *netease.Playlist) {
	a.library.Naming = viewmodel.PlaylistName{Open: true, Name: pl.Name, Rename: pl.ID}
}

func (a *app) confirmDelete(pl *netease.Playlist) {
	a.library.Deleting = viewmodel.PlaylistDelete{Open: true, ID: pl.ID, Name: pl.Name}
}

// Existing API functions remain replaceable by integration tests.
var (
	createPlaylist     = netease.CreatePlaylist
	renamePlaylist     = netease.RenamePlaylist
	addToPlaylist      = netease.AddToPlaylist
	removeFromPlaylist = netease.RemoveFromPlaylist
	deletePlaylist     = netease.DeletePlaylist
	savePlaylist       = netease.SubscribePlaylist
	subscribeAlbum     = netease.SubscribeAlbum
	likeSong           = netease.Like
	likeProgram        = netease.LikeProgram
	programsLiked      = netease.ProgramsLiked
)
