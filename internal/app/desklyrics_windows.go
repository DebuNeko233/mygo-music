//go:build windows

package app

import (
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
)

var (
	clearFrameProc = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	getWindowLong  = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowLongPtrW")
	setWindowLong  = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowLongPtrW")
	setWindowPos   = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")
)

// clearFrame leaves the transparent lyrics window no native decoration: a
// tool window keeps Windows 11's rounded corners and DWM shadow even with
// HasShadow false. A popup has no frame whose alpha could darken the desktop.
// The window stays resizable: WS_THICKFRAME is kept so Windows handles the
// resize, while WM_NCCALCSIZE (in MyGo) hides the frame visually.
func clearFrame(win *mygo.Window) {
	handle := win.NativeHandle()
	win.SetResizable(true)
	const (
		decorations = 0x00C00000 | 0x00080000 | 0x00040000 | 0x00020000 | 0x00010000
		thickFrame  = 0x00040000 // WS_THICKFRAME: keep for native resize
		popup       = 0x80000000
		frameFlags  = 0x0037 // NOSIZE | NOMOVE | NOZORDER | NOACTIVATE | FRAMECHANGED
	)
	style, _, _ := getWindowLong.Call(handle, ^uintptr(15))
	style = style&^(decorations&^thickFrame) | popup | thickFrame
	setWindowLong.Call(handle, ^uintptr(15), style)
	setWindowPos.Call(handle, 0, 0, 0, 0, 0, frameFlags)

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
