//go:build !windows

/*
提供非 Windows 平台的歌词文档字体选择。
*/

package playback

import (
	"runtime"
)

func lyricDocumentFont(lang string) string {
	switch runtime.GOOS {
	case "darwin":
		switch lang {
		case "en-US":
			return "Avenir Next, Helvetica Neue, system-ui"
		case "ja":
			return "Hiragino Sans, Hiragino Kaku Gothic ProN, system-ui"
		case "zh-Hans":
			return "PingFang SC, Hiragino Sans GB, system-ui"
		case "zh-Hant":
			return "PingFang TC, system-ui"
		case "zh-Hant-HK":
			return "PingFang HK, PingFang TC, system-ui"
		}
	case "linux":
		switch lang {
		case "en-US":
			return "Noto Sans, DejaVu Sans, sans-serif"
		case "ja":
			return "Noto Sans CJK JP, Noto Sans JP, Source Han Sans, sans-serif"
		case "zh-Hans":
			return "Noto Sans CJK SC, Noto Sans SC, Source Han Sans SC, sans-serif"
		case "zh-Hant":
			return "Noto Sans CJK TC, Noto Sans TC, Source Han Sans TC, sans-serif"
		case "zh-Hant-HK":
			return "Noto Sans CJK HK, Noto Sans HK, Noto Sans CJK TC, sans-serif"
		}
	}
	return ""
}
