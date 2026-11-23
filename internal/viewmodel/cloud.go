package viewmodel

import (
	"context"
	"errors"
	"time"

	"netease-music-mygo/internal/netease"
)

const cloudPageSize = 100

// CloudLoadMode distinguishes continuing a scan from replacing its listing.
type CloudLoadMode uint8

const (
	CloudNextPage CloudLoadMode = iota
	CloudRefresh
)

// CloudListing is the published metadata snapshot, also used to stage a refresh.
type CloudListing struct {
	Tracks        []netease.Track
	Count, Offset int
	More          bool
	seen          map[int64]struct{}
}

// Cloud owns one account's in-memory listing. All commands and posted
// results run on its owning thread; only fetching runs in the background.
type Cloud struct {
	CloudListing
	Loaded  bool
	Failure error
	mode    CloudLoadMode
	pending *CloudListing
	queue   uint64
	request *cloudRequest
	At      time.Time
	Locate  int64
}

type cloudRequest struct{ cancel context.CancelFunc }

func (m *Cloud) Loading() bool { return m.request != nil }

// CloudServices supplies I/O, publication and the existing player.
// Post runs updates asynchronously on the owning thread. It may discard
// callbacks after the account lifetime ends. Changed observes each published page.
type CloudServices struct {
	Fetch       func(context.Context, int, int) (*netease.CloudPage, error)
	Post        func(func() bool)
	Now         func() time.Time
	AppendQueue func([]netease.Track, uint64) uint64
	Changed     func()
}

// Stop retires the execution and pending refresh, retaining the published listing.
func (m *Cloud) Stop() {
	if m.request != nil {
		m.request.cancel()
		m.request = nil
	}
	m.Locate = 0
	m.pending, m.queue = nil, 0
}

func (m *Cloud) Refresh(s CloudServices) {
	m.Load(CloudRefresh, s)
}

// Load keeps the old listing visible until the complete replacement arrives.
// A failed page retries its own offset and retains the pending refresh snapshot.
func (m *Cloud) Load(mode CloudLoadMode, s CloudServices) {
	if mode == CloudNextPage && m.Failure != nil {
		mode = m.mode
	}
	if m.Loading() {
		return
	}
	listing := &m.CloudListing
	if m.pending != nil {
		listing = m.pending
	}
	if mode != CloudRefresh && m.Failure == nil && m.Loaded && !listing.More {
		return
	}
	offset := listing.Offset
	if mode == CloudRefresh {
		offset, m.queue = 0, 0
		if m.Loaded {
			m.pending = &CloudListing{}
		}
	}
	m.Failure, m.mode = nil, mode
	ctx, cancel := context.WithCancel(context.Background())
	run := &cloudRequest{cancel: cancel}
	m.request = run
	go func() {
		defer cancel()
		page, err := s.Fetch(ctx, cloudPageSize, offset)
		if err == nil && page == nil {
			err = errors.New("netease: empty cloud response")
		}
		if err == nil && page.HasMore && page.NextOffset <= offset {
			err = errors.New("netease: cloud page did not advance")
		}
		s.Post(func() bool {
			if m.request != run {
				return false
			}
			m.finishPage(page, err, offset, s)
			return true
		})
	}()
}

func (m *Cloud) finishPage(page *netease.CloudPage, err error, offset int, s CloudServices) {
	m.Failure, m.request = err, nil
	more := false
	if err == nil {
		listing := &m.CloudListing
		if m.pending != nil {
			listing = m.pending
		}
		before := len(listing.Tracks)
		listing.mergePage(page.Tracks, offset == 0)
		listing.Count, listing.Offset, listing.More = page.Count, page.NextOffset, page.HasMore && page.NextOffset > offset
		more = listing.More
		if m.pending == nil {
			m.Loaded = true
			if m.queue != 0 {
				m.queue = s.AppendQueue(listing.Tracks[before:], m.queue)
			}
		} else if !listing.More {
			m.CloudListing, m.pending = *listing, nil
		}
		if !listing.More {
			m.At, m.queue = s.Now(), 0
		}
	}
	if s.Changed != nil {
		s.Changed()
	}
	if more {
		m.Load(CloudNextPage, s)
	}
}

// The player owns its queue. Initial loading appends only the new entries;
// refresh builds a separate slice instead of changing the published snapshot.
func (d *CloudListing) mergePage(tracks []netease.Track, refresh bool) {
	if refresh {
		d.Tracks, d.seen = nil, nil
	}
	if d.seen == nil {
		d.seen = make(map[int64]struct{}, len(d.Tracks)+len(tracks))
		for _, tr := range d.Tracks {
			d.seen[tr.ID] = struct{}{}
		}
	}
	for _, tr := range tracks {
		if tr.ID <= 0 {
			continue
		}
		if _, exists := d.seen[tr.ID]; exists {
			continue
		}
		d.Tracks = append(d.Tracks, tr)
		d.seen[tr.ID] = struct{}{}
	}
}

// Refreshing reports whether a replacement listing is being staged while the
// published listing remains available for searching and playback.
func (m *Cloud) Refreshing() bool { return m.pending != nil }

// FollowQueue attaches an initial scan to the existing player's queue revision.
// A refresh never extends the queue that was built from the old snapshot.
func (m *Cloud) FollowQueue(revision uint64) {
	m.queue = 0
	if m.More && m.pending == nil {
		m.queue = revision
	}
}
