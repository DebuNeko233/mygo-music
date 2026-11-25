package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/desktop/view/components"
	"netease-music-mygo/internal/netease"
	commentsvm "netease-music-mygo/internal/viewmodel/comments"
)

// These load a page of a thread's comments, and like a comment or unlike
// it, with NetEase; tests replace them.
var (
	threadComments = netease.ThreadComments
	likeComment    = netease.LikeComment
)

func (a *app) commentServices() commentsvm.CommentServices {
	return commentsvm.CommentServices{Post: a.detailPost(), Fetch: threadComments, Like: likeComment, Notify: func(message string) { a.toast = message }}
}

func (a *app) resetComments() { a.comments.Reset(); a.views.comments.Scroll = ui.ScrollState{} }

func (a *app) commentsFor(tr *netease.Track) *commentsvm.Comments {
	if a.comments.Track != tr.Key() {
		a.views.comments.Scroll = ui.ScrollState{}
		a.comments.For(tr, a.commentServices())
	}
	return &a.comments
}

func (a *app) loadComments() {
	if !a.comments.Loading {
		a.comments.Load(a.commentServices())
	}
}

// commentsTab names the comments' pane, with how many there are.
func (a *app) commentsTab() string {
	if n := a.commentCount(); n > 0 {
		return "评论 " + components.Count(n)
	}
	return "评论"
}

// commentCount is the count the comments' tab shows: the last song's
// while the next one's loads, so that it rolls from one to the other.
func (a *app) commentCount() int {
	switch cs := &a.comments; {
	case cs.Loaded:
		return cs.Total
	case cs.Error != "":
		return 0
	}
	return a.TabCount.Value()
}

// toggleCommentLike likes a comment, or unlikes it, at once in the app and
// then with NetEase, which may refuse: it goes back then. Signed out, it
// asks the listener to sign in.
func (a *app) toggleCommentLike(id int64) {
	if a.account.Profile == nil {
		a.openLogin()
		return
	}
	a.comments.ToggleLike(id, a.commentServices())
}
