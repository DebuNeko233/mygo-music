package radio

import (
	"math/rand/v2"
	"strings"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

const HeartRadio = "heart"

var FMModes = []struct{ Mode, Label string }{{netease.FMDefault, "推荐"}, {netease.FMFamiliar, "熟悉"}, {netease.FMExplore, "探索"}}

func FMRadio(mode string) string { return "fm:" + mode }
func RadioLabel(name string) string {
	if name == HeartRadio {
		return "心动模式"
	}
	for _, m := range FMModes {
		if name == FMRadio(m.Mode) {
			return "私人 FM · " + m.Label
		}
	}
	return "私人 FM"
}

type Radio struct {
	Mode             int
	Synced, Starting string
	generation       uint64
}

func (m *Radio) CancelPending() { m.generation++; m.Starting = "" }
func (m *Radio) Sync(name string) {
	if name == m.Synced {
		return
	}
	m.Synced = name
	for i, mode := range FMModes {
		if name == FMRadio(mode.Mode) {
			m.Mode = i
			return
		}
	}
}

type RadioServices struct {
	Player *player.Player
	// Collection queries run on the owner thread, including each refill.
	// Cached playlists are borrowed read-only for the lifetime of a request.
	LikedPlaylist  func() int64
	IsLiked        func(int64) bool
	CachedPlaylist func(int64) *netease.Playlist
	Post           func(func())
	Notify         func(string)
	NowPlaying     func()
	PersonalFM     func(string) ([]netease.Track, error)
	Heart          func(int64, int64, int64) ([]netease.Track, error)
	Playlist       func(int64) (*netease.Playlist, error)
	Dislike        func(int64) error
}

func playable(tracks []netease.Track) []netease.Track {
	var out []netease.Track
	for _, t := range tracks {
		if t.Playable() {
			out = append(out, t)
		}
	}
	return out
}

// radioFeed returns what gives the radio name more tracks: it loads them
// on a goroutine, and gives them to got on the main thread, none if it
// cannot.
func (m *Radio) Feed(name string, s RadioServices) func(got func([]netease.Track)) {
	return func(got func([]netease.Track)) {
		var fetch func() ([]netease.Track, error)
		switch {
		case strings.HasPrefix(name, "fm:"):
			mode := strings.TrimPrefix(name, "fm:")
			fetch = func() ([]netease.Track, error) { return s.PersonalFM(mode) }
		case name == HeartRadio:
			// More like the last song queued, from the song it began with.
			q, pid := s.Player.Queue(), s.LikedPlaylist()
			if len(q) == 0 || pid == 0 {
				got(nil)
				return
			}
			start, last := q[0].ID, q[len(q)-1].ID
			fetch = func() ([]netease.Track, error) { return s.Heart(last, start, pid) }
		default:
			got(nil)
			return
		}
		go func() {
			// The FM gives a song or three at a time, some of them VIP
			// songs the listener may not play: ask again for those.
			var tracks []netease.Track
			for tries := 0; tries < 3 && len(tracks) == 0; tries++ {
				more, err := fetch()
				if err != nil {
					break
				}
				tracks = playable(more)
			}
			s.Post(func() { got(tracks) })
		}()
	}
}

// playFM plays the personal FM in the mode chosen, or goes on playing it.
func (m *Radio) StartFM(name string, s RadioServices) {
	if s.Player.Radio() == name {
		// A start still on its way would play the mode it asked for when it
		// lands, so it is given up now that this one is wanted.
		m.CancelPending()
		if !s.Player.Playing() {
			s.Player.TogglePlay()
		}
		return
	}
	m.CancelPending()
	m.Starting = name
	generation, revision, origin := m.generation, s.Player.QueueRevision(), s.Player.Radio()
	feed := m.Feed(name, s)
	feed(func(tracks []netease.Track) {
		if m.Starting != name || generation != m.generation {
			return
		}
		m.Starting = ""
		// Refilling the same radio extends its queue without superseding a mode change.
		if revision != s.Player.QueueRevision() && (origin == "" || s.Player.Radio() != origin) {
			return
		}
		if len(tracks) == 0 {
			s.Notify("私人 FM 暂时无法播放，请稍后再试")
			return
		}
		s.Player.PlayRadio(name, tracks, feed)
	})
}

// exitRadio stops the radio playing, but not the song it plays.
func (m *Radio) Exit(s RadioServices) {
	m.CancelPending()
	name := s.Player.Radio()
	if name == "" {
		return
	}
	s.Player.EndRadio()
	label := RadioLabel(name)
	if name != HeartRadio {
		label = "私人 FM"
	}
	s.Notify("已退出" + label)
}

// trashFM skips the song playing on the FM, which plays it no more.
func (m *Radio) Trash(s RadioServices) {
	tr := s.Player.Current()
	if tr == nil {
		return
	}
	id := tr.ID
	send := s.Dislike
	s.Player.Next()
	go func() {
		if err := send(id); err != nil {
			s.Post(func() {
				s.Notify("不喜欢操作未保存，请稍后再试")
			})
		}
	}()
}

// startHeartMode plays songs like one the listener likes: the one playing,
// if they like it, or else one of their liked songs at random.
func (m *Radio) StartHeart(s RadioServices) {
	pid := s.LikedPlaylist()
	if pid == 0 {
		s.Notify("喜欢一些歌曲后，即可开启心动模式")
		return
	}
	if s.Player.Radio() == HeartRadio {
		m.CancelPending()
		s.NowPlaying()
		return
	}
	var seed *netease.Track
	fromPlaying := false
	if cur := s.Player.Current(); cur != nil && s.IsLiked(cur.ID) {
		s := *cur
		seed, fromPlaying = &s, true
	}
	liked := s.CachedPlaylist(pid)
	m.CancelPending()
	m.Starting = HeartRadio
	generation, revision, origin := m.generation, s.Player.QueueRevision(), s.Player.Radio()
	go func() {
		if seed == nil {
			if liked == nil {
				liked, _ = s.Playlist(pid)
			}
			if liked != nil {
				if ok := playable(liked.Tracks); len(ok) > 0 {
					seed = &ok[rand.IntN(len(ok))]
				}
			}
		}
		var more []netease.Track
		var err error
		if seed != nil {
			more, err = s.Heart(seed.ID, seed.ID, pid)
		}
		s.Post(func() {
			if m.Starting != HeartRadio || generation != m.generation {
				return
			}
			m.Starting = ""
			// Refilling the same radio extends its queue without superseding a mode change.
			if revision != s.Player.QueueRevision() && (origin == "" || s.Player.Radio() != origin) {
				return
			}
			if seed == nil || err != nil {
				s.Notify("心动模式暂时无法开启，请稍后再试")
				return
			}
			more = playable(more)
			if fromPlaying && s.Player.Current() != nil && s.Player.Current().ID == seed.ID {
				s.Player.RadioFrom(HeartRadio, more, m.Feed(HeartRadio, s))
				return
			}
			s.Player.PlayRadio(HeartRadio, append([]netease.Track{*seed}, more...), m.Feed(HeartRadio, s))
		})
	}()
}
