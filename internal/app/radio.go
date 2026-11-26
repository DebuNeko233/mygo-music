/*
连接私人 FM 与心动模式服务，协调播放、退出和不喜欢操作。
*/

package app

import (
	"netease-music-mygo/internal/features/netease"
	radiovm "netease-music-mygo/internal/ui/viewmodel/radio"
)

var personalFM = netease.PersonalFM

var fmTrash = netease.FMTrash

func (a *app) radioServices() radiovm.RadioServices {
	return radiovm.RadioServices{
		Player: a.pl, Post: a.detailPost(),
		LikedPlaylist:  a.library.LikedPlaylist,
		IsLiked:        func(id int64) bool { return a.library.Liked[id] },
		CachedPlaylist: func(id int64) *netease.Playlist { return a.library.Playlists[id] },
		Notify:         func(message string) { a.toast = message }, NowPlaying: func() { a.playbackState.Open = true },
		PersonalFM: personalFM, Heart: netease.HeartMode, Playlist: netease.PlaylistDetail, Dislike: fmTrash,
	}
}

func (a *app) radioFeed(name string) func(func([]netease.Track)) {
	return a.radio.Feed(name, a.radioServices())
}

func (a *app) playFM() {
	a.radio.StartFM(radiovm.FMRadio(radiovm.FMModes[a.radio.Mode].Mode), a.radioServices())
}

func (a *app) exitRadio() { a.radio.Exit(a.radioServices()) }

func (a *app) trashFM() { a.radio.Trash(a.radioServices()) }

func (a *app) startHeartMode() {
	if a.requireLibraryLogin() {
		a.radio.StartHeart(a.radioServices())
	}
}
