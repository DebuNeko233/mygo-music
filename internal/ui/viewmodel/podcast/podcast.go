/*
管理播客推荐、详情、节目分页及订阅，协调待播放意图和失败恢复。
*/

package podcast

import (
	"slices"

	pagecache "netease-music-mygo/internal/features/cache/page"
	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/ui/viewmodel"
)

const programsPage = 30

type PodcastPage struct {
	Podcast       *netease.Podcast
	Loading       bool
	Error         string
	Failure       error
	Programs      []netease.Program
	Page          viewmodel.Page
	Subscribing   bool
	thenPlay      bool
	programOffset int
	queueRevision uint64
	queueTrack    *netease.Track
}

type PodcastHome struct {
	Subs, Picks, Hot    []netease.Podcast
	HotPage             viewmodel.Page
	HotAt               int
	Loading             bool
	Error               string
	Failure             error
	SubscriptionsShown  bool
	SubscriptionLoading bool
	SubscriptionFailure error
}

// Podcasts owns home/detail requests and the one subscription list shared with the listener's profile.
type Podcasts struct {
	Pages                pagecache.Cache[int64, *PodcastPage]
	Home                 *PodcastHome
	pendingPlay          *PodcastPage
	subscriptionEdits    int
	subscriptionRevision uint64
	subscriptionsRetry   bool
}

func (m *Podcasts) Reset() {
	m.Pages = pagecache.New[int64, *PodcastPage](func(d *PodcastPage) bool {
		return d != nil && (d.Loading || d.Page.Loading || d.Subscribing || d.thenPlay)
	})
	m.Home = &PodcastHome{}
	m.pendingPlay = nil
	m.subscriptionEdits, m.subscriptionRevision, m.subscriptionsRetry = 0, 0, false
}

// Post must publish on the owner thread and reject retired account sessions.
// Play delegates the prepared queue to the existing Player.
type PodcastServices struct {
	SignedIn      bool
	Post          func(func())
	Detail        func(int64) (*netease.Podcast, error)
	Programs      func(int64, int, int) ([]netease.Program, bool, error)
	Subscribe     func(int64, bool) error
	Subscriptions func() ([]netease.Podcast, error)
	Recommended   func() ([]netease.Podcast, error)
	Hot           func(int, int) ([]netease.Podcast, bool, error)
	QueryLikes    func([]int64)
	Play          func([]netease.Track, int)
	QueueRevision func() uint64
	CurrentTrack  func() *netease.Track
	Notify        func(string)
}

func (m *Podcasts) page(id int64) *PodcastPage {
	d := m.Pages.Get(id)
	if d == nil {
		d = &PodcastPage{}
		m.Pages.Set(id, d)
	}
	return d
}
func (m *Podcasts) NeedsLoad(id int64) bool {
	d := m.Pages.Peek(id)
	return d == nil || d.Podcast == nil && !d.Loading || !d.Page.Loaded && !d.Page.Loading
}
func (m *Podcasts) Load(id int64, s PodcastServices) {
	d := m.page(id)
	if !d.Page.Loaded {
		m.LoadPrograms(id, s)
	}
	if d.Podcast != nil || d.Loading {
		return
	}
	d.Loading, d.Error, d.Failure = true, "", nil
	fetch := s.Detail
	go func() {
		pod, err := fetch(id)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Loading = false
			d.Failure = err
			if err != nil || pod == nil {
				d.Error = "播客加载失败"
				m.cancelPlay(d)
				return
			}
			d.Podcast = pod
			m.playLoaded(d, s)
		})
	}()
}

// LoadPrograms advances by the server's raw page size even when overlapping episodes are hidden.
func (m *Podcasts) LoadPrograms(id int64, s PodcastServices) {
	d := m.page(id)
	if d.Page.Loading {
		return
	}
	d.Page.Loading, d.Page.Error, d.Page.Failure = true, "", nil
	offset := max(d.programOffset, len(d.Programs))
	fetch := s.Programs
	go func() {
		programs, more, err := fetch(id, programsPage, offset)
		s.Post(func() {
			if m.Pages.Peek(id) != d {
				return
			}
			d.Page.Loading = false
			d.Page.Failure = err
			if err != nil {
				d.Page.Error = "节目加载失败"
				m.cancelPlay(d)
				return
			}
			seen := make(map[int64]bool, len(d.Programs)+len(programs))
			for _, pr := range d.Programs {
				seen[pr.ID] = true
			}
			for _, pr := range programs {
				if !seen[pr.ID] {
					seen[pr.ID] = true
					d.Programs = append(d.Programs, pr)
				}
			}
			d.programOffset = offset + len(programs)
			d.Page.Loaded, d.Page.More = true, more && len(programs) > 0
			ids := make([]int64, len(programs))
			for i := range programs {
				ids[i] = programs[i].ID
			}
			s.QueryLikes(ids)
			m.playLoaded(d, s)
		})
	}()
}

func (m *Podcasts) cancelPlay(d *PodcastPage) {
	d.thenPlay = false
	if m.pendingPlay == d {
		m.pendingPlay = nil
	}
}

// RequestPlay waits for metadata and episodes; newer explicit playback always wins.
func (m *Podcasts) RequestPlay(id int64, s PodcastServices) {
	if m.pendingPlay != nil {
		m.cancelPlay(m.pendingPlay)
	}
	m.Load(id, s)
	d := m.Pages.Get(id)
	d.thenPlay, d.queueRevision = true, s.QueueRevision()
	d.queueTrack = s.CurrentTrack()
	m.pendingPlay = d
	m.playLoaded(d, s)
}
func (m *Podcasts) playLoaded(d *PodcastPage, s PodcastServices) {
	if !d.thenPlay || d.Podcast == nil || !d.Page.Loaded {
		return
	}
	revision := d.queueRevision
	current := m.pendingPlay == d
	m.cancelPlay(d)
	// Appending background pages keeps the same current track; a new playback queue does not.
	if current && (revision == s.QueueRevision() || d.queueTrack != nil && d.queueTrack == s.CurrentTrack()) {
		m.PlayProgram(d, 0, s)
	}
}

// PlayProgram only builds the episode queue; decoding and playback stay in the existing Player.
func (m *Podcasts) PlayProgram(d *PodcastPage, i int, s PodcastServices) {
	if m.pendingPlay != nil {
		m.cancelPlay(m.pendingPlay)
	}
	if d.Podcast == nil {
		return
	}
	var queue []netease.Track
	at := 0
	for j := range d.Programs {
		if d.Programs[j].MainSong.ID == 0 {
			continue
		}
		if j <= i {
			at = len(queue)
		}
		queue = append(queue, d.Programs[j].Track(d.Podcast))
	}
	if len(queue) > 0 {
		s.Play(queue, at)
	}
}

func (m *Podcasts) NeedsHomeLoad() bool { return !m.Home.HotPage.Loaded && !m.Home.Loading }
func (m *Podcasts) LoadHome(s PodcastServices) {
	h := m.Home
	if !m.NeedsHomeLoad() {
		return
	}
	h.Loading, h.Error, h.Failure = true, "", nil
	if s.SignedIn {
		m.LoadSubscriptions(s)
	}
	picks, hot := s.Recommended, s.Hot
	go func() {
		recommended, err1 := picks()
		popular, more, err2 := hot(programsPage, 0)
		s.Post(func() {
			if m.Home != h {
				return
			}
			h.Loading = false
			if err1 != nil && err2 != nil {
				h.Error, h.Failure = "无法连接网易云音乐，请检查网络", err2
				return
			}
			h.Picks = recommended
			// A restarted first page replaces the previous raw cursor and cards.
			h.Hot, h.HotAt = nil, 0
			h.AddHot(popular)
			h.HotPage = viewmodel.Page{Loaded: true, More: more && len(popular) > 0}
			if err2 != nil {
				h.HotPage.Error, h.HotPage.Failure = "热门播客加载失败", err2
			}
		})
	}()
}
func (m *Podcasts) LoadHot(s PodcastServices) {
	h := m.Home
	if h.Loading || h.HotPage.Loading {
		return
	}
	h.HotPage.Loading, h.HotPage.Error, h.HotPage.Failure = true, "", nil
	offset, fetch := h.HotAt, s.Hot
	go func() {
		hot, more, err := fetch(programsPage, offset)
		s.Post(func() {
			if m.Home != h {
				return
			}
			h.HotPage.Loading = false
			if err != nil {
				h.HotPage.Error, h.HotPage.Failure = "热门播客加载失败", err
				return
			}
			h.AddHot(hot)
			h.HotPage.More = more && len(hot) > 0
		})
	}()
}
func (h *PodcastHome) AddHot(page []netease.Podcast) {
	h.HotAt += len(page)
	for _, pod := range page {
		if !slices.ContainsFunc(h.Hot, func(p netease.Podcast) bool { return p.ID == pod.ID }) {
			h.Hot = append(h.Hot, pod)
		}
	}
}

func (m *Podcasts) IsSubscribed(pod *netease.Podcast) bool {
	if m.Home.SubscriptionsShown {
		return slices.ContainsFunc(m.Home.Subs, func(p netease.Podcast) bool { return p.ID == pod.ID })
	}
	return pod.Subed
}

// LoadSubscriptions is shared by the podcast home and the listener's own user page.
func (m *Podcasts) LoadSubscriptions(s PodcastServices) {
	if !s.SignedIn || m.Home.SubscriptionLoading {
		return
	}
	if m.subscriptionEdits != 0 {
		m.subscriptionsRetry = true
		return
	}
	h, revision, fetch := m.Home, m.subscriptionRevision, s.Subscriptions
	h.SubscriptionLoading, h.SubscriptionFailure = true, nil
	m.subscriptionsRetry = false
	go func() {
		subs, err := fetch()
		s.Post(func() {
			if m.Home != h {
				return
			}
			h.SubscriptionLoading = false
			if err != nil {
				h.SubscriptionFailure = err
				return
			}
			if m.subscriptionEdits != 0 || m.subscriptionRevision != revision {
				m.subscriptionsRetry = true
				m.retrySubscriptions(s)
				return
			}
			h.Subs, h.SubscriptionsShown = subs, true
		})
	}()
}
func (m *Podcasts) retrySubscriptions(s PodcastServices) {
	if m.subscriptionsRetry && m.subscriptionEdits == 0 && !m.Home.SubscriptionLoading {
		m.LoadSubscriptions(s)
	}
}
func (h *PodcastHome) setSubscribed(pod netease.Podcast, sub bool, at int) int {
	was := slices.IndexFunc(h.Subs, func(p netease.Podcast) bool { return p.ID == pod.ID })
	subs := slices.DeleteFunc(slices.Clone(h.Subs), func(p netease.Podcast) bool { return p.ID == pod.ID })
	if sub {
		subs = slices.Insert(subs, min(at, len(subs)), pod)
	}
	h.Subs = subs
	return was
}
func (m *Podcasts) ToggleSubscribe(d *PodcastPage, s PodcastServices) {
	if d.Podcast == nil || d.Subscribing || m.Pages.Peek(d.Podcast.ID) != d {
		return
	}
	h, pod := m.Home, d.Podcast
	old := *pod
	old.Subed = m.IsSubscribed(pod)
	sub := !old.Subed
	pod.Subed, d.Subscribing = sub, true
	if sub {
		pod.SubCount++
	} else {
		pod.SubCount--
	}
	before := h.Subs
	at := h.setSubscribed(*pod, sub, 0)
	m.subscriptionRevision++
	m.subscriptionEdits++
	id, fetch := pod.ID, s.Subscribe
	go func() {
		err := fetch(id, sub)
		s.Post(func() {
			if m.Home != h || m.Pages.Peek(id) != d {
				return
			}
			d.Subscribing = false
			m.subscriptionRevision++
			m.subscriptionEdits--
			if err != nil {
				pod.Subed, pod.SubCount = old.Subed, old.SubCount
				restoreAt := len(h.Subs)
				if !sub && at >= 0 {
					for _, next := range before[at+1:] {
						if i := slices.IndexFunc(h.Subs, func(p netease.Podcast) bool { return p.ID == next.ID }); i >= 0 {
							restoreAt = i
							break
						}
					}
				}
				h.setSubscribed(old, !sub, restoreAt)
				s.Notify("操作失败，请稍后再试")
			} else if sub {
				s.Notify("已订阅")
			} else {
				s.Notify("已取消订阅")
			}
			if !h.SubscriptionsShown {
				m.subscriptionsRetry = true
			}
			m.retrySubscriptions(s)
		})
	}()
}

func (m *Podcasts) NeedsRecovery(id int64, home, subscriptions, manual bool) bool {
	retry := func(err error) bool { return err != nil && (manual || netease.RetryableError(err)) }
	h := m.Home
	if subscriptions && !h.SubscriptionLoading && retry(h.SubscriptionFailure) {
		return true
	}
	if home && !h.Loading && (retry(h.Failure) || !h.HotPage.Loading && retry(h.HotPage.Failure)) {
		return true
	}
	d := m.Pages.Peek(id)
	return id != 0 && d != nil && (!d.Loading && retry(d.Failure) || !d.Page.Loading && retry(d.Page.Failure))
}
func (m *Podcasts) Retry(id int64, home, subscriptions, manual bool, s PodcastServices) {
	retry := func(err error) bool { return err != nil && (manual || netease.RetryableError(err)) }
	h := m.Home
	if subscriptions && !h.SubscriptionLoading && retry(h.SubscriptionFailure) {
		m.LoadSubscriptions(s)
	}
	if home && !h.Loading {
		if retry(h.Failure) {
			h.HotPage.Loaded = false
			m.LoadHome(s)
		} else if !h.HotPage.Loading && retry(h.HotPage.Failure) {
			m.LoadHot(s)
		}
	}
	if d := m.Pages.Peek(id); id != 0 && d != nil {
		if !d.Loading && retry(d.Failure) {
			m.Load(id, s)
		}
		if !d.Page.Loading && retry(d.Page.Failure) {
			m.LoadPrograms(id, s)
		}
	}
}
func (m *Podcasts) Busy(id int64, home, subscriptions bool) bool {
	if home && (m.Home.Loading || m.Home.HotPage.Loading) || subscriptions && m.Home.SubscriptionLoading {
		return true
	}
	d := m.Pages.Peek(id)
	return id != 0 && d != nil && (d.Loading || d.Page.Loading)
}
