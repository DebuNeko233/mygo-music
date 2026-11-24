package app

import (
	"github.com/egoist/mygo/ui"
)

func (s *settings) uiTheme(base *ui.Theme) ui.Theme {
	theme := *base
	if s.UIFont != "" {
		theme.Font = s.UIFont
	}
	return theme
}
