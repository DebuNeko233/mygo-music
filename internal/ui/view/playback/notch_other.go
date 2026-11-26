//go:build !darwin

/*
为非 macOS 平台提供刘海播放器几何和原生窗口能力的回退实现。
*/

package playback

func notchSize() (w, h float64, ok bool) { return 0, 0, false }

func raiseAboveMenuBar(handle uintptr) {}

func pinToTop(handle uintptr, w, h float64) {}

func WindowFrame(handle uintptr) (x, y, w, h float64) { return }

func menuBarHeight() float64 { return 0 }

func watchPointer(fn func()) (stop func(), ok bool) { return func() {}, false }
