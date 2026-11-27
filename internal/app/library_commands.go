/*
连接喜欢、收藏及歌单编辑命令，协调登录要求与页面导航。
*/

package app

import (
	"netease-music-mygo/internal/features/netease"
	libraryvm "netease-music-mygo/internal/ui/viewmodel/library"
)

func (a *app) libraryServices() libraryvm.LibraryServices {
	uid, generation := a.listenerID(), a.account.Generation
	return libraryvm.LibraryServices{
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
		Open: func(id int64) { a.open(route{Page: pagePlaylist, ID: id}) }, LeaveDeleted: a.leaveDeletedPlaylist, Notify: func(message string) { a.toast = message },
		VisibleLikedPlaylist: func() int64 {
			if a.nav.Current.Page == pagePlaylist {
				if pl := a.library.Playlists[a.nav.Current.ID]; pl != nil && pl.SpecialType == 5 {
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
	gone := func(r route) bool { return r.Page == pagePlaylist && r.ID == id }
	if r, changed := a.nav.Forget(gone); changed {
		a.show(r)
	}
	a.shell.Pages.Retain(&a.nav)
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
	a.library.Naming = libraryvm.PlaylistName{Open: true, Add: tr}
}

func (a *app) startRename(pl *netease.Playlist) {
	a.library.Naming = libraryvm.PlaylistName{Open: true, Name: pl.Name, Rename: pl.ID}
}

func (a *app) confirmDelete(pl *netease.Playlist) {
	a.library.Deleting = libraryvm.PlaylistDelete{Open: true, ID: pl.ID, Name: pl.Name}
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

// likeCurrent likes the song or episode playing, or unlikes it, from the
// keyboard (⌘D), which shows no heart: a toast says which it did.
func (a *app) likeCurrent() {
	tr := a.pl.Current()
	if tr == nil || tr.IsLocal() {
		return
	}
	a.toggleLike(tr)
	switch {
	case a.account.Profile == nil:
		return // asked to sign in
	case !a.library.IsLiked(tr):
		a.toast = "已取消喜欢"
	case tr.ProgramID != 0:
		a.toast = "已喜欢这期节目"
	default:
		a.toast = "已添加到我喜欢的音乐"
	}
}
