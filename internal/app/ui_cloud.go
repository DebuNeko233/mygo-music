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

// Cloud listings stay in memory and are discarded on account changes.
type cloudData struct {
	tracks                []netease.Track
	count, offset         int
	loaded, loading, more bool
	failure               error
	retryRefresh          bool
	all, retryAll         bool
	paused                bool
	cancel                context.CancelFunc
	request               uint64
	at                    time.Time
	locate                int64
}

func (a *app) loadEntireCloud() {
	d := &a.cloud
	if a.profile == nil || d.loaded && !d.more && d.failure == nil {
		return
	}
	d.all = true
	if !d.loading {
		a.loadCloud(false)
	}
}

// Stop cancels the active HTTP request and rejects a reply already posted.
func (a *app) stopCloudLoad() {
	d := &a.cloud
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.request++
	d.locate = 0
	d.loading, d.all, d.retryAll, d.paused = false, false, false, true
}

func (a *app) refreshCloud() {
	d := &a.cloud
	if d.loading || a.profile == nil {
		return
	}
	// A refresh of a complete library keeps the scope of its search complete.
	d.all = d.loaded && !d.more
	a.loadCloud(true)
}

// A failed page retries at the same offset. Refresh replaces the listing
// only on success, so a temporary network failure never empties the view.
func (a *app) loadCloud(refresh bool) {
	d := &a.cloud
	refresh = refresh || d.failure != nil && d.retryRefresh
	if a.profile == nil || d.loading || (!refresh && d.failure == nil && d.loaded && !d.more) {
		return
	}
	offset := d.offset
	if refresh {
		offset = 0
	}
	d.all = d.all || d.failure != nil && d.retryAll
	d.loading, d.failure, d.retryRefresh, d.paused = true, nil, refresh, false
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
			if d.locate != 0 {
				current := a.pl.Current()
				if a.route.page != pageCloud || a.nowPlaying || current == nil || current.Key() != d.locate {
					d.locate = 0
				}
			}
			d.loading, d.failure, d.cancel = false, err, nil
			if err != nil {
				if d.locate != 0 {
					a.toast = "定位歌曲失败，请稍后重试"
					if errors.Is(err, netease.ErrNotLoggedIn) {
						a.toast = "登录已失效，请重新登录"
					}
				}
				d.retryAll, d.all = d.all, false
				return
			}
			// Append into a new slice: the player may still own the old queue.
			tracks := d.tracks
			if offset == 0 {
				tracks = nil
			}
			merged := make([]netease.Track, 0, len(tracks)+len(page.Tracks))
			merged = append(merged, tracks...)
			seen := make(map[int64]bool, len(merged))
			for _, tr := range merged {
				seen[tr.ID] = true
			}
			for _, tr := range page.Tracks {
				if tr.ID > 0 && !seen[tr.ID] {
					merged = append(merged, tr)
					seen[tr.ID] = true
				}
			}
			d.tracks = merged
			d.loaded, d.count, d.offset, d.more = true, page.Count, page.NextOffset, page.HasMore && page.NextOffset > offset
			d.at = a.listsNow()
			if d.locate != 0 {
				if at := a.currentTrackIndex(d.tracks); at >= 0 {
					d.locate = 0
					a.revealTrack(at)
				} else if !d.more {
					d.locate = 0
					a.toast = "当前网盘中未找到这首歌曲"
				}
			}
			if (d.all || d.locate != 0) && d.more {
				a.loadCloud(false)
			} else {
				d.all, d.retryAll = false, false
			}
		})
	}()
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
	if !d.loaded && !d.loading && !d.paused && d.failure == nil {
		a.loadCloud(false)
	}
	header := func(c *ui.Context) { a.cloudHeader(c) }
	if !d.loaded && d.loading {
		a.trackSkeleton(c, a.pageShimmer(c), true, header)
		return
	}
	at := a.currentTrackIndex(d.tracks)
	a.syncTrackViewport(c, trackHeaderKey{name: "音乐网盘", description: d.summary(), tracks: len(d.tracks)}, at)
	visible := a.trackRows(c, d.tracks, true, header, func(c *ui.Context) {
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
					ui.Button(c.Key("cloud-retry"), "重试加载").OnClick(func() { a.loadCloud(false) })
				}
			})
		case d.paused && (!d.loaded || d.more):
			ui.Column(c).Center().Gap(12).Padding(24, 0, 24, 0).Children(func() {
				ui.Text(c, "加载已停止，已加载的歌曲仍可播放").FontSize(13).TextColor(a.pal.muted)
				ui.Button(c.Key("cloud-resume"), "继续加载").OnClick(func() { a.loadCloud(false) })
			})
		default:
			// Reuse the search/artist list footer: it requests a page near the viewport,
			// while loadCloud keeps repeated builds from starting duplicate requests.
			page := paged{loaded: d.loaded, loading: d.loading, more: d.more}
			a.pageFoot(c, &page, func() { a.loadCloud(false) })
			if d.loaded && !d.loading && len(d.tracks) == 0 {
				a.empty(c, "网盘中还没有歌曲")
			}
		}
	})
	if at >= 0 {
		a.trackLocateButton(c, at, visible)
	} else if current := a.pl.Current(); current != nil && current.Cloud && d.more {
		a.locatePlayingButton(c, false, func() {
			if current := a.pl.Current(); current != nil && current.Cloud {
				d.locate = current.Key()
				a.loadCloud(false)
			}
		})
	}
}

func (d *cloudData) summary() string {
	info := "网易云音乐账号中上传的歌曲"
	if d.loaded {
		info = fmt.Sprintf("%d 首歌曲 · 已加载 %d 首", d.count, len(d.tracks))
	}
	if d.loading && d.all {
		if d.count > 0 {
			info += fmt.Sprintf(" · 正在加载 %d / %d", min(d.offset, d.count), d.count)
		} else {
			info += " · 正在加载全部"
		}
	}
	return info
}

func (a *app) cloudHeader(c *ui.Context) {
	d, p := &a.cloud, a.pal
	ui.Row(c).Padding(0, 12, 14, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "音乐网盘").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
			info := d.summary()
			ui.Text(c, info).FontSize(12).TextColor(p.muted)
		})
		if d.loading {
			ui.Button(c.Key("stop-cloud"), "停止加载").OnClick(a.stopCloudLoad)
		}
	})
}
