package viewmodel

import (
	"fmt"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
)

type AlbumPage struct {
	Album   *netease.AlbumProfile
	Loading bool
	Error   string
	Failure error
}

// KeptAlbum preserves songs omitted by AlbumProfile's API JSON representation.
type KeptAlbum struct {
	Album *netease.AlbumProfile
	Songs []netease.Track
}

// Albums owns account-scoped detail requests and offline snapshots.
type Albums struct {
	Pages pagecache.Cache[int64, *AlbumPage]
}

func (m *Albums) Reset() {
	m.Pages = pagecache.New[int64, *AlbumPage](func(d *AlbumPage) bool { return d != nil && d.Loading })
}

type AlbumServices struct {
	Post   func(func())
	Cache  func(string) diskcache.Snapshot
	Detail func(int64) (*netease.AlbumProfile, error)
}

func (m *Albums) Load(id int64, s AlbumServices) {
	d := m.Pages.Get(id)
	if d == nil {
		d = &AlbumPage{}
		m.Pages.Set(id, d)
	}
	if d.Album != nil && d.Error == "" || d.Loading {
		return
	}
	d.Loading = true
	req := s.Cache(fmt.Sprintf("album/%d", id))
	fetch := s.Detail
	go func() {
		var kept KeptAlbum
		if at := req.Read(&kept); !at.IsZero() && kept.Album != nil && kept.Album.ID == id {
			kept.Album.Songs = kept.Songs
			s.Post(func() {
				if m.Pages.Peek(id) == d && d.Album == nil {
					d.Album = kept.Album
				}
			})
		}
		al, err := fetch(id)
		var bytes []byte
		if err == nil && al != nil {
			bytes = diskcache.Encode(KeptAlbum{al, al.Songs})
		}
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Loading = false
			d.Failure = err
			if err != nil {
				d.Error = "专辑加载失败"
				return
			}
			d.Album = al
			d.Error = ""
			req.Keep(bytes)
		})
	}()
}

func (m *Albums) NeedsLoad(id int64) bool {
	d := m.Pages.Peek(id)
	return d == nil || !d.Loading && (d.Album == nil || d.Error != "")
}
