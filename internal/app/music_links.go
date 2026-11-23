package app

import (
	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

var (
	resolveMusicLink     = netease.ResolveMusicLink
	musicLinkTrack       = netease.MusicLinkTrack
	registerMusicLinks   = func() error { return mygo.App.RegisterURLScheme("orpheus") }
	musicLinksRegistered = func() bool { return mygo.App.IsURLSchemeRegistered("orpheus") }
)

func (a *app) linkServices() viewmodel.LinkServices {
	return viewmodel.LinkServices{Ready: a.pl != nil, Player: a.pl, Post: a.detailPost(), Closed: func() bool { return a.quitting }, Show: a.showMain, NowPlaying: func() { a.playbackState.Open = true }, DismissSuggestions: a.dismissSuggestions, Navigate: a.open, Notify: func(msg string) { a.toast = msg }, Resolve: resolveMusicLink, Fetch: musicLinkTrack}
}
func (a *app) cancelMusicLink()                     { a.links.Cancel() }
func (a *app) openMusicInput(text string) bool      { return a.links.Input(text, a.linkServices()) }
func (a *app) openPendingMusicLink()                { a.links.OpenPending(a.linkServices()) }
func (a *app) openMusicLink(link netease.MusicLink) { a.links.Open(link, a.linkServices()) }

func (a *app) refreshMusicLinkHandler() {
	a.links.Registered = musicLinksRegistered()
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) registerMusicLinkHandler() {
	a.links.RegistrationError = ""
	if err := registerMusicLinks(); err != nil {
		a.links.RegistrationError = "设置失败，请重试"
		return
	}
	a.refreshMusicLinkHandler()
	if !a.links.Registered {
		a.links.RegistrationError = "请在系统设置中选择 MyGo"
	}
}
