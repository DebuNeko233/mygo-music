package player

import (
	"context"
	"errors"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"strings"
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
	p.loadMetadata(manual, true)
}

func (p *Player) loadMetadata(manual, withCover bool) {
	if p.track == nil || p.cacheChanging {
		return
	}
	t, gen := *p.track, p.gen
	if withCover && !p.coverLoaded && !p.coverBusy && t.Album.PicURL != "" && (manual || metadataRecoverable(p.coverFailure)) {
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
	p.cancelLyricRequest()
	ctx, cancel := context.WithCancel(context.Background())
	p.lyricCancel = cancel
	p.lyricBusy = true
	requestID, revision := p.lyricRequestID, p.lyricRevision
	current := func() bool { return p.gen == gen && p.lyricRequestID == requestID }
	refresh := func(lines []lyrics.Line, display lyrics.Display) {
		if current() && p.lyricRevision == revision {
			p.applyRefreshedLyrics(t.ID, lines, display)
		}
	}
	words := lyricsOf
	fetch := p.FetchLyricDisplay
	if p.FetchLyrics != nil {
		words, fetch = p.FetchLyrics, nil
	} else if cached := p.FetchCachedLyrics; fetch == nil && cached != nil {
		words = func(id int64) (string, string, string, error) {
			return cached(id, refresh)
		}
	}
	go func() {
		var display lyrics.Display
		var failure error
		for try := 1; try <= lyricTries; try++ {
			if ctx.Err() != nil {
				return
			}
			if fetch != nil {
				display, failure = fetch(ctx, t, func(d lyrics.Display) {
					p.post(func() { refresh(d.SungLines(), d) })
				})
			} else {
				var lrc, trans, yrc string
				lrc, trans, yrc, failure = words(t.ID)
				if failure == nil {
					_, display = lyrics.BuildDisplay(lrc, trans, yrc)
				}
			}
			if failure == nil || !netease.RetryableError(failure) || errors.Is(failure, netease.ErrOffline) {
				break
			}
			if try < lyricTries {
				select {
				case <-time.After(retryGap(try)):
				case <-ctx.Done():
					return
				}
			}
		}
		p.post(func() {
			if !current() {
				return
			}
			p.lyricBusy = false
			if p.lyricRevision != revision {
				return
			}
			p.lyricFailure = failure
			if failure == nil {
				p.setLyrics(display.SungLines(), display)
				p.lyricsErr = ""
				p.offlineLyricsError = false
			} else if !p.lyricsLoaded {
				p.offlineLyricsError = errors.Is(failure, netease.ErrOffline)
				p.lyricsErr = "歌词加载失败"
				var lyricErr *LyricsUnavailable
				if errors.As(failure, &lyricErr) {
					p.lyricsErr = lyricErr.Message
				}
				if errors.Is(failure, netease.ErrOffline) {
					p.lyricsErr = "暂无缓存歌词"
				}
			}
		})
	}()
}

// LyricsUnavailable is a source-specific error suitable for the playback UI.
type LyricsUnavailable struct{ Message string }

func (err *LyricsUnavailable) Error() string { return err.Message }

func (p *Player) cancelLyricRequest() {
	p.lyricRequestID++
	if p.lyricCancel != nil {
		p.lyricCancel()
		p.lyricCancel = nil
	}
}

// ReloadLyrics switches source without restarting playback or fetching cover art.
func (p *Player) ReloadLyrics() {
	if p.track == nil {
		return
	}
	p.cancelLyricRequest()
	p.lyricBusy, p.lyricsLoaded, p.offlineLyricsError = false, false, false
	p.lyrics, p.lyricDisplay, p.lyricLanguage = nil, lyrics.Display{}, ""
	p.lyricsErr, p.lyricFailure = "", nil
	p.loadMetadata(true, false)
}

// RefreshLyrics applies background revalidation without moving playback. A
// cached result already in flight cannot overwrite this newer server answer.
func (p *Player) RefreshLyrics(id int64, original, translation, words string) {
	if p.track == nil || p.track.ID != id {
		return
	}
	lines, display := lyrics.BuildDisplay(original, translation, words)
	p.applyRefreshedLyrics(id, lines, display)
}

func (p *Player) applyRefreshedLyrics(id int64, lines []lyrics.Line, display lyrics.Display) {
	if p.track == nil || p.track.ID != id {
		return
	}
	p.lyricsErr = ""
	p.offlineLyricsError = false
	p.lyricFailure = nil
	p.lyricRevision++
	p.setLyrics(lines, display)
}

func (p *Player) setLyrics(lines []lyrics.Line, display lyrics.Display) {
	p.lyrics, p.lyricsLoaded, p.lyricLanguage = lines, true, lyrics.Lang(display.Lines)
	p.lyricDisplay = display
	if display.Language == "ja" || strings.HasPrefix(display.Language, "ja-") {
		p.lyricLanguage = "ja"
	}
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
	p.cancelLyricRequest()
	p.gen++
	p.radioGen++
	p.retrying, p.loading, p.awaiting, p.feeding, p.playing = false, false, false, false, false
	p.lyricBusy, p.coverBusy = false, false
	p.err = "播放中断"
	p.offlineAudioError = true
	p.sync()
}
