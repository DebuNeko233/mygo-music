/*
验证导航分支保留规则，以及删除页面后前后历史的回退行为。
*/

package navigation

import "testing"

// The total retained visits stay bounded while Back and Forward move the
// boundary; opening another route retires the old forward branch.
func TestNavigationHistoryBound(t *testing.T) {
	var nav Navigator
	for i := range 3 * HistoryLimit {
		nav.Open(Route{Page: Playlist, ID: int64(i + 1)})
	}
	count := 0
	for {
		r, ok := nav.Previous()
		if !ok {
			break
		}
		nav.Current = r
		count++
	}
	if count != HistoryLimit-1 || nav.Current.ID != 2*HistoryLimit+1 {
		t.Fatalf("wrong retained history boundary: %d visits, current %+v", count, nav.Current)
	}
	for {
		r, ok := nav.Next()
		if !ok {
			break
		}
		nav.Current = r
	}
	if nav.Current.ID != 3*HistoryLimit {
		t.Fatal("forward did not reach the latest page")
	}
}

// Reopening the current page preserves forward history; a genuinely new page clears it.
func TestNavigationBranch(t *testing.T) {
	m := new(Navigator)
	a, b := Route{Page: Charts}, Route{Page: Playlist, ID: 7}
	m.Open(a)
	m.Open(b)
	r, ok := m.Previous()
	if !ok || r != a {
		t.Fatal("lost previous page")
	}
	m.Current = r
	if m.Open(a) || len(m.Forward) != 1 {
		t.Fatal("same page changed history")
	}
	m.Open(Route{Page: Local})
	if _, ok := m.Next(); ok {
		t.Fatal("kept old forward branch")
	}
}

// A deleted playlist is removed from both directions and falls back to the last surviving page.
func TestNavigationForget(t *testing.T) {
	gone := Route{Page: Playlist, ID: 7}
	keep := Route{Page: Album, ID: 8}
	m := Navigator{Current: gone, History: []Route{{Page: Home}, gone, keep, gone}, Forward: []Route{gone, keep}}
	r, changed := m.Forget(func(r Route) bool { return r == gone })
	if !changed || r != keep || len(m.History) != 1 || len(m.Forward) != 1 || m.Forward[0] != keep {
		t.Fatalf("bad fallback: %+v, %+v", r, m)
	}
}
