/*
接收任务栏与桌面启动器的播放命令，协调单实例转交和会话恢复后的执行。
*/

package app

import "strings"

const playbackActionFlag = "--playback-action="

// Launchers retain these commands after exit. Windows replaces only the mode
// labels while running; Linux keeps the packaged labels unchanged. Their
// relative order follows the macOS Dock menu.
var launcherPlaybackTasks = []struct{ title, action string }{
	{"切换循环模式", "repeat"},
	{"切换随机播放", "shuffle"},
	{"播放/暂停", "play-pause"},
	{"下一首", "next"},
	{"上一首", "previous"},
}

// Consume only the exact invocation emitted by a launcher; files and music
// links continue through their existing opening paths and ordering rules.
func (a *app) handlePlaybackAction(args []string) bool {
	if len(args) != 1 || !strings.HasPrefix(args[0], playbackActionFlag) {
		return false
	}
	action := strings.TrimPrefix(args[0], playbackActionFlag)
	for _, task := range launcherPlaybackTasks {
		if task.action != action {
			continue
		}
		if !a.quitting {
			if a.startup.waiting || !a.restored || a.pl == nil {
				a.startup.queue(openingIntent{playbackActions: []string{action}})
			} else {
				a.runPlaybackAction(action)
			}
		}
		return true
	}
	return false
}

func (a *app) runPlaybackAction(action string) {
	if a.quitting || a.pl == nil {
		return
	}
	switch action {
	case "play-pause":
		a.pl.TogglePlay()
	case "previous":
		a.pl.Prev()
	case "next":
		a.pl.Next()
	case "shuffle":
		a.pl.ToggleShuffle()
	case "repeat":
		a.pl.CycleRepeat()
	}
	a.redraw()
}
