/*
验证页面缓存保留近期内容，且未完成加载不会挤掉新打开的页面。
*/

package page

import "testing"

// The pages browsed last keep their data and those browsed before them let
// it go, so that a long session does not keep every page it showed; a page
// whose data is still on its way stays, whatever its age, as its reply
// lands where the page still is.
func TestPageCacheKeepsTheLastPages(t *testing.T) {
	c := New[int64, *testPage](testBusy)
	for id := int64(-Limit); id < 0; id++ {
		c.Set(id, &testPage{})
	}
	if len(c.items) != Limit {
		t.Fatalf("kept %d pages, want %d", len(c.items), Limit)
	}
	// A page opened again is the last one opened, so it outlives the rest.
	if d := c.Get(-Limit); d == nil {
		t.Fatal("the page opened first was already gone")
	}
	c.Set(1, &testPage{})
	if c.Peek(-Limit+1) != nil {
		t.Error("kept the page opened longest ago over the limit")
	}
	if c.Peek(-Limit) == nil || c.Peek(1) == nil {
		t.Error("dropped a page opened since")
	}
	// A page still loading is not dropped, however many are opened after it.
	loading := &testPage{loading: true}
	c.Set(-Limit+1, loading)
	for id := int64(100); id < 100+Limit*2; id++ {
		c.Set(id, &testPage{})
	}
	if c.Peek(-Limit+1) != loading {
		t.Error("dropped a page whose data was still on its way")
	}
}

type testPage struct{ loading bool }

func testBusy(p *testPage) bool { return p != nil && p.loading }

// Opening another page while all older pages are loading must not evict the new page before its load starts.
func TestPageCacheKeepsNewPageBesidePendingLoads(t *testing.T) {
	c := New[int, *testPage](testBusy)
	for id := 0; id < Limit; id++ {
		c.Set(id, &testPage{loading: true})
	}
	current := &testPage{}
	c.Set(Limit, current)
	if c.Peek(Limit) != current || len(c.items) != Limit+1 {
		t.Fatal("new page evicted itself before starting its request")
	}
	c.Peek(0).loading = false
	c.Get(Limit)
	if len(c.items) != Limit || c.Peek(0) != nil || c.Peek(Limit) != current {
		t.Fatal("retention did not resume after a pending page completed")
	}
}
