package navigation

import "testing"

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
