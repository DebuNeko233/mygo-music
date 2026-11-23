package app

import (
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
		Now: a.listsNow,
		AppendQueue: func(tracks []netease.Track, revision uint64) uint64 {
			if a.pl.AppendQueue(tracks, revision) {
				return a.pl.QueueRevision()
			}
			return 0
		},
		Changed: a.resolveCloudLocate,
	}
}

func (a *app) cancelCloudLoad() { a.cloud.Stop() }

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
