/*
验证歌手详情请求合并、分页、离线恢复、关注优先级及失效响应隔离。
*/

package artist

import (
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
	"netease-music-mygo/internal/viewmodel"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

func artistFixture() (*Artists, ArtistServices, chan func()) {
	m := new(Artists)
	m.Reset()
	posts := make(chan func(), 32)
	s := ArtistServices{
		Post: func(update func()) { posts <- update }, Cache: func(string) diskcache.Snapshot { return diskcache.Snapshot{} },
		Detail: func(id int64) (*netease.ArtistProfile, error) {
			return &netease.ArtistProfile{Artist: netease.Artist{ID: id}, AccountID: 99}, nil
		},
		Latest: func(int64, int) ([]netease.Track, error) { return []netease.Track{{ID: 1}}, nil },
		Albums: func(int64, int, int) ([]netease.AlbumProfile, bool, error) {
			return []netease.AlbumProfile{{ID: 1}}, false, nil
		},
		Events: func(int64, int, int64) ([]netease.Event, int64, bool, error) {
			return []netease.Event{{ID: 1}}, 1, false, nil
		},
		Follow: func(int64, bool) error { return nil }, Visible: func() int64 { return 7 }, Notify: func(string) {},
	}
	return m, s, posts
}

// Reset and same-ID replacements retire every kind of old detail completion before flags, data or effects can change.
func TestArtistDetailsRejectRetiredReplies(t *testing.T) {
	commands := map[string]func(*Artists, ArtistServices){
		"profile": func(m *Artists, s ArtistServices) { m.Page(7).Error = "retry"; m.LoadProfile(7, s) },
		"latest":  func(m *Artists, s ArtistServices) { m.LoadLatest(7, s) },
		"albums":  func(m *Artists, s ArtistServices) { m.LoadAlbums(7, s) },
		"events":  func(m *Artists, s ArtistServices) { m.LoadEvents(7, s) },
		"follow":  func(m *Artists, s ArtistServices) { m.ToggleFollow(7, s) },
	}
	for name, command := range commands {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reset=%v", name, reset), func(t *testing.T) {
				m, s, posts := artistFixture()
				m.Page(7).Profile = &netease.ArtistProfile{Artist: netease.Artist{ID: 7}, AccountID: 99}
				effects := 0
				s.Notify = func(string) { effects++ }
				command(m, s)
				done := testutil.Wait(t, posts, "old detail result")
				if reset {
					m.Reset()
				}
				fresh := &ArtistPage{Profile: &netease.ArtistProfile{Artist: netease.Artist{ID: 7}, Followed: true}, Loading: true, LatestLoading: true, Following: true, AlbumsPage: viewmodel.Page{Loading: true}, EventsPage: viewmodel.Page{Loading: true}}
				m.Pages.Set(7, fresh)
				done()
				if m.Pages.Peek(7) != fresh || !fresh.Loading || !fresh.LatestLoading || !fresh.Following || !fresh.AlbumsPage.Loading || !fresh.EventsPage.Loading || !fresh.Profile.Followed || len(fresh.Albums) != 0 || len(fresh.Events) != 0 || effects != 0 {
					t.Fatal("retired result changed replacement state")
				}
			})
		}
	}
}

// A successful follow that finished before an older profile request lands must not be undone by that response.
func TestArtistFollowWinsOverEarlierDetail(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			m, s, posts := artistFixture()
			d := m.Page(7)
			d.Profile = &netease.ArtistProfile{Artist: netease.Artist{ID: 7}, AccountID: 99}
			d.Error = "retry"
			m.LoadProfile(7, s)
			detail := testutil.Wait(t, posts, "detail before follow")
			if failed {
				s.Follow = func(int64, bool) error { return errors.New("refused") }
			}
			m.ToggleFollow(7, s)
			if !d.Profile.Followed || !d.Following {
				t.Fatal("follow was not optimistic")
			}
			testutil.Wait(t, posts, "follow result")()
			detail()
			if d.Profile.Followed == failed || d.Following || d.Loading {
				t.Fatal("old profile overwrote follow/rollback or left a pending flag")
			}
		})
	}
}

// Only one active page request is made, and exhausted cursors cannot keep fetching an artist's events.
func TestArtistDetailCoalescesAndStopsEmptyPagination(t *testing.T) {
	m, s, posts := artistFixture()
	var latest, albums, events atomic.Int32
	s.Latest = func(int64, int) ([]netease.Track, error) { latest.Add(1); return []netease.Track{}, nil }
	s.Albums = func(int64, int, int) ([]netease.AlbumProfile, bool, error) { albums.Add(1); return nil, true, nil }
	s.Events = func(int64, int, int64) ([]netease.Event, int64, bool, error) {
		events.Add(1)
		return []netease.Event{{ID: 1}}, 50, true, nil
	}
	m.Page(7).Profile = &netease.ArtistProfile{Artist: netease.Artist{ID: 7}, AccountID: 99}
	for range 2 {
		m.LoadLatest(7, s)
		m.LoadAlbums(7, s)
		m.LoadEvents(7, s)
	}
	for range 3 {
		testutil.Wait(t, posts, "coalesced result")()
	}
	d := m.Page(7)
	if latest.Load() != 1 || albums.Load() != 1 || events.Load() != 1 || d.AlbumsPage.More || !d.EventsPage.More || d.Latest == nil {
		t.Fatal("requests did not coalesce or empty page still has more")
	}
	m.LoadEvents(7, s)
	testutil.Wait(t, posts, "same event cursor")()
	if d.EventsPage.More || d.EventsBefore != 50 {
		t.Fatal("non-advancing event cursor did not terminate pagination")
	}
}

// Cached album pages remain browsable after the first network failure, and retry replaces them from the failed offset without duplicates.
func TestArtistAlbumsOfflineChainAndRetry(t *testing.T) {
	m, s, posts := artistFixture()
	store := diskcache.New(t.TempDir(), 4<<20)
	defer store.Close()
	s.Cache = func(key string) diskcache.Snapshot {
		return diskcache.Snapshot{Store: store, Scope: "7", Key: key, Epoch: store.Epoch()}
	}
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("artist-albums/7/%d", i)
		store.Put("7", key, diskcache.Encode(KeptArtistAlbums{Albums: []netease.AlbumProfile{{ID: int64(10 + i)}}, More: i == 0}), store.Epoch())
	}
	s.Albums = func(int64, int, int) ([]netease.AlbumProfile, bool, error) { return nil, false, netease.ErrOffline }
	m.LoadAlbums(7, s)
	testutil.Wait(t, posts, "offline chain")()
	testutil.Wait(t, posts, "offline failure")()
	d := m.Page(7)
	if len(d.Albums) != 2 || d.Albums[1].ID != 11 || !d.AlbumsFailed || d.AlbumsPage.More {
		t.Fatal("cached later pages disappeared after page zero failed")
	}
	offset := make(chan int, 1)
	s.Albums = func(_ int64, _ int, start int) ([]netease.AlbumProfile, bool, error) {
		offset <- start
		return []netease.AlbumProfile{{ID: 20}}, false, nil
	}
	m.LoadAlbums(7, s)
	testutil.Wait(t, posts, "retry cached pages")()
	testutil.Wait(t, posts, "retry result")()
	if testutil.Wait(t, offset, "retry offset") != 0 || !reflect.DeepEqual(d.Albums, []netease.AlbumProfile{{ID: 20}}) || d.AlbumsFailed || d.AlbumsPage.Loading {
		t.Fatal("retry appended stale pages or failed to replace offset zero")
	}
}

// The visible events tab starts only once the artist's account ID arrives, and navigating away suppresses that follow-up.
func TestArtistEventsWaitForProfileAndVisibility(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(fmt.Sprint(visible), func(t *testing.T) {
			m, s, posts := artistFixture()
			m.Tab = ArtistEvents
			s.Visible = func() int64 {
				if visible {
					return 7
				}
				return 8
			}
			calls := make(chan int64, 1)
			s.Events = func(uid int64, _ int, before int64) ([]netease.Event, int64, bool, error) {
				calls <- uid
				return nil, 0, false, nil
			}
			m.Load(7, s)
			testutil.Wait(t, posts, "profile result")()
			if visible {
				testutil.Wait(t, posts, "deferred events")()
				if testutil.Wait(t, calls, "artist account") != 99 {
					t.Fatal("events loaded from artist ID instead of account ID")
				}
			} else if len(calls) != 0 {
				t.Fatal("hidden artist started an unnecessary follow-up")
			}
		})
	}
}

// A pending follow stays in the shared retention cache until its completion can publish.
func TestArtistRetentionProtectsFollow(t *testing.T) {
	m, s, posts := artistFixture()
	d := m.Page(7)
	d.Profile = &netease.ArtistProfile{Artist: netease.Artist{ID: 7}}
	m.ToggleFollow(7, s)
	done := testutil.Wait(t, posts, "follow result")
	for id := int64(20); id < 20+pagecache.Limit+2; id++ {
		m.Page(id)
	}
	if m.Pages.Peek(7) != d {
		t.Fatal("pending follow was evicted")
	}
	done()
	m.Page(99)
	if m.Pages.Peek(7) != nil {
		t.Fatal("finished follow remained pinned forever")
	}
}
