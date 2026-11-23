package artist

import (
	"fmt"
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
	"netease-music-mygo/internal/viewmodel"
)

type ArtistPage struct {
	Profile        *netease.ArtistProfile
	Loading        bool
	Error          string
	Failure        error
	Latest         []netease.Track
	LatestLoading  bool
	LatestError    string
	LatestFailure  error
	Albums         []netease.AlbumProfile
	AlbumsPage     viewmodel.Page
	AlbumsRetry    int
	AlbumsFailed   bool
	Events         []netease.Event
	EventsPage     viewmodel.Page
	EventsBefore   int64
	Following      bool
	followRevision uint64
}

const (
	ArtistHot = iota
	ArtistLatest
	ArtistAlbums
	ArtistEvents
)
const (
	latestCount = 100
	albumsPage  = 30
	eventsPage  = 20
)

// Artists owns cached detail requests, pagination and follow commands on the owner thread.
type Artists struct {
	Pages pagecache.Cache[int64, *ArtistPage]
	Tab   int
}

func (m *Artists) Reset() {
	m.Pages = pagecache.New[int64, *ArtistPage](func(d *ArtistPage) bool {
		return d != nil && (d.Loading || d.LatestLoading || d.AlbumsPage.Loading || d.EventsPage.Loading || d.Following)
	})
}

type ArtistServices struct {
	Post    func(func())
	Cache   func(string) diskcache.Snapshot
	Detail  func(int64) (*netease.ArtistProfile, error)
	Latest  func(int64, int) ([]netease.Track, error)
	Albums  func(int64, int, int) ([]netease.AlbumProfile, bool, error)
	Events  func(int64, int, int64) ([]netease.Event, int64, bool, error)
	Follow  func(int64, bool) error
	Visible func() int64
	Notify  func(string)
}

func (m *Artists) Page(id int64) *ArtistPage {
	d := m.Pages.Get(id)
	if d == nil {
		d = &ArtistPage{}
		m.Pages.Set(id, d)
	}
	return d
}

// loadArtist loads an artist's page, and what it lists.
func (m *Artists) Load(id int64, s ArtistServices) {
	d := m.Page(id)
	switch m.Tab {
	case ArtistLatest:
		m.LoadLatest(id, s)
	case ArtistAlbums:
		if !d.AlbumsPage.Loaded {
			m.LoadAlbums(id, s)
		}
	}
	m.LoadProfile(id, s)
}

func (m *Artists) LoadProfile(id int64, s ArtistServices) {
	d := m.Page(id)
	if d.Profile != nil && d.Error == "" || d.Loading {
		if m.Tab == ArtistEvents && !d.EventsPage.Loaded {
			m.LoadEvents(id, s)
		}
		return
	}
	d.Loading = true
	req := s.Cache(fmt.Sprintf("artist/%d", id))
	fetch, revision := s.Detail, d.followRevision
	go func() {
		var kept *netease.ArtistProfile
		if at := req.Read(&kept); !at.IsZero() && kept != nil && kept.ID == id {
			s.Post(func() {
				if m.Pages.Peek(id) == d && d.Profile == nil {
					d.Profile = kept
				}
			})
		}
		ar, err := fetch(id)
		bytes := diskcache.Encode(ar)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Loading = false
			d.Failure = err
			if err != nil {
				d.Error = "歌手加载失败"
				return
			}
			if d.Following || d.followRevision != revision {
				return
			}
			d.Profile = ar
			d.Error = ""
			req.Keep(bytes)
			// Their events are their account's, which their profile names.
			if m.Tab == ArtistEvents && s.Visible() == id {
				m.LoadEvents(id, s)
			}
		})
	}()
}

func (m *Artists) LoadLatest(id int64, s ArtistServices) {
	d := m.Page(id)
	if d.Latest != nil && d.LatestError == "" || d.LatestLoading {
		return
	}
	d.LatestLoading = true
	req := s.Cache(fmt.Sprintf("artist-latest/%d", id))
	fetch := s.Latest
	go func() {
		var kept []netease.Track
		if at := req.Read(&kept); !at.IsZero() {
			s.Post(func() {
				if m.Pages.Peek(id) == d && d.Latest == nil {
					d.Latest = kept
				}
			})
		}
		songs, err := fetch(id, latestCount)
		bytes := diskcache.Encode(songs)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.LatestLoading = false
			d.LatestFailure = err
			if err != nil {
				d.LatestError = "歌曲加载失败"
				return
			}
			d.Latest = songs
			d.LatestError = ""
			req.Keep(bytes)
		})
	}()
}

type KeptArtistAlbums struct {
	Albums []netease.AlbumProfile
	More   bool
}

// loadAlbums loads the next page of an artist's albums.
func (m *Artists) LoadAlbums(id int64, s ArtistServices) {
	d := m.Page(id)
	if d.AlbumsPage.Loading {
		return
	}
	d.AlbumsPage.Loading, d.AlbumsPage.Error = true, ""
	offset := len(d.Albums)
	if d.AlbumsFailed {
		offset = d.AlbumsRetry
	}
	req := s.Cache(fmt.Sprintf("artist-albums/%d/%d", id, offset))
	fetch := s.Albums
	go func() {
		// Restore already visited pages together, so an offline failure of
		// page zero does not prevent browsing the cached pages after it.
		var cached []netease.AlbumProfile
		var cachedAt time.Time
		cachedMore := false
		for page, cursor := 0, offset; page < 128; page++ {
			part := req
			part.Key = fmt.Sprintf("artist-albums/%d/%d", id, cursor)
			var kept KeptArtistAlbums
			at := part.Read(&kept)
			if at.IsZero() {
				break
			}
			cached = append(cached, kept.Albums...)
			cachedMore = kept.More
			if cachedAt.IsZero() {
				cachedAt = at
			}
			if !kept.More || len(kept.Albums) == 0 {
				break
			}
			cursor += len(kept.Albums)
		}
		if !cachedAt.IsZero() {
			s.Post(func() {
				if m.Pages.Peek(id) == d && len(d.Albums) == offset {
					d.Albums = append(d.Albums, cached...)
					d.AlbumsPage.Loaded, d.AlbumsPage.More = true, cachedMore
				}
			})
		}
		albums, more, err := fetch(id, albumsPage, offset)
		bytes := diskcache.Encode(KeptArtistAlbums{albums, more})
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.AlbumsPage.Loading = false
			d.AlbumsPage.Failure = err
			if err != nil {
				d.AlbumsPage.Error = "专辑加载失败"
				d.AlbumsFailed, d.AlbumsRetry = true, offset
				return
			}
			d.Albums = append(d.Albums[:offset], albums...)
			d.AlbumsFailed = false
			req.Keep(bytes)
			d.AlbumsPage.Loaded, d.AlbumsPage.More = true, more && len(albums) > 0
		})
	}()
}

// loadEvents loads the next page of an artist's events, once their profile
// says whose they are.
func (m *Artists) LoadEvents(id int64, s ArtistServices) {
	d := m.Page(id)
	if d.Profile == nil || d.Profile.AccountID == 0 || d.EventsPage.Loading {
		return
	}
	d.EventsPage.Loading, d.EventsPage.Error, d.EventsPage.Failure = true, "", nil
	uid, before := d.Profile.AccountID, d.EventsBefore
	if !d.EventsPage.Loaded {
		before = -1
	}
	fetch := s.Events
	go func() {
		events, last, more, err := fetch(uid, eventsPage, before)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.EventsPage.Loading = false
			if err != nil {
				d.EventsPage.Error, d.EventsPage.Failure = "动态加载失败", err
				return
			}
			// The first page replaces what an earlier one kept, and the
			// ones after it follow on: a page loaded again must not keep
			// its events twice.
			if before < 0 {
				d.Events = events
			} else {
				d.Events = append(d.Events, events...)
			}
			d.EventsPage.Loaded = true
			// A page that starts no earlier would load forever.
			d.EventsPage.More = more && len(events) > 0 && (before == -1 || last < before)
			d.EventsBefore = last
		})
	}()
}

// toggleFollow follows an artist, or unfollows them, at once, going back if
// NetEase refuses. Signed out, it asks the listener to sign in.
func (m *Artists) ToggleFollow(id int64, s ArtistServices) {
	d := m.Page(id)
	if d.Profile == nil || d.Following {
		return
	}
	ar := d.Profile
	follow := !ar.Followed
	ar.Followed, d.Following = follow, true
	d.followRevision++
	followArtist := s.Follow
	go func() {
		err := followArtist(id, follow)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Following = false
			switch {
			case err != nil:
				ar.Followed = !follow
				s.Notify("操作失败，请稍后再试")
			case follow:
				s.Notify("已关注")
			default:
				s.Notify("已取消关注")
			}
		})
	}()
}

// NeedsLoad keeps loaded navigation free of service closures and duplicate jobs.
func (m *Artists) NeedsLoad(id int64) bool {
	d := m.Pages.Peek(id)
	if d == nil {
		return true
	}
	if d.Profile == nil && !d.Loading || d.Error != "" && !d.Loading {
		return true
	}
	switch m.Tab {
	case ArtistLatest:
		return !d.LatestLoading && (d.Latest == nil || d.LatestError != "")
	case ArtistAlbums:
		return !d.AlbumsPage.Loaded && !d.AlbumsPage.Loading
	case ArtistEvents:
		return d.Profile != nil && d.Profile.AccountID != 0 && !d.EventsPage.Loaded && !d.EventsPage.Loading
	}
	return false
}
