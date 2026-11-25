/*
接入网盘加载与刷新服务，并协调当前播放歌曲在网盘中的定位。
*/

package app

import (
	"errors"

	"netease-music-mygo/internal/netease"
	cloudvm "netease-music-mygo/internal/viewmodel/cloud"
)

var cloudSongs = netease.CloudSongsContext

func (a *app) cloudServices() cloudvm.CloudServices {
	uid, generation := a.listenerID(), a.account.Generation
	return cloudvm.CloudServices{
		Fetch: cloudSongs,
		Post: func(update func() bool) {
			a.postIfChanged(func() bool {
				if a.account.Generation != generation || a.listenerID() != uid {
					return false
				}
				return update()
			})
		},
		Now: a.library.Now,
		AppendQueue: func(tracks []netease.Track, revision uint64) uint64 {
			if a.pl.AppendQueue(tracks, revision) {
				return a.pl.QueueRevision()
			}
			return 0
		},
		Changed: a.resolveCloudLocate,
	}
}

func (a *app) refreshCloud() {
	if !a.quitting && a.account.Profile != nil {
		a.cloud.Refresh(a.cloudServices())
	}
}

func (a *app) loadCloud(mode cloudvm.CloudLoadMode) {
	if !a.quitting && a.account.Profile != nil {
		a.cloud.Load(mode, a.cloudServices())
	}
}

func (a *app) resolveCloudLocate() {
	d := &a.cloud
	if d.Locate == 0 {
		return
	}
	current := a.pl.Current()
	if a.nav.Current.Page != pageCloud || a.playbackState.Open || current == nil || current.Key() != d.Locate {
		d.Locate = 0
		return
	}
	if d.Failure != nil {
		a.toast = "定位歌曲失败，请稍后重试"
		if errors.Is(d.Failure, netease.ErrNotLoggedIn) {
			a.toast = "登录已失效，请重新登录"
		}
		return
	}
	if at := a.views.tracks.CurrentIndex(d.Tracks); at >= 0 {
		d.Locate = 0
		a.views.tracks.Reveal(at)
	} else if !d.Refreshing() && !d.More {
		d.Locate = 0
		a.toast = "当前网盘中未找到这首歌曲"
	}
}
