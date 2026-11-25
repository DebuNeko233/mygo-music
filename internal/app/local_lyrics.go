package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	localvm "netease-music-mygo/internal/viewmodel/local"
)

var searchLocalLyrics = netease.Search

func (a *app) localLyricsServices() localvm.LocalLyricsServices {
	return localvm.LocalLyricsServices{
		Post: a.post, Current: a.pl.Current, Search: searchLocalLyrics, Fetch: lyricsRequest,
		Save: a.local.LyricCache.Save, Reload: a.pl.ReloadLocalLyrics,
	}
}

func (a *app) openLocalLyricMatch() {
	track := a.pl.Current()
	display, loaded := a.pl.LyricsDisplay()
	if track == nil || !track.IsLocal() || !loaded || display.HasSinging() || a.pl.MetadataLoading() {
		return
	}
	a.views.local.LyricScroll = ui.ScrollState{}
	a.local.OpenLyricMatch(track, strings.TrimSpace(track.Name+" "+track.ArtistNames()), a.localLyricsServices())
}

func (a *app) findLocalLyricMatches(d *localvm.LocalLyricMatch) {
	if a.local.SearchLyrics(d, a.localLyricsServices()) {
		a.views.local.LyricScroll = ui.ScrollState{}
	}
}

func (a *app) confirmLocalLyricMatch(d *localvm.LocalLyricMatch) {
	a.local.ConfirmLyrics(d, a.localLyricsServices())
}
