/*
管理搜索页面、分页请求和历史记录，隔离已替换的查询结果。
*/

package search

import (
	"netease-music-mygo/internal/navigation"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/pagecache"
	"netease-music-mygo/internal/viewmodel"
)

type SearchPage struct {
	Res  netease.SearchResult
	Page viewmodel.Page
}
type Search struct {
	Pages       pagecache.Cache[navigation.Route, *SearchPage]
	Query       string
	Last        navigation.Route
	Suggestions Suggestions
}
type SearchServices struct {
	Post    func(func())
	Fetch   func(string, netease.SearchType, int, int) (netease.SearchResult, error)
	Suggest func(string) (netease.Suggestions, error)
}

func (m *Search) Reset() {
	m.DismissSuggestions()
	m.Pages = pagecache.New[navigation.Route, *SearchPage](func(d *SearchPage) bool { return d != nil && d.Page.Loading })
}
func (m *Search) Activate(r navigation.Route) { m.Query, m.Last = r.Query, r }
func (m *Search) NeedsLoad(r navigation.Route, again bool) bool {
	return again || m.Pages.Peek(r) == nil
}
func (m *Search) Load(r navigation.Route, again bool, s SearchServices) {
	if m.Pages.Get(r) != nil && !again {
		return
	}
	d := &SearchPage{}
	m.Pages.Set(r, d)
	m.LoadMore(r, d, s)
}
func (m *Search) LoadMore(r navigation.Route, d *SearchPage, s SearchServices) {
	if m.Pages.Peek(r) != d || d.Page.Loading {
		return
	}
	d.Page.Loading, d.Page.Error = true, ""
	offset, fetch := d.Res.Len(), s.Fetch
	go func() {
		res, err := fetch(r.Query, r.Kind, 60, offset)
		s.Post(func() {
			if m.Pages.Peek(r) != d {
				return
			}
			d.Page.Loading = false
			d.Page.Failure = err
			if err != nil {
				d.Page.Error = "搜索失败，请稍后再试"
				return
			}
			d.Res.Append(res)
			d.Page.Loaded, d.Page.More = true, res.Len() > 0 && d.Res.Len() < d.Res.Total
		})
	}()
}

// RememberQuery keeps the latest twenty queries, putting this one first once.
func RememberQuery(previous []string, query string) []string {
	history := []string{query}
	for _, s := range previous {
		if s != query && len(history) < 20 {
			history = append(history, s)
		}
	}
	return history
}
