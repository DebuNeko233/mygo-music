package viewmodel

import (
	"unicode/utf8"

	"netease-music-mygo/internal/netease"
)

const QuietCommentRunes = 140
const commentPage = 30

// Comments owns one thread; scroll and character transitions remain view resources.
type Comments struct {
	Track                 int64
	Thread                string
	Hot                   []netease.Comment
	QuietHot              []*netease.Comment
	Latest                []netease.Comment
	Total                 int
	More, Loaded, Loading bool
	Error                 string
	generation            uint64
	offset                int
	likes                 map[int64]uint64
}
type CommentServices struct {
	Post   func(func())
	Fetch  func(string, int, int) (*netease.Comments, error)
	Like   func(string, int64, bool) error
	Notify func(string)
}

func (m *Comments) Reset() { *m = Comments{generation: m.generation + 1} }
func (m *Comments) For(tr *netease.Track, s CommentServices) {
	if m.Track == tr.Key() {
		return
	}
	m.Reset()
	m.Track, m.Thread = tr.Key(), tr.Thread()
	m.Load(s)
}
func (m *Comments) Load(s CommentServices) {
	if m.Loading {
		return
	}
	m.Loading, m.Error = true, ""
	thread, generation, offset := m.Thread, m.generation, max(m.offset, len(m.Latest))
	go func() {
		page, err := s.Fetch(thread, offset, commentPage)
		s.Post(func() {
			if m.generation != generation || m.Thread != thread {
				return
			}
			m.Loading = false
			if err != nil {
				m.Error = "评论加载失败"
				return
			}
			m.Loaded = true
			if offset == 0 {
				m.SetHot(page.Hot)
			}
			seen := make(map[int64]bool, len(m.Latest))
			for _, cm := range m.Latest {
				seen[cm.ID] = true
			}
			for _, cm := range page.Latest {
				if !seen[cm.ID] {
					seen[cm.ID] = true
					m.Latest = append(m.Latest, cm)
				}
			}
			m.offset = offset + len(page.Latest)
			m.Total, m.More = page.Total, page.More && len(page.Latest) > 0
		})
	}()
}
func (m *Comments) ToggleLike(id int64, s CommentServices) {
	cm := m.Find(id)
	if cm == nil {
		return
	}
	like := !cm.Liked
	if m.likes == nil {
		m.likes = map[int64]uint64{}
	}
	m.likes[id]++
	version := m.likes[id]
	generation, thread := m.generation, m.Thread
	m.SetLiked(id, like)
	go func() {
		err := s.Like(thread, id, like)
		s.Post(func() {
			if m.generation != generation || m.Thread != thread || m.likes[id] != version {
				return
			}
			if err != nil {
				m.SetLiked(id, !like)
				s.Notify("操作失败，请稍后再试")
			}
		})
	}()
}

// find returns the comment id, hot or latest, or nil.
func (cs *Comments) Find(id int64) *netease.Comment {
	for _, list := range [][]netease.Comment{cs.Hot, cs.Latest} {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
	}
	return nil
}

// setLiked marks the comment id liked, or not, counting the listener in or
// out, both where it is hot and where it is latest.
func (cs *Comments) SetLiked(id int64, like bool) {
	for _, list := range [][]netease.Comment{cs.Hot, cs.Latest} {
		for i := range list {
			if cm := &list[i]; cm.ID == id && cm.Liked != like {
				cm.Liked = like
				if like {
					cm.LikedCount++
				} else {
					cm.LikedCount = max(cm.LikedCount-1, 0)
				}
			}
		}
	}
}

// setHot picks comments short enough to read in the gaps between lyrics.
// Keep pointers to the originals so the card reflects likes made in the list.
func (cs *Comments) SetHot(hot []netease.Comment) {
	clear(cs.QuietHot)
	cs.QuietHot = cs.QuietHot[:0]
	cs.Hot = hot
	for i := range hot {
		if n := utf8.RuneCountInString(hot[i].Content); n > 0 && n <= QuietCommentRunes {
			cs.QuietHot = append(cs.QuietHot, &hot[i])
		}
	}
}
