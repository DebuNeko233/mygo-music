/*
验证心动模式种子、实时集合读取和请求身份，保留用户新的播放意图。
*/

package radio

import (
	"errors"
	"testing"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
	"netease-music-mygo/internal/ui/viewmodel/internal/testutil"
)

// Heart mode uses the liked current song, a cached playable song, or a fetched
// playlist without needing access to another feature's mutable state.
func TestHeartSeedQueries(t *testing.T) {
	for _, source := range []string{"playing", "cached", "fetched"} {
		t.Run(source, func(t *testing.T) {
			p := player.New(func(func()) {})
			p.SuspendCache()
			player.Cue(p, []netease.Track{{ID: 7}})
			posts := make(chan func(), 8)
			asked := make(chan [3]int64, 1)
			fetched := make(chan int64, 1)
			playlist := &netease.Playlist{ID: 42, Tracks: []netease.Track{{ID: 1, Fee: 1}, {ID: 8}}}
			m := new(Radio)
			s := RadioServices{
				Player: p, Post: func(f func()) { posts <- f },
				Notify:        func(message string) { t.Errorf("unexpected notice: %s", message) },
				LikedPlaylist: func() int64 { return 42 },
				IsLiked:       func(id int64) bool { return source == "playing" && id == 7 },
				CachedPlaylist: func(id int64) *netease.Playlist {
					if id != 42 {
						t.Fatalf("cached playlist = %d", id)
					}
					if source == "cached" {
						return playlist
					}
					return nil
				},
				Playlist: func(id int64) (*netease.Playlist, error) {
					fetched <- id
					return playlist, nil
				},
				Heart: func(last, start, pid int64) ([]netease.Track, error) {
					asked <- [3]int64{last, start, pid}
					tracks := make([]netease.Track, 20)
					for i := range tracks {
						tracks[i].ID = int64(100 + i)
					}
					return tracks, nil
				},
			}
			m.StartHeart(s)
			want := int64(8)
			if source == "playing" {
				want = 7
			}
			if got := testutil.Wait(t, asked, "heart seed"); got != [3]int64{want, want, 42} {
				t.Fatalf("heart request = %v", got)
			}
			testutil.Wait(t, posts, "heart result")()
			if p.Radio() != HeartRadio || p.Current() == nil || p.Current().ID != want {
				t.Fatalf("radio = %q, current = %+v", p.Radio(), p.Current())
			}
			if source == "fetched" {
				if id := testutil.Wait(t, fetched, "playlist"); id != 42 {
					t.Fatalf("fetched playlist = %d", id)
				}
			} else if len(fetched) != 0 {
				t.Fatal("fetched a playlist despite a usable seed")
			}
		})
	}
}

// A retained heart feed reads the current collection identity on each refill,
// and stops requesting songs when that liked playlist disappears.
func TestHeartFeedUsesLiveCollection(t *testing.T) {
	p := player.New(func(func()) {})
	p.SuspendCache()
	player.Cue(p, []netease.Track{{ID: 7}, {ID: 8}})
	posts := make(chan func(), 4)
	asked := make(chan [3]int64, 4)
	pid := int64(42)
	m := new(Radio)
	feed := m.Feed(HeartRadio, RadioServices{
		Player: p, Post: func(f func()) { posts <- f },
		LikedPlaylist: func() int64 { return pid },
		Heart: func(last, start, playlist int64) ([]netease.Track, error) {
			asked <- [3]int64{last, start, playlist}
			return []netease.Track{{ID: 9}}, nil
		},
	})
	for _, id := range []int64{42, 43} {
		pid = id
		var result []netease.Track
		feed(func(tracks []netease.Track) { result = tracks })
		if got := testutil.Wait(t, asked, "refill"); got != [3]int64{8, 7, id} {
			t.Fatalf("refill = %v", got)
		}
		testutil.Wait(t, posts, "refill result")()
		if len(result) != 1 || result[0].ID != 9 {
			t.Fatalf("refill result = %v", result)
		}
	}
	pid = 0
	called := false
	feed(func(tracks []netease.Track) {
		called = true
		if len(tracks) != 0 {
			t.Fatal("refilled without a liked playlist")
		}
	})
	if !called || len(asked) != 0 {
		t.Fatal("missing playlist did not finish the feed immediately")
	}
}

// A-B-A mode changes reject the first A response before the latest A has finished.
func TestRadioSameModeRequestIdentity(t *testing.T) {
	posts := make(chan func(), 16)
	asked := make(chan chan []netease.Track, 4)
	stop := make(chan struct{})
	defer close(stop)
	p := player.New(func(func()) {})
	p.SuspendCache()
	m := new(Radio)
	s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, Notify: func(string) { t.Error("unexpected error") }, PersonalFM: func(string) ([]netease.Track, error) {
		reply := make(chan []netease.Track, 1)
		asked <- reply
		select {
		case tracks := <-reply:
			return tracks, nil
		case <-stop:
			return nil, errors.New("stopped")
		}
	}}
	m.StartFM(FMRadio(netease.FMDefault), s)
	first := testutil.Wait(t, asked, "first A")
	m.StartFM(FMRadio(netease.FMExplore), s)
	middle := testutil.Wait(t, asked, "B")
	m.StartFM(FMRadio(netease.FMDefault), s)
	last := testutil.Wait(t, asked, "last A")
	first <- []netease.Track{{ID: 1}}
	testutil.Wait(t, posts, "old A")()
	middle <- []netease.Track{{ID: 2}}
	testutil.Wait(t, posts, "old B")()
	if p.Radio() != "" || m.Starting != FMRadio(netease.FMDefault) {
		t.Fatal("obsolete response started playback")
	}
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i].ID = int64(100 + i)
	}
	last <- tracks
	testutil.Wait(t, posts, "latest A")()
	if p.Current() == nil || p.Current().ID != 100 || p.Radio() != FMRadio(netease.FMDefault) {
		t.Fatal("latest request did not win")
	}
}

// Selecting a normal queue or exiting while FM loads prevents its later response from replacing that choice.
func TestRadioPendingKeepsManualIntent(t *testing.T) {
	for _, exit := range []bool{false, true} {
		p := player.New(func(func()) {})
		p.SuspendCache()
		m := new(Radio)
		posts := make(chan func(), 4)
		s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, PersonalFM: func(string) ([]netease.Track, error) { return []netease.Track{{ID: 7}}, nil }}
		m.StartFM(FMRadio(netease.FMDefault), s)
		update := testutil.Wait(t, posts, "FM request")
		if exit {
			m.Exit(s)
		} else {
			player.Cue(p, []netease.Track{{ID: 99}})
		}
		update()
		if p.Radio() != "" || m.Starting != "" || (!exit && (p.Current() == nil || p.Current().ID != 99)) {
			t.Fatal("old FM response replaced newer intent")
		}
	}
}

// An old radio's automatic refill must not cancel the explicit switch to another mode.
func TestRadioModeSwitchSurvivesRefill(t *testing.T) {
	p := player.New(func(func()) {})
	p.SuspendCache()
	player.Cue(p, []netease.Track{{ID: 1}})
	var refill func([]netease.Track)
	p.AttachRadio(FMRadio(netease.FMDefault), func(got func([]netease.Track)) { refill = got })
	posts := make(chan func(), 4)
	m := new(Radio)
	tracks := make([]netease.Track, 100)
	for i := range tracks {
		tracks[i].ID = int64(100 + i)
	}
	s := RadioServices{Player: p, Post: func(f func()) { posts <- f }, PersonalFM: func(string) ([]netease.Track, error) { return tracks, nil }}
	m.StartFM(FMRadio(netease.FMExplore), s)
	update := testutil.Wait(t, posts, "new mode")
	if refill == nil {
		t.Fatal("old radio did not request refill")
	}
	refill([]netease.Track{{ID: 2}, {ID: 3}})
	update()
	if p.Radio() != FMRadio(netease.FMExplore) || p.Current() == nil || p.Current().ID != 100 {
		t.Fatal("automatic refill cancelled mode switch")
	}
}
