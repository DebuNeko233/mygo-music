/*
验证窗口内页面位置随历史分支、删除和淘汰释放，加载期间保留待恢复值。
*/

package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/features/navigation"
)

// Position metadata is bounded by live history, including after a new branch
// discards Forward and a playlist is deleted from both navigation directions.
func TestPagePositionLifetime(t *testing.T) {
	var p PagePositions
	var nav navigation.Navigator
	visit := func(r navigation.Route, y float32) {
		p.Take(false)
		p.Save(nav.Current, ui.ScrollState{Y: y}, Position{})
		nav.Open(r)
		p.Retain(&nav)
		p.Enter(nav.Current)
	}
	for i := range 3 * navigation.HistoryLimit {
		visit(navigation.Route{Page: navigation.Playlist, ID: int64(i + 1)}, float32(i+1))
	}
	if len(p.saved) > navigation.HistoryLimit || len(p.saved) < 2 {
		t.Fatalf("position count not bounded by active history: %d", len(p.saved))
	}
	if _, ok := p.saved[navigation.Route{Page: navigation.Playlist, ID: 1}]; ok {
		t.Fatal("evicted history retained a position")
	}
	last := nav.Current
	for range 3 {
		p.Take(false)
		p.Save(nav.Current, ui.ScrollState{Y: 900}, Position{})
		r, _ := nav.Previous()
		nav.Current = r
		p.Enter(r)
	}
	visit(navigation.Route{Page: navigation.Charts}, 1000)
	if _, ok := p.saved[last]; ok {
		t.Fatal("abandoned forward branch retained its position")
	}
	gone := nav.History[len(nav.History)-1]
	nav.Forget(func(r navigation.Route) bool { return r == gone })
	p.Retain(&nav)
	if _, ok := p.saved[gone]; ok {
		t.Fatal("deleted page retained its position")
	}
}

// Leaving a loading/error view preserves a pending position, and real content
// consumes restoration once without retaining UI state inside the snapshot.
func TestPagePositionPendingRestore(t *testing.T) {
	var s State
	r := navigation.Route{Page: navigation.Search, Query: "cached"}
	s.Pages.Take(false)
	s.Pages.Save(r, ui.ScrollState{Y: 1200}, Position{})
	s.Pages.Enter(r)
	s.RestoreScroll(false)
	s.Pages.Save(r, ui.ScrollState{}, Position{})
	s.Pages.Enter(r)
	s.RestoreScroll(true)
	if s.PageScroll.Y != 1200 {
		t.Fatal("loading replaced the remembered offset")
	}
	s.PageScroll.Y = 2400
	s.RestoreScroll(true)
	if s.PageScroll.Y != 2400 {
		t.Fatal("restoration kept overriding user input")
	}
	// Another main window starts independently; nothing is persisted.
	var other State
	other.Pages.Enter(r)
	other.RestoreScroll(true)
	if other.PageScroll.Y != 0 {
		t.Fatal("another window inherited session-only position")
	}
}
