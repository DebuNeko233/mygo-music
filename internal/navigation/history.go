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

func (m *Navigator) Open(r Route) bool {
	if r == m.Current {
		return false
	}
	m.History = append(m.History, m.Current)
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
	m.History = m.History[:len(m.History)-1]
	return r, true
}
func (m *Navigator) Next() (Route, bool) {
	if len(m.Forward) == 0 {
		return Route{}, false
	}
	m.History = append(m.History, m.Current)
	r := m.Forward[len(m.Forward)-1]
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
		m.History = m.History[:n-1]
	}
	return r, true
}
