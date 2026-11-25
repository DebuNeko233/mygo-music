/*
管理用户公开资料与歌单，并为当前账号加载私有关注和订阅内容。
*/

package user

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
)

type UserPage struct {
	User         *netease.User
	Playlists    []netease.Playlist
	Loading      int
	Error        string
	Artists      []netease.ArtistProfile
	ArtistsShown bool
	SubsLoading  int
}

// Users owns public profiles and the current listener's followed artists.
type Users struct {
	Pages pagecache.Cache[int64, *UserPage]
}

func (m *Users) Reset() {
	m.Pages = pagecache.New[int64, *UserPage](func(d *UserPage) bool { return d != nil && (d.Loading > 0 || d.SubsLoading > 0) })
}

type UserServices struct {
	Post      func(func())
	Detail    func(int64) (*netease.User, error)
	Playlists func(int64) ([]netease.Playlist, []netease.Playlist, error)
	Artists   func() ([]netease.ArtistProfile, error)
}

func (m *Users) Load(id, listener int64, s UserServices) {
	d := m.Pages.Get(id)
	if d == nil {
		d = &UserPage{}
		m.Pages.Set(id, d)
	}
	if listener != 0 && listener == id {
		m.loadSubscriptions(id, d, s)
	}
	if d.User != nil || d.Loading > 0 {
		return
	}
	d.Loading, d.Error = 2, ""
	detail, lists := s.Detail, s.Playlists
	go func() {
		u, err := detail(id)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Loading--
			if err != nil {
				d.Error = "用户加载失败"
				return
			}
			d.User = u
		})
	}()
	go func() {
		made, _, err := lists(id)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Loading--
			if err == nil {
				d.Playlists = publicPlaylists(made, id)
			}
		})
	}()
}

// loadSubscriptions loads the artists the listener follows and the
// podcasts they subscribe to, each time their page shows: they follow and
// subscribe on other pages, and these keep up. What they had shows while
// the lists are on their way.
func (m *Users) loadSubscriptions(id int64, d *UserPage, s UserServices) {
	if d.SubsLoading > 0 {
		return
	}
	d.SubsLoading = 1
	artists := s.Artists
	go func() {
		ars, err := artists()
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.SubsLoading--
			if err == nil {
				d.Artists, d.ArtistsShown = ars, true
			}
		})
	}()
}

// publicPlaylists are the playlists of lists a user made that anyone may
// see: their own, as made lists those they saved too, but not their
// private ones.
func publicPlaylists(lists []netease.Playlist, uid int64) []netease.Playlist {
	var out []netease.Playlist
	for _, pl := range lists {
		if pl.Creator.UserID == uid && pl.Privacy != 10 {
			out = append(out, pl)
		}
	}
	return out
}

func (m *Users) NeedsLoad(id, listener int64) bool {
	d := m.Pages.Peek(id)
	return d == nil || d.User == nil && d.Loading == 0 || listener != 0 && listener == id && d.SubsLoading == 0
}
