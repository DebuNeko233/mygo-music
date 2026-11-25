/*
连接音乐链接解析、系统协议注册与打开流程，协调取消和播放意图。
*/

package app

import (
	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
	linksvm "netease-music-mygo/internal/viewmodel/links"
)

var (
	resolveMusicLink     = netease.ResolveMusicLink
	musicLinkTrack       = netease.MusicLinkTrack
	registerMusicLinks   = func() error { return mygo.App.RegisterURLScheme("orpheus") }
	musicLinksRegistered = func() bool { return mygo.App.IsURLSchemeRegistered("orpheus") }
)

func (a *app) linkServices() linksvm.LinkServices {
	return linksvm.LinkServices{
		Player: a.pl, Post: a.detailPost(), Closed: func() bool { return a.quitting },
		Show: a.showMain, NowPlaying: func() { a.playbackState.Open = true },
		DismissSuggestions: a.searches.DismissSuggestions, Navigate: a.open, Notify: func(message string) { a.toast = message },
		Resolve: resolveMusicLink, Fetch: musicLinkTrack,
	}
}

func (a *app) cancelMusicLink() { a.links.Cancel(); a.startup.pending = nil }

func (a *app) openMusicInput(text string) bool {
	link, ok, err := netease.ParseMusicLink(text)
	if !ok {
		return false
	}
	if a.quitting {
		return true
	}
	a.local.CancelOpen()
	a.cancelMusicLink()
	a.searches.DismissSuggestions()
	if err != nil {
		a.toast = linksvm.LinkError(err)
		return true
	}
	if a.startup.waiting || a.pl == nil {
		a.startup.queue(openingIntent{link: link})
	} else {
		a.openMusicLink(link)
	}
	return true
}

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
