package viewmodel

// Preferences holds in-memory settings navigation. Native widget handles remain in the view.
type Preferences struct{ Section, Query string }

func (m *Preferences) Open(section string) { m.Section, m.Query = section, "" }
