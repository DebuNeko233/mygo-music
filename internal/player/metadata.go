package player

import (
	"errors"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"time"
)

// MetadataPending excludes an in-flight request: recovery never starts a
// second copy, and it never restarts audio or changes pause/seek state.
func (p *Player) MetadataPending() bool {
	return p.track != nil && !p.cacheChanging && (!p.lyricsLoaded && !p.lyricBusy || !p.coverLoaded && !p.coverBusy && p.track.Album.PicURL != "")
}
func (p *Player) RetryMetadata() {
	if p.track == nil || p.cacheChanging {
		return
	}
	t, gen := *p.track, p.gen
	if !p.coverLoaded && !p.coverBusy && t.Album.PicURL != "" {
		p.coverBusy = true
		go func() {
			cover, err := netease.Fetch(netease.Sized(t.Album.PicURL, 600))
			p.post(func() {
				if p.gen != gen {
					return
				}
				p.coverBusy = false
				if err == nil {
					p.coverLoaded = true
					p.announce(&t, cover)
				}
			})
		}()
	}
	if p.lyricsLoaded || p.lyricBusy {
		return
	}
	p.lyricBusy = true
	revision := p.lyricRevision
	words := lyricsOf
	if p.FetchLyrics != nil {
		words = p.FetchLyrics
	}
	go func() {
		var lines []lyrics.Line
		var success bool
		var failure error
		for try := 1; try <= lyricTries; try++ {
			lrc, trans, yrc, err := words(t.ID)
			if err == nil {
				lines = lyrics.Build(lrc, trans, yrc)
				success = true
				break
			}
			failure = err
			if errors.Is(err, netease.ErrOffline) {
				break
			}
			if try < lyricTries {
				time.Sleep(retryGap(try))
			}
		}
		p.post(func() {
			if p.gen != gen {
				return
			}
			p.lyricBusy = false
			if success && p.lyricRevision == revision {
				p.lyrics, p.lyricsLoaded = lines, true
				p.lyricsErr = ""
			} else if !p.lyricsLoaded {
				p.lyricsErr = "歌词加载失败，可刷新重试"
				if errors.Is(failure, netease.ErrOffline) {
					p.lyricsErr = "当前离线，暂无已缓存歌词"
				}
			}
		})
	}()
}

// RefreshLyrics applies background revalidation without moving playback. A
// cached result already in flight cannot overwrite this newer server answer.
func (p *Player) RefreshLyrics(id int64, original, translation, words string) {
	if p.track == nil || p.track.ID != id {
		return
	}
	p.lyricsErr = ""
	p.lyricRevision++
	p.lyrics, p.lyricsLoaded = lyrics.Build(original, translation, words), true
}

func (p *Player) MetadataLoading() bool { return p.lyricBusy || p.coverBusy }

func (p *Player) LyricsError() string { return p.lyricsErr }

// A disconnected retry timer or radio feed is no longer active buffering.
// Downloads cancel through netease.SetOffline; a complete local song and
// already decoded audio keep playing until their own end/failure callback.
func (p *Player) NetworkOffline() {
	if !p.lyricsLoaded {
		p.lyricsErr = "当前离线，暂无已缓存歌词"
	}
	if !p.retrying && !p.awaiting {
		return
	}
	p.gen++
	p.radioGen++
	p.retrying, p.loading, p.awaiting, p.feeding, p.playing = false, false, false, false, false
	p.lyricBusy, p.coverBusy = false, false
	p.err = "当前离线，点击播放可重试"
	p.sync()
}
