package viewmodel

import (
	"errors"
	"testing"

	"netease-music-mygo/internal/netease"
)

// Returning to the same song rejects its previous thread request before publishing the fresh one.
func TestCommentsRejectsABAThreadResult(t *testing.T) {
	m := new(Comments)
	posts := make(chan func(), 8)
	s := CommentServices{Post: func(f func()) { posts <- f }, Fetch: func(string, int, int) (*netease.Comments, error) {
		return &netease.Comments{Latest: []netease.Comment{{ID: 1}}}, nil
	}}
	a, b := &netease.Track{ID: 7}, &netease.Track{ID: 8}
	m.For(a, s)
	old := wait(t, posts, "first A")
	m.For(b, s)
	middle := wait(t, posts, "B")
	m.For(a, s)
	fresh := wait(t, posts, "last A")
	old()
	middle()
	if !m.Loading || len(m.Latest) != 0 {
		t.Fatal("old result changed new thread")
	}
	fresh()
	if m.Loading || len(m.Latest) != 1 {
		t.Fatal("new thread did not finish")
	}
}

// Overlapping and within-page duplicates do not repeat rows or reset the server offset to the deduplicated length.
func TestCommentsRawPagingCursor(t *testing.T) {
	m := Comments{Track: 7, Thread: "thread"}
	posts := make(chan func(), 8)
	offsets := make(chan int, 8)
	s := CommentServices{Post: func(f func()) { posts <- f }, Fetch: func(_ string, offset, _ int) (*netease.Comments, error) {
		offsets <- offset
		return &netease.Comments{Latest: []netease.Comment{{ID: 1}, {ID: 1}, {ID: 2}}, More: true}, nil
	}}
	m.Load(s)
	wait(t, posts, "first page")()
	m.Load(s)
	wait(t, posts, "next page")()
	if <-offsets != 0 || <-offsets != 3 || len(m.Latest) != 2 {
		t.Fatal("deduplication changed pagination cursor")
	}
}

// A failed earlier like must not roll back a later toggle, even if the first request finishes last.
func TestCommentsLikeFailureKeepsLatestToggle(t *testing.T) {
	m := Comments{Track: 7, Thread: "thread", Hot: []netease.Comment{{ID: 1, LikedCount: 10}}}
	posts := make(chan func(), 8)
	s := CommentServices{Post: func(f func()) { posts <- f }, Like: func(string, int64, bool) error { return errors.New("offline") }, Notify: func(string) {}}
	m.ToggleLike(1, s)
	first := wait(t, posts, "like")
	m.ToggleLike(1, s)
	last := wait(t, posts, "unlike")
	first()
	if m.Hot[0].Liked || m.Hot[0].LikedCount != 10 {
		t.Fatal("old failure changed latest choice")
	}
	last()
	if !m.Hot[0].Liked || m.Hot[0].LikedCount != 11 {
		t.Fatal("latest failure did not roll back")
	}
}
