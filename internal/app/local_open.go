package app

import (
	"github.com/egoist/mygo"

	"netease-music-mygo/internal/localcatalog"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) chooseTemporaryAudio() {
	a.showMain()
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.win, Title: "打开音频文件",
		Filters: []mygo.FileFilter{{
			Name: "本地音频文件", Extensions: localExtensions,
			MIMETypes: []string{"audio/aac", "audio/aacp", "audio/3gpp", "audio/3gpp2", "audio/mp4", "audio/mp4a-latm", "audio/mpeg4-generic"},
		}},
	})
	if err != nil {
		a.toast = "无法打开文件选择窗口"
	} else if len(paths) > 0 {
		a.openTemporaryAudio(paths[0])
	}
}

// openTemporaryAudio is shared by the File menu and system open-file events.
func (a *app) openTemporaryAudio(path string) {
	if a.quitting {
		return
	}
	if !a.restored {
		a.localView.pendingFile = path
		return
	}
	a.showMain()
	a.cancelMusicLink()
	a.local.OpenTemporary(path, viewmodel.LocalOpenServices{
		Post: a.post, Stopped: func() bool { return a.quitting }, Prepare: localcatalog.PrepareTemporary,
		Release: func(audio *player.LocalAudio) { audio.Close() },
		Play: func(previous, song localcatalog.Song, audio *player.LocalAudio) {
			delete(a.trackArt.known, localArtworkKey(previous, true))
			a.pl.PlayTemporary(song.Track, audio)
			a.nowPlaying = true
		},
		Notify: func(message string) { a.toast = message },
	})
}
