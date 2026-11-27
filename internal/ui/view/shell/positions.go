/*
保存主窗口导航页的轻量滚动位置，随导航历史淘汰，不持有列表或业务数据。
*/

package shell

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
)

// Position contains only values. List rows include the scrolling header at zero;
// Song and Occurrence identify a song even after rows move or repeat.
type Position struct {
	X, Y       float32
	Row        int
	Song       int64
	Occurrence int
	List       bool
}

// PagePositions belongs to one main window, not settings or the playback
// session. A route remembers its most recent position while still in history.
type PagePositions struct {
	saved   map[navigation.Route]Position
	pending Position
	restore bool
	shown   bool
	list    bool
}

// Save leaves an existing position intact if the page only showed loading or
// an error. In particular, leaving before a restore must not replace it with 0.
func (p *PagePositions) Save(r navigation.Route, scroll ui.ScrollState, rows Position) {
	if !p.shown {
		return
	}
	pos := Position{X: scroll.X, Y: scroll.Y}
	if p.list {
		pos = rows
		pos.X, pos.Y, pos.List = scroll.X, scroll.Y, true
	}
	if p.saved == nil {
		p.saved = make(map[navigation.Route]Position)
	}
	p.saved[r] = pos
}

// Retain releases abandoned forward branches, deleted routes and old history.
func (p *PagePositions) Retain(nav *navigation.Navigator) {
	keep := make(map[navigation.Route]bool, len(nav.History)+len(nav.Forward)+1)
	keep[nav.Current] = true
	for _, routes := range [][]navigation.Route{nav.History, nav.Forward} {
		for _, r := range routes {
			keep[r] = true
		}
	}
	for r := range p.saved {
		if !keep[r] {
			delete(p.saved, r)
		}
	}
}

func (p *PagePositions) Enter(r navigation.Route) {
	p.pending, p.restore = p.saved[r]
	p.shown, p.list = false, false
}

// Take is called only when real content builds, never by a loading skeleton.
// The view applies restoration once; subsequent scrolling belongs to the user.
func (p *PagePositions) Take(list bool) (Position, bool) {
	p.shown, p.list = true, list
	pos, restore := p.pending, p.restore
	p.pending, p.restore = Position{}, false
	return pos, restore && pos.List == list
}

func (s *State) RestoreScroll(ready bool) {
	if !ready {
		return
	}
	if pos, ok := s.Pages.Take(false); ok {
		s.PageScroll.X, s.PageScroll.Y = pos.X, pos.Y
	}
}
