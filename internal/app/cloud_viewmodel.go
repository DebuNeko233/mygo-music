package app

import (
	"context"
	"errors"
	"time"

	"netease-music-mygo/internal/netease"
)

const cloudPageSize = 100

type cloudLoadMode uint8

const (
	cloudNextPage cloudLoadMode = iota
	cloudRefresh
)

// cloudListing is the published metadata snapshot, also used to stage a refresh.
type cloudListing struct {
	tracks        []netease.Track
	count, offset int
	more          bool
	seen          map[int64]struct{}
}

// cloudViewModel owns one account's in-memory listing. All commands and posted
// results run on its owning thread; only fetching runs in the background.
type cloudViewModel struct {
	cloudListing
	loaded, loading bool
	failure         error
	mode            cloudLoadMode
	pending         *cloudListing
	queue           uint64
	request         *cloudRequest
	at              time.Time
	locate          int64
}

type cloudRequest struct{ cancel context.CancelFunc }

// The composition root supplies I/O, publication and the existing player.
// These functions carry no window or UI context into the view model.
type cloudServices struct {
	fetch       func(context.Context, int, int) (*netease.CloudPage, error)
	post        func(func() bool)
	now         func() time.Time
	appendQueue func([]netease.Track, uint64) uint64
	changed     func()
}

func (m *cloudViewModel) stop() {
	if m.request != nil {
		m.request.cancel()
		m.request = nil
	}
	m.locate, m.loading = 0, false
	m.pending, m.queue = nil, 0
}

func (m *cloudViewModel) refresh(s cloudServices) {
	if !m.loading {
		m.load(cloudRefresh, s)
	}
}

// Refresh keeps the old listing visible until the complete replacement arrives.
// A failed page retries its own offset and retains the pending refresh snapshot.
func (m *cloudViewModel) load(mode cloudLoadMode, s cloudServices) {
	if mode == cloudNextPage && m.failure != nil {
		mode = m.mode
	}
	if m.loading {
		return
	}
	listing := &m.cloudListing
	if m.pending != nil {
		listing = m.pending
	}
	if mode != cloudRefresh && m.failure == nil && m.loaded && !listing.more {
		return
	}
	offset := listing.offset
	if mode == cloudRefresh {
		offset, m.queue = 0, 0
		if m.loaded {
			m.pending = &cloudListing{}
		}
	}
	m.loading, m.failure, m.mode = true, nil, mode
	ctx, cancel := context.WithCancel(context.Background())
	run := &cloudRequest{cancel: cancel}
	m.request = run
	go func() {
		defer cancel()
		page, err := s.fetch(ctx, cloudPageSize, offset)
		if err == nil && page == nil {
			err = errors.New("netease: empty cloud response")
		}
		if err == nil && page.HasMore && page.NextOffset <= offset {
			err = errors.New("netease: cloud page did not advance")
		}
		s.post(func() bool {
			if m.request != run {
				return false
			}
			m.finishPage(page, err, offset, s)
			return true
		})
	}()
}

func (m *cloudViewModel) finishPage(page *netease.CloudPage, err error, offset int, s cloudServices) {
	m.loading, m.failure, m.request = false, err, nil
	if err == nil {
		listing := &m.cloudListing
		if m.pending != nil {
			listing = m.pending
		}
		before := len(listing.tracks)
		listing.mergePage(page.Tracks, offset == 0)
		listing.count, listing.offset, listing.more = page.Count, page.NextOffset, page.HasMore && page.NextOffset > offset
		if m.pending == nil {
			m.loaded = true
			if m.queue != 0 {
				m.queue = s.appendQueue(listing.tracks[before:], m.queue)
			}
		} else if !listing.more {
			m.cloudListing, m.pending = *listing, nil
		}
		if !listing.more {
			m.at, m.queue = s.now(), 0
		}
	}
	if s.changed != nil {
		s.changed()
	}
	if err == nil {
		listing := &m.cloudListing
		if m.pending != nil {
			listing = m.pending
		}
		if listing.more {
			m.load(cloudNextPage, s)
		}
	}
}

// The player owns its queue. Initial loading appends only the new entries;
// refresh builds a separate slice instead of changing the published snapshot.
func (d *cloudListing) mergePage(tracks []netease.Track, refresh bool) {
	if refresh {
		d.tracks, d.seen = nil, nil
	}
	if d.seen == nil {
		d.seen = make(map[int64]struct{}, len(d.tracks)+len(tracks))
		for _, tr := range d.tracks {
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
		d.tracks = append(d.tracks, tr)
		d.seen[tr.ID] = struct{}{}
	}
}
