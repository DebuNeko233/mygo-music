package viewmodel

import (
	"time"

	"netease-music-mygo/internal/diskcache"
	"netease-music-mygo/internal/netease"
)

// Home owns the shared home, charts and daily recommendations on one thread.
// Each module publishes independently and retains usable content through a retry.
type Home struct {
	Featured, Charts, Recommended []netease.Playlist
	Daily                         []netease.Track
	Loading                       bool
	Error                         string
	Failures                      map[string]error
	pending                       int
	At                            time.Time
	generation                    uint64
}

// HomeServices captures one account/cache lifetime and an asynchronous main-thread dispatcher.
// Valid rejects results from a retired account; Reset independently retires the VM lifetime.
type HomeServices struct {
	Personal                      bool
	Post                          func(func())
	Cache                         diskcache.Snapshot
	Valid                         func() bool
	Now                           func() time.Time
	Featured, Charts, Recommended func() ([]netease.Playlist, error)
	Daily                         func() ([]netease.Track, error)
}

func (m *Home) Reset() {
	generation := m.generation + 1
	*m = Home{generation: generation}
}
func (m *Home) Load(s HomeServices) { m.load(false, false, s) }
func (m *Home) Retry(manual bool, s HomeServices) {
	if !m.Loading && len(m.Failures) > 0 {
		m.load(true, manual, s)
	}
}
func (m *Home) NeedsRetry(manual bool) bool {
	if m.Loading {
		return false
	}
	for _, err := range m.Failures {
		if manual || netease.RetryableError(err) {
			return true
		}
	}
	return false
}
func (m *Home) Freshen(s HomeServices) {
	if !m.Loading && (m.At.IsZero() || dailyRenewed(m.At, s.Now())) {
		m.Load(s)
	}
}

func (m *Home) load(failedOnly, manual bool, s HomeServices) {
	if m.Loading {
		return
	}
	if !failedOnly {
		m.Failures = map[string]error{}
		// Freshness tracks a full attempt, including partial failures. Focus
		// must not keep reloading healthy modules while failures back off.
		m.At = s.Now()
	}
	m.Error = ""
	m.Loading = true
	m.pending = 0
	run := func(key string) bool {
		if err := m.Failures[key]; failedOnly && (err == nil || !manual && !netease.RetryableError(err)) {
			return false
		}
		m.pending++
		return true
	}
	if run("featured") {
		homeModule(m, s, "featured", s.Featured, func(v []netease.Playlist, cached bool) {
			if !cached || m.Featured == nil {
				m.Featured = v
			}
		})
	}
	if run("charts") {
		homeModule(m, s, "charts", s.Charts, func(v []netease.Playlist, cached bool) {
			if !cached || m.Charts == nil {
				m.Charts = v
			}
		})
	}
	if s.Personal {
		if run("daily") {
			homeModule(m, s, "daily", s.Daily, func(v []netease.Track, cached bool) {
				if !cached || m.Daily == nil {
					m.Daily = v
				}
			})
		}
		if run("recommended") {
			homeModule(m, s, "recommended", s.Recommended, func(v []netease.Playlist, cached bool) {
				if !cached || m.Recommended == nil {
					m.Recommended = v
				}
			})
		}
	}
	if m.pending == 0 {
		m.Loading = false
	}
}

func homeModule[T any](m *Home, s HomeServices, key string, fetch func() (T, error), apply func(T, bool)) {
	req, gen := s.Cache, m.generation
	req.Key = key
	post, valid := s.Post, s.Valid
	go func() {
		var kept T
		if at := req.Read(&kept); !at.IsZero() {
			post(func() {
				if m.generation == gen && (valid == nil || valid()) {
					apply(kept, true)
				}
			})
		}
		value, err := fetch()
		bytes := diskcache.Encode(value)
		post(func() {
			if m.generation != gen || valid != nil && !valid() {
				return
			}
			if err == nil {
				apply(value, false)
				req.Keep(bytes)
				delete(m.Failures, key)
			} else {
				m.Failures[key] = err
			}
			m.pending--
			if m.pending == 0 {
				m.Loading = false
				if len(m.Failures) > 0 && len(m.Featured) == 0 && len(m.Charts) == 0 {
					m.Error = "内容加载失败"
				}
			}
		})
	}()
}

// beijing is NetEase's time, without daylight saving.
var beijing = time.FixedZone("CST", 8*60*60)

// dailyRenewed reports whether NetEase has picked new daily songs between
// loaded and now. It does at 6:00, Beijing time.
func dailyRenewed(loaded, now time.Time) bool {
	b := now.In(beijing)
	six := time.Date(b.Year(), b.Month(), b.Day(), 6, 0, 0, 0, beijing)
	if b.Before(six) {
		six = six.AddDate(0, 0, -1)
	}
	return loaded.Before(six)
}
