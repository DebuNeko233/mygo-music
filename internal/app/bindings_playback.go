/*
为播放视图连接播放器、歌词、封面与播放控制命令。
*/

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	desktopview "netease-music-mygo/internal/desktop/view"
	"netease-music-mygo/internal/desktop/view/components"
	playbackview "netease-music-mygo/internal/desktop/view/playback"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/preferences"
)

func (a *app) bindPlaybackViews() {
	p := &a.views.page
	a.views.daily = desktopview.Daily{Page: p, Model: &a.home, Account: &a.account, Player: func() *player.Player { return a.pl }, Open: a.open, Load: a.loadHome, OpenLogin: a.openLogin, Play: a.playPageQueue, Shuffle: a.shufflePageTracks, ArtistLinks: a.views.artist.Links, SongItems: a.views.songMenu.Items}
	a.views.radio = desktopview.Radio{Page: p, Model: &a.radio, Account: &a.account, Player: func() *player.Player { return a.pl }, PlayFM: a.playFM, Trash: a.trashFM, Exit: a.exitRadio, StartHeart: a.startHeartMode, Like: a.views.like.Button, ArtistLinks: a.views.artist.Links}
	a.views.comments = desktopview.Comments{Page: p, For: a.commentsFor, Load: a.loadComments, ToggleLike: a.toggleCommentLike, Avatar: a.views.artwork.Avatar}

	a.views.playback = playbackview.View{
		Page: p, State: &a.mainViewState.State, Playback: &a.playbackState, Reading: &a.lyricReading,
		Captions: &a.captions, Comments: &a.comments, Config: (*preferences.Settings)(&a.settings), Picked: &a.picked,
		Player: func() *player.Player { return a.pl }, Window: func() *mygo.Window { return a.win }, Quitting: func() bool { return a.quitting }, Notch: func() *playbackview.Notch { return a.notch }, DesktopLyrics: func() *playbackview.DeskLyrics { return a.lyrics }, CursorPoint: func() mygo.Point { return cursorPoint() },
		Theme: func(t *ui.Theme) ui.Theme { return a.settings.uiTheme(t) }, SaveSettings: func() { a.settings.saveSoon() },
		ProgramPane: a.views.podcast.ProgramPane, CommentsPane: a.views.comments.Pane, ArtistLinks: a.views.artist.Links, TrackCover: a.trackCover, LikeButton: a.views.like.Button, SongMenu: a.views.songMenu.Button, SongItems: a.views.songMenu.Items, ModelCard: a.views.settings.ModelCard, LocalLyricMatchButton: a.views.local.LyricMatchButton, Avatar: a.views.artwork.Avatar,
		CommentsFor: a.commentsFor, CommentsTab: a.commentsTab, CommentCount: a.commentCount, CaptionsOn: a.captionsOn, CaptionsShown: a.captionsShown, CaptionsWritten: a.captionsWritten, ModelReady: a.modelReady, CaptionsWriting: a.captionsWriting,
		SyncCaptions: a.syncCaptions, SyncVolume: a.syncVolume, SettingsChanged: a.changed, Redraw: a.redraw, OpenSettings: a.openSettings, ShowMain: a.showMain, ExitRadio: a.exitRadio, SyncWindows: a.syncWindows, Dispatch: a.dispatch, VolumeServices: a.volumeServices, Picture: a.playingPicture,
		TrackArtwork: func(tr *netease.Track, px int, blur bool) components.Image {
			return viewImage(a.trackArtwork(tr, px, blur))
		}, HasTrackCover: a.hasTrackCover,
		BarArtwork: func(tr *netease.Track) components.Image {
			return viewImage(a.playbackTrackArtwork(tr, barCoverRequest))
		}, StageArtwork: func(tr *netease.Track) components.Image {
			return viewImage(a.playbackTrackArtwork(tr, nowPlayingCoverRequest))
		},
		ListenNative: a.native.Listen,
	}
}
