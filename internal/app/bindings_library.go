package app

import (
	"github.com/egoist/mygo/ui"

	desktopview "netease-music-mygo/internal/desktop/view"
	"netease-music-mygo/internal/desktop/view/components"
	libraryview "netease-music-mygo/internal/desktop/view/library"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

func (a *app) bindLibraryViews() {
	p := &a.views.page
	a.views.account = desktopview.Account{CopyText: func(text string) error { return copyText(text) }, Notify: func(message string) { a.toast = message }, Page: p, Model: &a.account, OpenLogin: a.openLogin, Settings: a.openSettings, SignOut: a.signOut, Open: a.open, Avatar: a.views.artwork.Avatar, AvatarImage: func(url string) *ui.Bitmap { return components.Bitmap(a.art.Get(url, false)) }}
	a.views.library = libraryview.View{Page: p, Model: &a.library, Account: &a.account, Playback: &a.playbackState, RadioState: &a.radio, Load: a.loadPlaylist, CurrentIndex: a.views.tracks.CurrentIndex, SyncViewport: a.views.tracks.SyncPlaylistViewport, Locate: a.views.tracks.Locate, Play: a.playPageTracks, Shuffle: a.shufflePageTracks, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, Savable: a.savable, Saved: a.library.IsSaved, Editable: a.library.Editable, ToggleSaved: a.toggleSaved, ToggleAlbumSaved: a.toggleAlbumSaved, Radio: func() string { return a.pl.Radio() }, ExitRadio: a.exitRadio, StartHeart: a.startHeartMode, SubmitName: a.submitName, DeleteChosen: a.deleteChosen, Rename: a.startRename, ConfirmDelete: a.confirmDelete, NewPlaylist: a.newPlaylist, AddTo: a.addTo, RemoveFrom: a.removeFrom, Open: a.open}
	a.views.songMenu = desktopview.SongMenu{Player: func() *player.Player { return a.pl }, Copy: func(text string) error { return copyText(text) }, LocalItems: a.localSongItems, Download: func(tracks []netease.Track) { a.enqueueDownloads(tracks) }, OpenArtist: a.openArtist, OpenPage: a.openPage, PlaylistItems: a.views.library.Items}
}
