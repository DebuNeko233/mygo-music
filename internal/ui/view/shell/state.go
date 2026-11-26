/*
定义主窗口外壳的控件与页面状态，由应用提供业务数据和功能视图。
*/

// Package shell composes the main window and owns its navigation and page widgets.
// The application supplies feature views and actions; shell never loads data or owns playback.
package shell

import (
	"github.com/egoist/mygo/ui"
)

// State belongs to one main window and survives pages being covered by Now Playing.
type State struct {
	PageScroll, NavScroll                          ui.ScrollState
	SearchBox                                      ui.Handle
	MineCollapsed, SavedCollapsed, AlbumsCollapsed bool
	UnderTop, UnderBottom                          bool
}
