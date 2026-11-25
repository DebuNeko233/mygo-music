/*
保存设置页的内存导航状态，不持有原生控件句柄。
*/

package settings

// Preferences holds in-memory settings navigation. Native widget handles remain in the view.
type Preferences struct{ Section, Query string }

func (m *Preferences) Open(section string) { m.Section, m.Query = section, "" }
