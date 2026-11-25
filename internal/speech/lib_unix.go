//go:build darwin || linux

/*
在 macOS 和 Linux 加载语音识别动态库并查找符号。
*/

package speech

import "github.com/ebitengine/purego"

// openLibrary loads the library at path, its symbols global so that the
// libraries loaded after it link to them.
func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func librarySymbol(h uintptr, name string) (uintptr, error) { return purego.Dlsym(h, name) }
