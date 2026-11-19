package app

import (
	"math"
	"os"
	"slices"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/player"
)

func (a *app) chooseTemporaryAudio() {
	a.showMain()
	paths, err := openLocalDialog(mygo.OpenDialogOptions{
		Parent: a.win, Title: "打开音频文件",
		Filters: []mygo.FileFilter{{Name: "本地音频文件", Extensions: localExtensions}},
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
		a.local.pendingFile = path
		return
	}
	a.showMain()
	a.cancelMusicLink()
	a.local.openRequest++
	request := a.local.openRequest
	go func() {
		song, audio := prepareTemporaryAudio(path)
		a.post(func() {
			if a.quitting || a.local.openRequest != request {
				if audio != nil {
					audio.Close()
				}
				return
			}
			if audio == nil {
				a.toast = "无法播放所选音频，请选择支持的音频文件"
				return
			}
			if i := slices.IndexFunc(a.local.catalog.Songs, func(known localSong) bool { return known.Path == song.Path }); i >= 0 {
				song.Track.LocalID = a.local.catalog.Songs[i].Track.LocalID
			} else {
				// Library IDs ascend from 1. Temporary IDs descend from MaxInt64.
				if a.local.temporaryID == 0 {
					a.local.temporaryID = math.MaxInt64
				}
				song.Track.LocalID = a.local.temporaryID
				a.local.temporaryID--
			}
			delete(a.local.artwork.known, a.local.temporary.artworkKey(true))
			a.local.temporary = song
			a.local.temporaryLyrics = keptLyrics{}
			a.pl.PlayTemporary(song.Track, audio)
			a.nowPlaying = true
		})
	}()
}

func prepareTemporaryAudio(path string) (localSong, *player.LocalAudio) {
	if !localAudioPath(path) {
		return localSong{}, nil
	}
	path, err := localFilePath(path)
	if err != nil {
		return localSong{}, nil
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return localSong{}, nil
	}
	audio, err := player.PrepareLocalAudio(path)
	if err != nil {
		return localSong{}, nil
	}
	return readLocalSong(path, st, audio.Duration), audio
}
