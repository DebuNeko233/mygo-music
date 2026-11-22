package app

import "netease-music-mygo/internal/netease"

var cloudSongs = netease.CloudSongsContext

func (a *app) cloudServices() cloudServices {
	uid, generation := a.listenerID(), a.homeGen
	return cloudServices{
		fetch: cloudSongs,
		post: func(update func() bool) {
			a.postIfChanged(func() bool {
				if a.homeGen != generation || a.listenerID() != uid {
					return false
				}
				return update()
			})
		},
		now: a.listsNow,
		appendQueue: func(tracks []netease.Track, revision uint64) uint64 {
			if a.pl.AppendQueue(tracks, revision) {
				return a.pl.QueueRevision()
			}
			return 0
		},
		changed: a.resolveCloudLocate,
	}
}

func (a *app) cancelCloudLoad() { a.cloud.stop() }

func (a *app) refreshCloud() {
	if !a.quitting && a.profile != nil {
		a.cloud.refresh(a.cloudServices())
	}
}

func (a *app) loadCloud(mode cloudLoadMode) {
	if !a.quitting && a.profile != nil {
		a.cloud.load(mode, a.cloudServices())
	}
}
