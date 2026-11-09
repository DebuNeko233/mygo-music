package app

import "testing"

// The pages browsed last keep their data and those browsed before them let
// it go, so that a long session does not keep every page it showed; a page
// whose data is still on its way stays, whatever its age, as its reply
// lands where the page still is.
func TestPageCacheKeepsTheLastPages(t *testing.T) {
	c := newPageCache[int64, *albumData](albumBusy)
	for id := int64(-pageLimit); id < 0; id++ {
		c.set(id, &albumData{})
	}
	if len(c.items) != pageLimit {
		t.Fatalf("kept %d pages, want %d", len(c.items), pageLimit)
	}
	// A page opened again is the last one opened, so it outlives the rest.
	if d := c.get(-pageLimit); d == nil {
		t.Fatal("the page opened first was already gone")
	}
	c.set(1, &albumData{})
	if c.peek(-pageLimit+1) != nil {
		t.Error("kept the page opened longest ago over the limit")
	}
	if c.peek(-pageLimit) == nil || c.peek(1) == nil {
		t.Error("dropped a page opened since")
	}
	// A page still loading is not dropped, however many are opened after it.
	loading := &albumData{loading: true}
	c.set(-pageLimit+1, loading)
	for id := int64(100); id < 100+pageLimit*2; id++ {
		c.set(id, &albumData{})
	}
	if c.peek(-pageLimit+1) != loading {
		t.Error("dropped a page whose data was still on its way")
	}
	// Signing out forgets every page.
	c.reset()
	if len(c.items) != 0 || c.peek(1) != nil {
		t.Error("kept pages after signing out")
	}
}
