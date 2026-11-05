//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

var clearFrameProc = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// clearFrame keeps Windows' own border off the transparent window. MyGo
// clears the caption and the frame it extends; Windows 11's border has its
// own colour, the one the framework gives its menus too.
func clearFrame(handle uintptr) {
	const borderColor = 34  // DWMWA_BORDER_COLOR
	const none = 0xFFFFFFFE // DWMWA_COLOR_NONE: no border
	colour := uint32(none)
	clearFrameProc.Call(handle, borderColor, uintptr(unsafe.Pointer(&colour)), unsafe.Sizeof(colour))
}
