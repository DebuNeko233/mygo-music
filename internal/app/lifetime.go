package app

import (
	"netease-music-mygo/internal/desktop/view/components"
)

type appLifetime struct {
	native        components.NativeLifetime
	released      bool
	initialized   bool
	sessionCancel func()
}
