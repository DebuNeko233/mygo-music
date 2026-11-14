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
	return p.track != nil && !p.cacheChanging && (!p.lyricsLoaded && !p.lyricBusy && metadataRecoverable(p.lyricFailure) || !p.coverLoaded && !p.coverBusy && p.track.Album.PicURL != "" && metadataRecoverable(p.coverFailure))
}
func metadataRecoverable(err error) bool { return err == nil || netease.RetryableError(err) }

// RetryMetadata is also the explicit lyric refresh action; RecoverMetadata
// is used by background recovery and leaves permanent failures alone.
func (p *Player) RetryMetadata()   { p.retryMetadata(true) }
func (p *Player) RecoverMetadata() { p.retryMetadata(false) }
func (p *Player) retryMetadata(manual bool) {
	if p.track == nil || p.cacheChanging {
		return
	}
	t, gen := *p.track, p.gen
	if !p.coverLoaded && !p.coverBusy && t.Album.PicURL != "" && (manual || metadataRecoverable(p.coverFailure)) {
		p.coverBusy = true
		go func() {
			cover, err := netease.Fetch(netease.Sized(t.Album.PicURL, 600))
			p.post(func() {
				if p.gen != gen {
					return
				}
				p.coverBusy = false
				p.coverFailure = err
				if err == nil {
					p.coverLoaded = true
					p.announce(&t, cover)
				}
			})
		}()
	}
	if p.lyricsLoaded || p.lyricBusy || !manual && !metadataRecoverable(p.lyricFailure) {
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
		var display lyrics.Display
		var success bool
		var failure error
		for try := 1; try <= lyricTries; try++ {
			lrc, trans, yrc, err := words(t.ID)
			if err == nil {
				lines, display = lyrics.BuildDisplay(lrc, trans, yrc)
				success = true
				break
			}
			failure = err
			if !netease.RetryableError(err) || errors.Is(err, netease.ErrOffline) {
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
			p.lyricFailure = failure
			if success && p.lyricRevision == revision {
				p.setLyrics(lines, display)
				p.lyricsErr = ""
				p.offlineLyricsError = false
			} else if !p.lyricsLoaded {
				p.offlineLyricsError = errors.Is(failure, netease.ErrOffline)
				p.lyricsErr = "歌词加载失败"
				if errors.Is(failure, netease.ErrOffline) {
					p.lyricsErr = "暂无缓存歌词"
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
	p.offlineLyricsError = false
	p.lyricFailure = nil
	p.lyricRevision++
	p.setLyrics(lyrics.BuildDisplay(original, translation, words))
}

func (p *Player) setLyrics(lines []lyrics.Line, display lyrics.Display) {
	p.lyrics, p.lyricsLoaded, p.lyricLanguage = lines, true, lyrics.Lang(lines)
	p.lyricDisplay = display
}

func (p *Player) MetadataLoading() bool { return p.lyricBusy || p.coverBusy }

func (p *Player) LyricsError() string {
	if p.offlineLyricsError && !netease.Offline() {
		if p.lyricBusy {
			return "加载中…"
		}
		return "歌词加载失败"
	}
	return p.lyricsErr
}

// LyricsRetryable hides the action while a request is running or the network
// is offline. Retrying metadata leaves audio and its playback position alone.
func (p *Player) LyricsRetryable() bool {
	return p.track != nil && !p.cacheChanging && !netease.Offline() && !p.lyricsLoaded && !p.lyricBusy && p.lyricsErr != ""
}

// A disconnected retry timer or radio feed is no longer active buffering.
// Downloads cancel through netease.SetOffline; a complete local song and
// already decoded audio keep playing until their own end/failure callback.
func (p *Player) NetworkOffline() {
	if !p.lyricsLoaded {
		p.offlineLyricsError = true
		p.lyricsErr = "暂无缓存歌词"
	}
	if !p.retrying && !p.awaiting {
		return
	}
	p.gen++
	p.radioGen++
	p.retrying, p.loading, p.awaiting, p.feeding, p.playing = false, false, false, false, false
	p.lyricBusy, p.coverBusy = false, false
	p.err = "播放中断"
	p.offlineAudioError = true
	p.sync()
}
