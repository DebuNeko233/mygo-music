/*
将临时本地文件接入当前播放流程，保留原队列位置并管理解码器所有权。
*/

package player

import (
	"slices"
	"time"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// PlayTemporary plays a prepared local file after the current track. A file
// already in the queue reuses its entry. It takes ownership of the decoder.
func (p *Player) PlayTemporary(track netease.Track, local *LocalAudio) {
	if p.closed {
		if local != nil {
			local.Close()
		}
		return
	}
	p.finish(MovedOn)
	track.Temporary = true
	at := p.at
	if existing := p.QueuePosition(track.Key()); existing >= 0 {
		if existing != at {
			if p.hasTemporary() {
				p.queue, p.order = p.withoutQueueEntry(at)
				if existing > at {
					existing--
				} else {
					at--
				}
			} else if existing > at {
				at++
			}
			p.moveQueueEntry(existing, at)
		}
	} else if p.hasTemporary() {
		p.queue[p.order[at]] = track
	} else {
		p.temporaryPosition = p.Position()
		p.temporaryAnchor = -1
		index := 0
		if at >= 0 {
			p.temporaryAnchor = p.order[at]
			index = p.temporaryAnchor + 1
		}
		for i, v := range p.order {
			if v >= index {
				p.order[i]++
			}
		}
		at++
		p.queue = slices.Insert(p.queue, index, track)
		p.order = slices.Insert(p.order, at, index)
	}
	p.queueRevision++
	p.step, p.tries = 1, 0
	p.skipDirection = 1
	p.loadTrack(at, 0, true, 0, local, false)
}

func (p *Player) hasTemporary() bool {
	return p.track != nil && p.track.Temporary && p.at >= 0 && p.at < len(p.order) &&
		p.queue[p.order[p.at]].Temporary
}

// withoutQueueEntry removes an entry from copies of the tracks and play order.
func (p *Player) withoutQueueEntry(position int) ([]netease.Track, []int) {
	index := p.order[position]
	queue := slices.Delete(slices.Clone(p.queue), index, index+1)
	order := slices.Delete(slices.Clone(p.order), position, position+1)
	for i, v := range order {
		if v > index {
			order[i]--
		}
	}
	return queue, order
}

func (p *Player) removeTemporary() {
	p.queue, p.order = p.withoutQueueEntry(p.at)
	p.queueRevision++
}

func (p *Player) temporaryCheckpoint() (int, time.Duration) {
	at := slices.Index(p.order, p.temporaryAnchor)
	if at < 0 {
		return max(p.at-1, 0), 0
	}
	if at > p.at {
		at--
	}
	return at, p.temporaryPosition
}

// After a one-off at the end of the queue, leave the previous song paused
// at its old position. With no previous song, clear the player.
func (p *Player) stopTemporary() {
	at, pos := p.temporaryCheckpoint()
	p.removeTemporary()
	if len(p.order) != 0 {
		p.step, p.tries = 0, 0
		p.restorePaused(at, pos)
		return
	}
	p.stop()
	p.track, p.at = nil, -1
	p.playing, p.loading, p.awaiting = false, false, false
	p.err, p.lyricsErr, p.pending = "", "", 0
	p.setLyrics(nil, lyrics.Display{})
	p.sync()
}
