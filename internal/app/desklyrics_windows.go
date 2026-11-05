//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

var clearFrameProc = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// clearFrame keeps Windows' own border and shadow off the transparent window.
// Changing a window to or from a layered one may make DWM put them back, so
// the non-client rendering policy goes with the border, not just at creation.
func clearFrame(handle uintptr) {
	const (
		nonClientRendering = 2          // DWMWA_NCRENDERING_POLICY
		nonClientDisabled  = 1          // DWMNCRP_DISABLED
		borderColor        = 34         // DWMWA_BORDER_COLOR
		none               = 0xFFFFFFFE // DWMWA_COLOR_NONE: no border
	)
	policy, colour := uint32(nonClientDisabled), uint32(none)
	clearFrameProc.Call(handle, nonClientRendering, uintptr(unsafe.Pointer(&policy)), unsafe.Sizeof(policy))
	clearFrameProc.Call(handle, borderColor, uintptr(unsafe.Pointer(&colour)), unsafe.Sizeof(colour))
}
