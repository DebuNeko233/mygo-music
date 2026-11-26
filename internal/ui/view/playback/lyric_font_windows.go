/*
通过 Windows 字体接口选择歌词文档字体，并管理相关原生对象。
*/

package playback

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

var windowsLyricFonts = map[string]func() string{
	"en-US":      windowsLyricFont("en-US", "Arial", ""),
	"ja":         windowsLyricFont("ja", "Yu Gothic", "Noto Sans CJK JP"),
	"zh-Hans":    windowsLyricFont("zh-Hans", "Microsoft YaHei", "Noto Sans CJK SC"),
	"zh-Hant":    windowsLyricFont("zh-Hant", "Microsoft JhengHei", "Noto Sans CJK TC"),
	"zh-Hant-HK": windowsLyricFont("zh-Hant-HK", "Microsoft JhengHei", "Noto Sans CJK HK"),
}

func lyricDocumentFont(lang string) string {
	if load := windowsLyricFonts[lang]; load != nil {
		return load()
	}
	return ""
}

func windowsLyricFont(lang, fallback, alternative string) func() string {
	return sync.OnceValue(func() string {
		families := fallback + ", system-ui"
		if alternative != "" {
			families = fallback + ", " + alternative + ", system-ui"
		}
		// Some recommendations are language components installed on demand.
		// DirectWrite tries the next family if one is unavailable.
		if family := windowsDocumentFont(lang); family != "" && family != fallback {
			families = family + ", " + families
		}
		return families
	})
}

// WinRT interfaces start with the six IInspectable methods.
const (
	fontRelease        = 2
	createFontGroup    = 6  // ILanguageFontGroupFactory.CreateLanguageFontGroup
	modernDocumentFont = 12 // ILanguageFontGroup.ModernDocumentFont
	fontFamilyName     = 6  // ILanguageFont.FontFamily

	roSingleThreaded = 0
	roMultiThreaded  = 1
	rpcChangedMode   = 0x80010106
)

type winRTFontObject struct {
	vtable *[modernDocumentFont + 1]uintptr
}

func (o *winRTFontObject) release() {
	syscall.SyscallN(o.vtable[fontRelease], uintptr(unsafe.Pointer(o)))
}

func windowsDocumentFont(lang string) string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	combase := windows.NewLazySystemDLL("combase.dll")
	initialize := combase.NewProc("RoInitialize")
	hr, _, _ := initialize.Call(roMultiThreaded)
	if uint32(hr) == rpcChangedMode {
		hr, _, _ = initialize.Call(roSingleThreaded)
	}
	if int32(hr) < 0 {
		return ""
	}
	defer combase.NewProc("RoUninitialize").Call()

	createString := combase.NewProc("WindowsCreateString")
	deleteString := combase.NewProc("WindowsDeleteString")
	hstring := func(s string) uintptr {
		words, err := windows.UTF16FromString(s)
		if err != nil {
			return 0
		}
		var value uintptr
		hr, _, _ := createString.Call(uintptr(unsafe.Pointer(&words[0])), uintptr(len(words)-1), uintptr(unsafe.Pointer(&value)))
		if int32(hr) < 0 {
			return 0
		}
		return value
	}
	class := hstring("Windows.Globalization.Fonts.LanguageFontGroup")
	if class == 0 {
		return ""
	}
	defer deleteString.Call(class)
	language := hstring(lang)
	if language == 0 {
		return ""
	}
	defer deleteString.Call(language)

	iid := windows.GUID{Data1: 0xfcaeac67, Data2: 0x4e77, Data3: 0x49c7, Data4: [8]byte{0xb8, 0x56, 0xdd, 0xe9, 0x34, 0xfc, 0x73, 0x5b}}
	var factory *winRTFontObject
	hr, _, _ = combase.NewProc("RoGetActivationFactory").Call(class, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == nil {
		return ""
	}
	defer factory.release()

	var group *winRTFontObject
	hr, _, _ = syscall.SyscallN(factory.vtable[createFontGroup], uintptr(unsafe.Pointer(factory)), language, uintptr(unsafe.Pointer(&group)))
	if int32(hr) < 0 || group == nil {
		return ""
	}
	defer group.release()

	var font *winRTFontObject
	hr, _, _ = syscall.SyscallN(group.vtable[modernDocumentFont], uintptr(unsafe.Pointer(group)), uintptr(unsafe.Pointer(&font)))
	if int32(hr) < 0 || font == nil {
		return ""
	}
	defer font.release()

	var name uintptr
	hr, _, _ = syscall.SyscallN(font.vtable[fontFamilyName], uintptr(unsafe.Pointer(font)), uintptr(unsafe.Pointer(&name)))
	if int32(hr) < 0 || name == 0 {
		return ""
	}
	defer deleteString.Call(name)
	var rawBuffer func(uintptr, *uint32) *uint16
	purego.RegisterFunc(&rawBuffer, combase.NewProc("WindowsGetStringRawBuffer").Addr())
	var length uint32
	buf := rawBuffer(name, &length)
	return windows.UTF16ToString(unsafe.Slice(buf, length))
}
