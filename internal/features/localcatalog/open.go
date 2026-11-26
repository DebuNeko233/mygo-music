/*
准备临时打开的本地音频，返回可用于播放的歌曲信息。
*/

package localcatalog

import (
	"os"

	"netease-music-mygo/internal/features/player"
)

func PrepareTemporary(path string) (Song, *player.LocalAudio) {
	if !audioPath(path) {
		return Song{}, nil
	}
	path, err := filePath(path)
	if err != nil {
		return Song{}, nil
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return Song{}, nil
	}
	audio, err := player.PrepareLocalAudio(path)
	if err != nil {
		return Song{}, nil
	}
	return readSong(path, st, audio.Duration), audio
}
