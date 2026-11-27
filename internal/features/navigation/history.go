/*
维护页面导航历史，处理打开、前进、后退和已删除页面清理。
*/

package navigation

import "slices"

// Navigator owns page history. The host displays routes returned by Previous,
// Next and Forget, so page-specific cleanup can run before Current changes.
type Navigator struct {
	Current          Route
	History, Forward []Route
}

// HistoryLimit bounds a window's back/current/forward visits together. Page
// position metadata can therefore share the history lifetime without growing.
const HistoryLimit = 100

func (m *Navigator) Open(r Route) bool {
	if r == m.Current {
		return false
	}
	m.History = append(m.History, m.Current)
	if len(m.History) >= HistoryLimit {
		m.History = slices.Delete(m.History, 0, len(m.History)-HistoryLimit+1)
	}
	m.Forward = nil
	m.Current = r
	return true
}
func (m *Navigator) Previous() (Route, bool) {
	if len(m.History) == 0 {
		return Route{}, false
	}
	m.Forward = append(m.Forward, m.Current)
	r := m.History[len(m.History)-1]
	m.History[len(m.History)-1] = Route{}
	m.History = m.History[:len(m.History)-1]
	return r, true
}
func (m *Navigator) Next() (Route, bool) {
	if len(m.Forward) == 0 {
		return Route{}, false
	}
	m.History = append(m.History, m.Current)
	r := m.Forward[len(m.Forward)-1]
	m.Forward[len(m.Forward)-1] = Route{}
	m.Forward = m.Forward[:len(m.Forward)-1]
	return r, true
}
func (m *Navigator) Forget(gone func(Route) bool) (Route, bool) {
	m.History = slices.DeleteFunc(m.History, gone)
	m.Forward = slices.DeleteFunc(m.Forward, gone)
	if !gone(m.Current) {
		return Route{}, false
	}
	r := Route{Page: Home}
	if n := len(m.History); n > 0 {
		r = m.History[n-1]
		m.History[n-1] = Route{}
		m.History = m.History[:n-1]
	}
	return r, true
}
