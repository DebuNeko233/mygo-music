package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

const cloudPageSize = 100

var cloudSongs = netease.CloudSongsContext

type cloudLoadMode uint8

const (
	cloudNextPage cloudLoadMode = iota
	cloudRefresh
)

// cloudListing is a metadata snapshot, also used to stage a refresh.
type cloudListing struct {
	tracks        []netease.Track
	count, offset int
	more          bool
	seen          map[int64]struct{}
}

// Cloud listings stay in memory and are discarded on account changes.
type cloudData struct {
	cloudListing
	loaded, loading bool
	failure         error
	mode            cloudLoadMode
	pending         *cloudListing // keep the published listing until refresh completes
	queue           uint64        // queue revision owned by the initial, incomplete listing
	cancel          context.CancelFunc
	request         uint64
	at              time.Time
	locate          int64
}

// Account changes and shutdown cancel the active request and reject a posted reply.
func (a *app) cancelCloudLoad() {
	d := &a.cloud
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.request++
	d.locate = 0
	d.loading = false
	d.pending, d.queue = nil, 0
}

func (a *app) refreshCloud() {
	d := &a.cloud
	if d.loading || a.profile == nil {
		return
	}
	a.loadCloud(cloudRefresh)
}

// A failed page retries at the same offset. Refresh replaces the listing
// only on success, so a temporary network failure never empties the view.
func (a *app) loadCloud(mode cloudLoadMode) {
	d := &a.cloud
	if mode == cloudNextPage && d.failure != nil {
		mode = d.mode
	}
	if a.quitting || a.profile == nil || d.loading {
		return
	}
	listing := &d.cloudListing
	if d.pending != nil {
		listing = d.pending
	}
	if mode != cloudRefresh && d.failure == nil && d.loaded && !listing.more {
		return
	}
	offset := listing.offset
	if mode == cloudRefresh {
		offset = 0
		d.queue = 0
		if d.loaded {
			d.pending = &cloudListing{}
		}
	}
	d.loading, d.failure, d.mode = true, nil, mode
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.request++
	request := d.request
	uid, gen, fetch := a.listenerID(), a.homeGen, cloudSongs
	go func() {
		defer cancel()
		page, err := fetch(ctx, cloudPageSize, offset)
		if err == nil && page == nil {
			err = errors.New("netease: empty cloud response")
		}
		if err == nil && page.HasMore && page.NextOffset <= offset {
			err = errors.New("netease: cloud page did not advance")
		}
		a.post(func() {
			if a.homeGen != gen || a.listenerID() != uid || d.request != request {
				return
			}
			a.finishCloudPage(page, err, offset)
		})
	}()
}

// Request lifetime/account checks happen before publishing a page here.
func (a *app) finishCloudPage(page *netease.CloudPage, err error, offset int) {
	d := &a.cloud
	d.loading, d.failure, d.cancel = false, err, nil
	if err != nil {
		a.resolveCloudLocate()
		return // retry this offset; the published snapshot stays available
	}
	listing := &d.cloudListing
	if d.pending != nil {
		listing = d.pending
	}
	before := len(listing.tracks)
	listing.mergePage(page.Tracks, offset == 0)
	listing.count, listing.offset, listing.more = page.Count, page.NextOffset, page.HasMore && page.NextOffset > offset
	if d.pending == nil {
		d.loaded = true
		if d.queue != 0 {
			if a.pl.AppendQueue(listing.tracks[before:], d.queue) {
				d.queue = a.pl.QueueRevision()
			} else {
				d.queue = 0 // the listener replaced or edited the queue
			}
		}
	} else if !listing.more {
		d.cloudListing, d.pending = *listing, nil
	}
	if !listing.more {
		d.at = a.listsNow()
		d.queue = 0
	}
	a.resolveCloudLocate()
	if listing.more {
		a.loadCloud(cloudNextPage)
	}
}

// The player copies its queue at PlayQueueFrom. Append only new entries here;
// refresh starts a separate slice, leaving earlier published tracks untouched.
func (d *cloudListing) mergePage(tracks []netease.Track, refresh bool) {
	if refresh {
		d.tracks = nil
		d.seen = nil
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

func (a *app) resolveCloudLocate() {
	d := &a.cloud
	if d.locate == 0 {
		return
	}
	current := a.pl.Current()
	if a.route.page != pageCloud || a.nowPlaying || current == nil || current.Key() != d.locate {
		d.locate = 0
		return
	}
	if d.failure != nil {
		a.toast = "定位歌曲失败，请稍后重试"
		if errors.Is(d.failure, netease.ErrNotLoggedIn) {
			a.toast = "登录已失效，请重新登录"
		}
		return
	}
	if at := a.currentTrackIndex(d.tracks); at >= 0 {
		d.locate = 0
		a.revealTrack(at)
	} else if d.pending == nil && !d.more {
		d.locate = 0
		a.toast = "当前网盘中未找到这首歌曲"
	}
}

func (a *app) cloudPage(c *ui.Context) {
	if a.profile == nil {
		top, bottom := a.insets(c)
		ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
			ui.Text(c, "登录后查看音乐网盘").FontSize(14).TextColor(a.pal.muted)
			ui.PrimaryButton(c.Key("cloud-sign-in"), "登录网易云音乐").OnClick(a.openLogin)
		})
		return
	}
	d := &a.cloud
	if !d.loading && d.failure == nil && (!d.loaded || d.more) {
		a.loadCloud(cloudNextPage)
	}
	header := func(c *ui.Context) { a.cloudHeader(c) }
	if !d.loaded && d.loading {
		a.trackSkeleton(c, a.pageShimmer(c), true, header)
		return
	}
	at := a.currentTrackIndex(d.tracks)
	a.syncTrackViewport(c, trackHeaderKey{name: "音乐网盘", description: d.summary(), tracks: len(d.tracks)}, at)
	var footer []func(*ui.Context)
	if d.failure != nil || d.loaded && !d.loading && len(d.tracks) == 0 {
		footer = append(footer, func(c *ui.Context) {
			switch {
			case d.failure != nil:
				ui.Column(c).Center().Gap(12).Padding(24, 0, 24, 0).Children(func() {
					message := "网盘加载失败，请重试"
					if errors.Is(d.failure, netease.ErrNotLoggedIn) {
						message = "登录已失效，请重新登录"
					}
					ui.Text(c, message).FontSize(13).TextColor(a.pal.muted)
					if errors.Is(d.failure, netease.ErrNotLoggedIn) {
						ui.Button(c.Key("cloud-login"), "重新登录").OnClick(a.openLogin)
					} else {
						ui.Button(c.Key("cloud-retry"), "重试加载").OnClick(func() { a.loadCloud(cloudNextPage) })
					}
				})
			default:
				a.empty(c, "网盘中还没有歌曲")
			}
		})
	}
	visible := a.trackRows(c, d.tracks, true, header, footer...)
	if at >= 0 {
		a.trackLocateButton(c, at, visible)
	} else if current := a.pl.Current(); current != nil && current.Cloud && (d.more || d.pending != nil) {
		a.locatePlayingButton(c, false, func() {
			if current := a.pl.Current(); current != nil && current.Cloud {
				d.locate = current.Key()
				a.loadCloud(cloudNextPage)
			}
		})
	}
}

func (d *cloudData) summary() string {
	if !d.loaded {
		return ""
	}
	return fmt.Sprintf("%d 首歌曲", d.count)
}

func (a *app) cloudHeader(c *ui.Context) {
	p := a.pal
	ui.Row(c).Padding(0, 12, 14, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "音乐网盘").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
			if info := a.cloud.summary(); info != "" {
				ui.Text(c, info).FontSize(12).TextColor(p.muted)
			}
		})
	})
}
