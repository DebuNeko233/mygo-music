//go:build !windows

package app

import (
	"math"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func clearFrame(win *mygo.Window) {}

// dragLyrics moves a Linux layer surface itself. MyGo's native layer drag
// adds the window's previous displacement to the pointer's displacement on
// every motion event, so the window accelerates away from the pointer.
// Global screen coordinates remain in the compositor's coordinate space,
// unlike pointer coordinates local to the moving surface.
func dragLyrics(root ui.Element, win *mygo.Window) bool {
	if runtime.GOOS != "linux" || win == nil {
		return false
	}
	pt := mygo.Screen.CursorScreenPoint()
	dx, dy, held := root.Dragged()
	if !held {
		draggingLyrics = false
		return false
	}
	b := win.Bounds()
	if !draggingLyrics {
		if dx == 0 && dy == 0 {
			return false
		}
		draggingLyrics = true
		dragOrigin = b
		dragPointer = pt
		return true
	}
	b.X = dragOrigin.X + pt.X - dragPointer.X
	b.Y = dragOrigin.Y + pt.Y - dragPointer.Y
	b.X, b.Y = int(math.Round(float64(b.X))), int(math.Round(float64(b.Y)))
	win.SetBounds(b)
	return true
}

var (
	draggingLyrics bool
	dragOrigin     mygo.Rectangle
	dragPointer    mygo.Point
)
