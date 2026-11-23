package viewmodel

import (
	"context"
	"errors"
	"time"

	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
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
	Ready                                bool
	Player                               LinkPlayer
	Post                                 func(func())
	Closed                               func() bool
	Show, NowPlaying, DismissSuggestions func()
	Navigate                             func(navigation.Route)
	Notify                               func(string)
	Resolve                              func(context.Context, netease.MusicLink) (netease.MusicLink, error)
	Fetch                                func(context.Context, netease.MusicLink) (netease.Track, error)
}

// musicLinkState holds at most one cold-start request. A newer link or
// navigation cancels old work before its result can replace the queue.
type Links struct {
	Starting          bool
	Pending           *netease.MusicLink
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
	m.Pending = nil
}

// openMusicInput is shared by the search box and the system URL event.
// False leaves ordinary text to the existing search path.
func (m *Links) Input(text string, s LinkServices) bool {
	link, ok, err := netease.ParseMusicLink(text)
	if !ok {
		return false
	}
	m.Cancel()
	s.DismissSuggestions()
	if err != nil {
		s.Notify(LinkError(err))
		return true
	}
	if m.Starting || !s.Ready {
		m.Pending = &link
	} else {
		m.Open(link, s)
	}
	return true
}

// openPendingMusicLink runs after account and playback restoration, so
// restoring the saved queue cannot overwrite the song just opened.
func (m *Links) OpenPending(s LinkServices) {
	m.Starting = false
	if link := m.Pending; link != nil {
		m.Pending = nil
		m.Open(*link, s)
	}
}

func (m *Links) Open(link netease.MusicLink, s LinkServices) {
	s.Show()
	if link.Kind == "short" {
		ctx, generation := m.request()
		resolve := s.Resolve
		go func() {
			resolved, err := resolve(ctx, link)
			s.Post(func() {
				if s.Closed() || generation != m.generation {
					return
				}
				m.Cancel()
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
