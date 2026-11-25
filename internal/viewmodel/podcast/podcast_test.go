/*
验证播客加载与播放合并、分页去重、订阅并发回滚及账号生命周期隔离。
*/

package podcast

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
	"netease-music-mygo/internal/viewmodel"
	"netease-music-mygo/internal/viewmodel/internal/testutil"
)

func podcastFixture() (*Podcasts, PodcastServices, chan func()) {
	m := new(Podcasts)
	m.Reset()
	posts := make(chan func(), 32)
	s := PodcastServices{
		SignedIn: true, Post: func(update func()) { posts <- update },
		Detail: func(id int64) (*netease.Podcast, error) {
			return &netease.Podcast{ID: id, Name: fmt.Sprint(id), SubCount: 10}, nil
		},
		Programs: func(id int64, _, offset int) ([]netease.Program, bool, error) {
			p := netease.Program{ID: id*10 + int64(offset), Name: "episode"}
			p.MainSong.ID = id * 100
			return []netease.Program{p}, false, nil
		},
		Subscribe: func(int64, bool) error { return nil }, Subscriptions: func() ([]netease.Podcast, error) { return nil, nil },
		Recommended: func() ([]netease.Podcast, error) { return []netease.Podcast{{ID: 1}}, nil },
		Hot:         func(int, int) ([]netease.Podcast, bool, error) { return []netease.Podcast{{ID: 2}}, false, nil },
		QueryLikes:  func([]int64) {}, Play: func([]netease.Track, int) {}, QueueRevision: func() uint64 { return 0 }, CurrentTrack: func() *netease.Track { return nil }, Notify: func(string) {},
	}
	return m, s, posts
}

// Repeated card plays coalesce and play only after both metadata and episodes arrive, in either arrival order.
func TestPodcastPlayWaitsAndCoalesces(t *testing.T) {
	for _, metadataFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(metadataFirst), func(t *testing.T) {
			m, s, posts := podcastFixture()
			detail, programs := s.Detail, s.Programs
			metadataDone, programsDone := make(chan struct{}), make(chan struct{})
			var metadataCalls, programCalls atomic.Int32
			s.Detail = func(id int64) (*netease.Podcast, error) { metadataCalls.Add(1); <-metadataDone; return detail(id) }
			s.Programs = func(id int64, n, offset int) ([]netease.Program, bool, error) {
				programCalls.Add(1)
				<-programsDone
				return programs(id, n, offset)
			}
			played := 0
			s.Play = func(queue []netease.Track, at int) {
				played++
				if len(queue) != 1 || at != 0 || queue[0].PodcastID != 7 {
					t.Error("wrong prepared episode queue")
				}
			}
			m.RequestPlay(7, s)
			m.RequestPlay(7, s)
			if metadataFirst {
				close(metadataDone)
			} else {
				close(programsDone)
			}
			testutil.Wait(t, posts, "first half")()
			if played != 0 {
				t.Fatal("played before both inputs arrived")
			}
			if metadataFirst {
				close(programsDone)
			} else {
				close(metadataDone)
			}
			testutil.Wait(t, posts, "second half")()
			if played != 1 || metadataCalls.Load() != 1 || programCalls.Load() != 1 || m.Pages.Peek(7).thenPlay {
				t.Fatal("pending play duplicated requests or restarted playback")
			}
		})
	}
}

// A new podcast request or a replacement Player queue supersedes older pending playback.
func TestPodcastPendingPlayKeepsNewerChoice(t *testing.T) {
	for _, otherQueue := range []bool{false, true} {
		t.Run(fmt.Sprint(otherQueue), func(t *testing.T) {
			m, s, posts := podcastFixture()
			var revision uint64
			var played []int64
			s.QueueRevision = func() uint64 { return revision }
			s.Play = func(queue []netease.Track, _ int) { played = append(played, queue[0].PodcastID); revision++ }
			m.RequestPlay(7, s)
			count := 2
			if otherQueue {
				revision++
			} else {
				m.RequestPlay(8, s)
				count = 4
			}
			replies := make([]func(), count)
			for i := range replies {
				replies[i] = testutil.Wait(t, posts, "pending playback result")
			}
			for _, done := range replies {
				done()
			}
			if otherQueue && len(played) != 0 || !otherQueue && !reflect.DeepEqual(played, []int64{8}) {
				t.Fatal("late response replaced a newer playback choice")
			}
		})
	}
}

// Retired metadata and episode replies cannot clear a replacement page's flags, query likes or trigger playback.
func TestPodcastDetailsRejectRetiredReplies(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint(reset), func(t *testing.T) {
			m, s, posts := podcastFixture()
			effects := 0
			s.QueryLikes = func([]int64) { effects++ }
			s.Play = func([]netease.Track, int) { effects++ }
			m.RequestPlay(7, s)
			first, second := testutil.Wait(t, posts, "old detail"), testutil.Wait(t, posts, "old episodes")
			if reset {
				m.Reset()
			}
			fresh := &PodcastPage{Loading: true, Page: viewmodel.Page{Loading: true}, thenPlay: true}
			m.Pages.Set(7, fresh)
			first()
			second()
			if !fresh.Loading || !fresh.Page.Loading || !fresh.thenPlay || fresh.Podcast != nil || len(fresh.Programs) != 0 || effects != 0 {
				t.Fatal("old request changed replacement state or effects")
			}
		})
	}
}

// Overlapping episode pages deduplicate IDs but advance by raw server offsets; empty pages terminate automatic loading.
func TestPodcastProgramsOverlapAndRawCursor(t *testing.T) {
	m, s, posts := podcastFixture()
	offsets := make(chan int, 4)
	s.Programs = func(_ int64, _, offset int) ([]netease.Program, bool, error) {
		offsets <- offset
		var ids []int64
		switch offset {
		case 0:
			ids = []int64{1, 2}
		case 2:
			ids = []int64{2, 3}
		case 4:
		default:
			t.Error("wrong raw offset")
		}
		var out []netease.Program
		for _, id := range ids {
			out = append(out, netease.Program{ID: id})
		}
		return out, true, nil
	}
	for range 3 {
		m.LoadPrograms(7, s)
		m.LoadPrograms(7, s)
		testutil.Wait(t, posts, "episode page")()
	}
	d := m.Pages.Peek(7)
	var ids []int64
	for _, pr := range d.Programs {
		ids = append(ids, pr.ID)
	}
	if !slices.Equal(ids, []int64{1, 2, 3}) || d.Page.More || !d.Page.Loaded {
		t.Fatal("overlap duplicated episodes or empty page kept loading")
	}
	for _, want := range []int{0, 2, 4} {
		if testutil.Wait(t, offsets, "raw offset") != want {
			t.Fatal("deduplication changed server cursor")
		}
	}
}

// Failure retains previous episodes, clears pending play, and retries the same raw page rather than advancing it.
func TestPodcastProgramsFailureRetriesSamePage(t *testing.T) {
	m, s, posts := podcastFixture()
	d := m.page(7)
	d.Programs = []netease.Program{{ID: 1}}
	d.Page = viewmodel.Page{Loaded: true, More: true}
	d.thenPlay = true
	s.Programs = func(int64, int, int) ([]netease.Program, bool, error) { return nil, false, netease.ErrOffline }
	m.LoadPrograms(7, s)
	testutil.Wait(t, posts, "failed episodes")()
	if len(d.Programs) != 1 || d.thenPlay || d.Page.Failure != netease.ErrOffline || d.Page.Loading {
		t.Fatal("failure lost content or left playback pending")
	}
	offset := make(chan int, 1)
	s.Programs = func(_ int64, _, start int) ([]netease.Program, bool, error) {
		offset <- start
		return []netease.Program{{ID: 2}}, false, nil
	}
	m.LoadPrograms(7, s)
	testutil.Wait(t, posts, "episode retry")()
	if testutil.Wait(t, offset, "retried offset") != 1 || len(d.Programs) != 2 || d.Page.Failure != nil {
		t.Fatal("retry changed the failed page offset")
	}
}

// Reset invalidates public home, private subscription and hot-page results before any old flags can be cleared.
func TestPodcastHomeRejectsRetiredReplies(t *testing.T) {
	for _, kind := range []string{"home", "hot", "subscriptions"} {
		t.Run(kind, func(t *testing.T) {
			m, s, posts := podcastFixture()
			count := 1
			switch kind {
			case "home":
				m.LoadHome(s)
				count = 2
			case "hot":
				m.LoadHot(s)
			case "subscriptions":
				m.LoadSubscriptions(s)
			}
			replies := make([]func(), count)
			for i := range replies {
				replies[i] = testutil.Wait(t, posts, "old home reply")
			}
			m.Reset()
			m.Home.Loading = true
			m.Home.HotPage.Loading = true
			m.Home.SubscriptionLoading = true
			for _, done := range replies {
				done()
			}
			if !m.Home.Loading || !m.Home.HotPage.Loading || !m.Home.SubscriptionLoading || len(m.Home.Hot) != 0 || m.Home.SubscriptionsShown {
				t.Fatal("old home reply changed the new account's data")
			}
		})
	}
}

// A slow old subscription list cannot overwrite an accepted subscription; only one fresh follow-up is scheduled.
func TestPodcastSubscriptionEditWinsOverOldList(t *testing.T) {
	m, s, posts := podcastFixture()
	m.Home.SubscriptionsShown = true
	d := m.page(7)
	d.Podcast = &netease.Podcast{ID: 7, SubCount: 10}
	var calls atomic.Int32
	s.Subscriptions = func() ([]netease.Podcast, error) {
		if calls.Add(1) == 1 {
			return nil, nil
		}
		return []netease.Podcast{{ID: 7, Subed: true}}, nil
	}
	m.LoadSubscriptions(s)
	old := testutil.Wait(t, posts, "old subscription list")
	m.ToggleSubscribe(d, s)
	if !m.IsSubscribed(d.Podcast) || len(m.Home.Subs) != 1 {
		t.Fatal("subscription did not update shared data optimistically")
	}
	testutil.Wait(t, posts, "subscription result")()
	old()
	testutil.Wait(t, posts, "fresh subscription list")()
	if calls.Load() != 2 || !m.IsSubscribed(d.Podcast) || d.Subscribing || m.Home.SubscriptionLoading {
		t.Fatal("old list undid the edit or triggered repeated follow-ups")
	}
}

// Subscription rollbacks restore membership and counts without moving a newer subscription behind an older one.
func TestPodcastSubscriptionRollbackPreservesConcurrentEdits(t *testing.T) {
	m, s, posts := podcastFixture()
	m.Home.SubscriptionsShown = true
	m.Home.Subs = []netease.Podcast{{ID: 7, Subed: true, SubCount: 10}, {ID: 1}}
	a, b := m.page(7), m.page(8)
	a.Podcast = &netease.Podcast{ID: 7, Subed: true, SubCount: 10}
	b.Podcast = &netease.Podcast{ID: 8, SubCount: 20}
	s.Subscribe = func(id int64, _ bool) error {
		if id == 7 {
			return errors.New("refused")
		}
		return nil
	}
	m.ToggleSubscribe(a, s)
	m.ToggleSubscribe(b, s)
	for range 2 {
		testutil.Wait(t, posts, "subscription result")()
	}
	var ids []int64
	for _, p := range m.Home.Subs {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []int64{8, 7, 1}) || !a.Podcast.Subed || a.Podcast.SubCount != 10 || !b.Podcast.Subed || b.Podcast.SubCount != 21 || a.Subscribing || b.Subscribing {
		t.Fatal("rollback lost membership, counts or concurrent ordering")
	}
}

// Subscription work remains pinned in the existing page cache and releases that protection when it completes.
func TestPodcastSubscriptionProtectsRetention(t *testing.T) {
	m, s, posts := podcastFixture()
	m.Home.SubscriptionsShown = true
	d := m.page(7)
	d.Podcast = &netease.Podcast{ID: 7, SubCount: 10}
	m.ToggleSubscribe(d, s)
	done := testutil.Wait(t, posts, "pending subscription")
	for id := int64(20); id < 20+pagecache.Limit+2; id++ {
		m.page(id)
	}
	if m.Pages.Peek(7) != d {
		t.Fatal("pending subscription was evicted")
	}
	done()
	m.page(99)
	if m.Pages.Peek(7) != nil {
		t.Fatal("completed subscription remained pinned")
	}
}

// Public recommendations remain usable when hot loading fails, and a hot retry starts at the same cursor.
func TestPodcastHomeKeepsPartialSuccessAndRetry(t *testing.T) {
	m, s, posts := podcastFixture()
	s.SignedIn = false
	s.Hot = func(int, int) ([]netease.Podcast, bool, error) { return nil, false, netease.ErrOffline }
	m.LoadHome(s)
	m.LoadHome(s)
	testutil.Wait(t, posts, "partial home")()
	if len(m.Home.Picks) != 1 || !m.Home.HotPage.Loaded || m.Home.HotPage.Failure != netease.ErrOffline || m.Home.Loading {
		t.Fatal("partial failure discarded recommendations")
	}
	offset := make(chan int, 1)
	s.Hot = func(_, start int) ([]netease.Podcast, bool, error) {
		offset <- start
		return []netease.Podcast{{ID: 2}}, false, nil
	}
	m.Retry(0, true, false, false, s)
	testutil.Wait(t, posts, "hot retry")()
	if testutil.Wait(t, offset, "hot offset") != 0 || len(m.Home.Hot) != 1 || m.Home.HotPage.Failure != nil {
		t.Fatal("hot retry lost its cursor or error state")
	}
}

// Old subscription completions cannot roll back data, emit notices or change edit gates after Reset.
func TestPodcastSubscriptionRejectsRetiredAccount(t *testing.T) {
	m, s, posts := podcastFixture()
	m.Home.SubscriptionsShown = true
	d := m.page(7)
	d.Podcast = &netease.Podcast{ID: 7, SubCount: 10}
	notices := 0
	s.Notify = func(string) { notices++ }
	s.Subscribe = func(int64, bool) error { return errors.New("refused") }
	m.ToggleSubscribe(d, s)
	old := testutil.Wait(t, posts, "retired subscription")
	m.Reset()
	m.subscriptionEdits = 2
	m.Home.Subs = []netease.Podcast{{ID: 8}}
	old()
	if m.subscriptionEdits != 2 || len(m.Home.Subs) != 1 || m.Home.Subs[0].ID != 8 || notices != 0 {
		t.Fatal("retired subscription changed the new account")
	}
}

// A changed subscription snapshot waits for all edits before exactly one follow-up, and a failed follow-up does not loop.
func TestPodcastSubscriptionRefreshWaitsForEdits(t *testing.T) {
	m, s, posts := podcastFixture()
	m.Home.SubscriptionsShown = true
	d := m.page(7)
	d.Podcast = &netease.Podcast{ID: 7, SubCount: 10}
	var calls atomic.Int32
	s.Subscriptions = func() ([]netease.Podcast, error) {
		if calls.Add(1) == 1 {
			return nil, nil
		}
		return nil, netease.ErrOffline
	}
	m.LoadSubscriptions(s)
	list := testutil.Wait(t, posts, "old list")
	m.ToggleSubscribe(d, s)
	edit := testutil.Wait(t, posts, "subscription")
	list()
	if calls.Load() != 1 || !m.subscriptionsRetry || !d.Subscribing {
		t.Fatal("refresh bypassed a pending edit")
	}
	edit()
	testutil.Wait(t, posts, "failed fresh list")()
	if calls.Load() != 2 || m.Home.SubscriptionFailure != netease.ErrOffline || m.Home.SubscriptionLoading || m.subscriptionsRetry || !m.IsSubscribed(d.Podcast) {
		t.Fatal("failed follow-up looped or erased the accepted subscription")
	}
}

// Permanent detail errors wait for a manual retry; successful retry also clears any old pending-play flags.
func TestPodcastDetailRecoveryClassifiesFailures(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		t.Run(fmt.Sprint(temporary), func(t *testing.T) {
			m, s, posts := podcastFixture()
			failure := error(netease.ErrNotLoggedIn)
			if temporary {
				failure = netease.ErrOffline
			}
			s.Detail = func(int64) (*netease.Podcast, error) { return nil, failure }
			m.RequestPlay(7, s)
			for range 2 {
				testutil.Wait(t, posts, "failed detail inputs")()
			}
			d := m.Pages.Peek(7)
			if d.thenPlay || m.pendingPlay != nil || m.NeedsRecovery(7, false, false, false) != temporary {
				t.Fatal("failure retained playback intent or wrong recovery policy")
			}
			s.Detail = func(id int64) (*netease.Podcast, error) { return &netease.Podcast{ID: id}, nil }
			m.Retry(7, false, false, true, s)
			testutil.Wait(t, posts, "manual detail retry")()
			if d.Podcast == nil || d.Loading || m.NeedsRecovery(7, false, false, true) {
				t.Fatal("detail retry did not clear its failure")
			}
		})
	}
}

// Background queue completion must not cancel a requested podcast when the currently selected track has not changed.
func TestPodcastPendingPlaySurvivesBackgroundQueueAppend(t *testing.T) {
	m, s, posts := podcastFixture()
	current := &netease.Track{ID: 99}
	var revision uint64
	played := 0
	s.CurrentTrack = func() *netease.Track { return current }
	s.QueueRevision = func() uint64 { return revision }
	s.Play = func([]netease.Track, int) { played++ }
	m.RequestPlay(7, s)
	first, second := testutil.Wait(t, posts, "pending metadata"), testutil.Wait(t, posts, "pending episodes")
	revision++
	first()
	second()
	if played != 1 {
		t.Fatal("background queue append cancelled the user's podcast request")
	}
}

// A page of popular podcasts lists only those not listed yet, and the next
// starts past all of it. NetEase's pages overlap: the tab listed podcasts
// twice, two cards under one key sharing their state.
func TestHotPodcastsOverlap(t *testing.T) {
	pods := func(ids ...int64) []netease.Podcast {
		var out []netease.Podcast
		for _, id := range ids {
			out = append(out, netease.Podcast{ID: id})
		}
		return out
	}
	var tab PodcastHome
	tab.AddHot(pods(1, 2, 3))
	tab.AddHot(pods(4, 2, 5))
	var ids []int64
	for _, p := range tab.Hot {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []int64{1, 2, 3, 4, 5}) || tab.HotAt != 6 {
		t.Fatalf("listed %v, next from %d", ids, tab.HotAt)
	}
}
