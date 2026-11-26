/*
管理音乐链接解析请求与取消，将有效结果交给现有导航和播放器。
*/

package links

import (
	"context"
	"errors"
	"time"

	"netease-music-mygo/internal/features/navigation"
	"netease-music-mygo/internal/features/netease"
)

// LinkPlayer is the existing player's queue and transport; links never own a second queue.
type LinkPlayer interface {
	Current() *netease.Track
	Playing() bool
	Queue() []netease.Track
	TogglePlay()
	Jump(int)
	PlayQueue([]netease.Track, int)
}
type LinkServices struct {
	Player                               LinkPlayer
	Post                                 func(func())
	Closed                               func() bool
	Show, NowPlaying, DismissSuggestions func()
	Navigate                             func(navigation.Route)
	Notify                               func(string)
	Resolve                              func(context.Context, netease.MusicLink) (netease.MusicLink, error)
	Fetch                                func(context.Context, netease.MusicLink) (netease.Track, error)
}

// Links owns link resolution; application startup owns opening intent ordering.
type Links struct {
	generation        uint64
	cancel            context.CancelFunc
	Registered        bool
	RegistrationError string
}

func (m *Links) Cancel() {
	m.generation++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Links) Open(link netease.MusicLink, s LinkServices) {
	s.Show()
	if link.Kind == "short" {
		ctx, generation := m.request()
		resolve := s.Resolve
		current, playing := s.Player.Current(), s.Player.Playing()
		go func() {
			resolved, err := resolve(ctx, link)
			s.Post(func() {
				if s.Closed() || generation != m.generation {
					return
				}
				m.Cancel()
				if s.Player.Current() != current || s.Player.Playing() != playing {
					return
				}
				if err != nil {
					s.Notify(LinkError(err))
					return
				}
				m.Open(resolved, s)
			})
		}()
		return
	}
	pages := map[string]navigation.Page{"playlist": navigation.Playlist, "album": navigation.Album, "artist": navigation.Artist, "user": navigation.User, "djradio": navigation.Podcast}
	if page, ok := pages[link.Kind]; ok {
		s.Navigate(navigation.Route{Page: page, ID: link.ID})
		return
	}
	if link.Kind != "song" && link.Kind != "program" {
		s.Notify(LinkError(netease.ErrUnsupportedLink))
		return
	}
	if tr := s.Player.Current(); tr != nil && (link.Kind == "song" && tr.ProgramID == 0 && tr.ID == link.ID || link.Kind == "program" && tr.ProgramID == link.ID) {
		if !s.Player.Playing() {
			s.Player.TogglePlay()
		}
		s.NowPlaying()
		return
	}
	for i, tr := range s.Player.Queue() {
		if link.Kind == "song" && tr.ProgramID == 0 && tr.ID == link.ID || link.Kind == "program" && tr.ProgramID == link.ID {
			if tr.Playable() {
				s.Player.Jump(i)
				s.NowPlaying()
				return
			}
		}
	}
	ctx, generation := m.request()
	current, playing := s.Player.Current(), s.Player.Playing()
	fetch := s.Fetch
	go func() {
		tr, err := fetch(ctx, link)
		s.Post(func() {
			if s.Closed() || generation != m.generation {
				return
			}
			m.Cancel()
			// A later manual play or pause takes precedence over the Pending link.
			if s.Player.Current() != current || s.Player.Playing() != playing {
				return
			}
			if err != nil {
				s.Notify(LinkError(err))
				return
			}
			if tr.ID <= 0 || !tr.Playable() {
				s.Notify("此歌曲暂不可播放")
				return
			}
			s.Player.PlayQueue([]netease.Track{tr}, 0)
			s.NowPlaying()
		})
	}()
}

func (m *Links) request() (context.Context, uint64) {
	m.Cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	m.cancel = cancel
	return ctx, m.generation
}

func LinkError(err error) string {
	switch {
	case errors.Is(err, netease.ErrInvalidLink):
		return "链接无效"
	case errors.Is(err, netease.ErrUnsupportedLink):
		return "暂不支持此链接"
	case errors.Is(err, netease.ErrOffline):
		return "离线，无法打开链接"
	default:
		return "链接加载失败"
	}
}
