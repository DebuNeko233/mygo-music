//go:build !windows

/*
提供非 Windows 平台的桌面歌词窗口边框处理接口。
*/

package playback

import (
	"github.com/egoist/mygo"
)

func clearFrame(win *mygo.Window) {}
