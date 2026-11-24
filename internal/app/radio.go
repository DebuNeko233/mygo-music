package app

import (
	"netease-music-mygo/internal/netease"
	radiovm "netease-music-mygo/internal/viewmodel/radio"
)

const heartRadio = radiovm.HeartRadio

var fmModes = radiovm.FMModes

func fmRadio(mode string) string { return radiovm.FMRadio(mode) }

func radioLabel(name string) string { return radiovm.RadioLabel(name) }

var personalFM = netease.PersonalFM

var fmTrash = netease.FMTrash

func (a *app) radioServices() radiovm.RadioServices {
	return radiovm.RadioServices{
		Player: a.pl, Library: &a.library, Post: a.detailPost(),
		Notify: func(message string) { a.toast = message }, NowPlaying: func() { a.playbackState.Open = true },
		PersonalFM: personalFM, Heart: netease.HeartMode, Playlist: netease.PlaylistDetail, Dislike: fmTrash,
	}
}

func (a *app) radioFeed(name string) func(func([]netease.Track)) {
	return a.radio.Feed(name, a.radioServices())
}

func (a *app) playFM() { a.radio.StartFM(fmRadio(fmModes[a.radio.Mode].Mode), a.radioServices()) }

func (a *app) exitRadio() { a.radio.Exit(a.radioServices()) }

func (a *app) trashFM() { a.radio.Trash(a.radioServices()) }

func (a *app) startHeartMode() {
	if a.requireLibraryLogin() {
		a.radio.StartHeart(a.radioServices())
	}
}
