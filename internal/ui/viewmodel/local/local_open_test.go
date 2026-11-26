/*
验证新的打开意图取消旧任务，并释放旧任务准备的解码器。
*/

package local

import (
	"testing"
	"time"

	"netease-music-mygo/internal/features/localcatalog"
	"netease-music-mygo/internal/features/player"
)

// A newer opening intent retires an old prepared decoder before it can take over playback.
func TestLocalOpenCancellationReleasesDecoder(t *testing.T) {
	m := new(LocalLibrary)
	posts := make(chan func(), 1)
	prepared := new(player.LocalAudio)
	released, played := 0, 0
	m.OpenTemporary("song.flac", LocalOpenServices{
		Post: func(fn func()) { posts <- fn }, Stopped: func() bool { return false },
		Prepare: func(string) (localcatalog.Song, *player.LocalAudio) { return localcatalog.Song{}, prepared },
		Release: func(audio *player.LocalAudio) {
			if audio != prepared {
				t.Error("released another decoder")
			}
			released++
		},
		Play: func(localcatalog.Song, localcatalog.Song, *player.LocalAudio) { played++ },
	})
	m.CancelOpen()
	select {
	case fn := <-posts:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("no prepared decoder")
	}
	if released != 1 || played != 0 {
		t.Fatalf("released=%d, played=%d", released, played)
	}
}
