/*
在 Windows 加载语音识别 DLL 并查找导出符号。
*/

package speech

import "syscall"

// openLibrary loads the DLL at path. A DLL it imports by name, as the API
// imports onnxruntime.dll, is the one loaded already.
func openLibrary(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}

func librarySymbol(h uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(h), name)
}
