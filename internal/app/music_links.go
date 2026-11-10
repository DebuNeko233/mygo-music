package app

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
)

var (
	resolveMusicLink     = netease.ResolveMusicLink
	musicLinkTrack       = netease.MusicLinkTrack
	registerMusicLinks   = func() error { return mygo.App.RegisterURLScheme("orpheus") }
	musicLinksRegistered = func() bool { return mygo.App.IsURLSchemeRegistered("orpheus") }
)

// musicLinkState holds at most one cold-start request. A newer link or
// navigation cancels old work before its result can replace the queue.
type musicLinkState struct {
	starting          bool
	pending           *netease.MusicLink
	generation        uint64
	cancel            context.CancelFunc
	registered        bool
	registrationError string
}

func (a *app) cancelMusicLink() {
	a.links.generation++
	if a.links.cancel != nil {
		a.links.cancel()
		a.links.cancel = nil
	}
	a.links.pending = nil
}

// openMusicInput is shared by the search box and the system URL event.
// False leaves ordinary text to the existing search path.
func (a *app) openMusicInput(text string) bool {
	link, ok, err := netease.ParseMusicLink(text)
	if !ok {
		return false
	}
	a.cancelMusicLink()
	a.dismissSuggestions()
	if err != nil {
		a.musicLinkError(err)
		return true
	}
	if a.links.starting || a.pl == nil {
		a.links.pending = &link
	} else {
		a.openMusicLink(link)
	}
	return true
}

// openPendingMusicLink runs after account and playback restoration, so
// restoring the saved queue cannot overwrite the song just opened.
func (a *app) openPendingMusicLink() {
	a.links.starting = false
	if link := a.links.pending; link != nil {
		a.links.pending = nil
		a.openMusicLink(*link)
	}
}

func (a *app) openMusicLink(link netease.MusicLink) {
	a.showMain()
	if link.Kind == "short" {
		ctx, generation := a.musicLinkRequest()
		resolve := resolveMusicLink
		go func() {
			resolved, err := resolve(ctx, link)
			a.post(func() {
				if a.quitting || generation != a.links.generation {
					return
				}
				a.cancelMusicLink()
				if err != nil {
					a.musicLinkError(err)
					return
				}
				a.openMusicLink(resolved)
			})
		}()
		return
	}
	pages := map[string]pageKind{"playlist": pagePlaylist, "album": pageAlbum, "artist": pageArtist, "user": pageUser, "djradio": pagePodcast}
	if page, ok := pages[link.Kind]; ok {
		a.open(route{page: page, id: link.ID})
		return
	}
	if link.Kind != "song" && link.Kind != "program" {
		a.musicLinkError(netease.ErrUnsupportedLink)
		return
	}
	if tr := a.pl.Current(); tr != nil && (link.Kind == "song" && tr.ProgramID == 0 && tr.ID == link.ID || link.Kind == "program" && tr.ProgramID == link.ID) {
		if !a.pl.Playing() {
			a.pl.TogglePlay()
		}
		a.nowPlaying = true
		return
	}
	for i, tr := range a.pl.Queue() {
		if link.Kind == "song" && tr.ProgramID == 0 && tr.ID == link.ID || link.Kind == "program" && tr.ProgramID == link.ID {
			if tr.Playable() {
				a.pl.Jump(i)
				a.nowPlaying = true
				return
			}
		}
	}
	ctx, generation := a.musicLinkRequest()
	current, playing := a.pl.Current(), a.pl.Playing()
	fetch := musicLinkTrack
	go func() {
		tr, err := fetch(ctx, link)
		a.post(func() {
			if a.quitting || generation != a.links.generation {
				return
			}
			a.cancelMusicLink()
			// A later manual play or pause takes precedence over the pending link.
			if a.pl.Current() != current || a.pl.Playing() != playing {
				return
			}
			if err != nil {
				a.musicLinkError(err)
				return
			}
			if tr.ID <= 0 || !tr.Playable() {
				a.toast = "此歌曲暂不可播放"
				return
			}
			a.pl.PlayQueue([]netease.Track{tr}, 0)
			a.nowPlaying = true
		})
	}()
}

func (a *app) musicLinkRequest() (context.Context, uint64) {
	a.cancelMusicLink()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.links.cancel = cancel
	return ctx, a.links.generation
}

func (a *app) musicLinkError(err error) {
	switch {
	case errors.Is(err, netease.ErrInvalidLink):
		a.toast = "链接无效"
	case errors.Is(err, netease.ErrUnsupportedLink):
		a.toast = "暂不支持此链接"
	case errors.Is(err, netease.ErrOffline):
		a.toast = "离线，无法打开链接"
	default:
		a.toast = "链接加载失败"
	}
}

func (a *app) refreshMusicLinkHandler() {
	a.links.registered = musicLinksRegistered()
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) registerMusicLinkHandler() {
	a.links.registrationError = ""
	if err := registerMusicLinks(); err != nil {
		a.links.registrationError = "设置失败，请重试"
		return
	}
	a.refreshMusicLinkHandler()
	if !a.links.registered {
		a.links.registrationError = "请在系统设置中选择 MyGo"
	}
}
