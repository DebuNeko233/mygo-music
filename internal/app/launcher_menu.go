/*
由 App 将播放器模式派生为 Windows 任务栏文字，避免进度通知重复提交原生任务。
Linux 不创建此适配器，继续使用安装包内固定的桌面动作。
*/

package app

import (
	"log"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/features/player"
)

type launcherTaskList interface {
	SetTasks([]mygo.JumpListTask) error
}

type launcherModeTitles struct{ shuffle, repeat string }

type launcherTaskMenu struct {
	list   launcherTaskList
	titles launcherModeTitles // last attempted presentation, including failed submissions
}

func (a *app) syncLauncherTasks() {
	if a.launcherMenu == nil || a.pl == nil || a.quitting {
		return
	}
	titles := launcherModeTitles{shuffle: "随机播放：关闭", repeat: "循环：关闭"}
	if a.pl.Shuffle() {
		titles.shuffle = "随机播放：开启"
	}
	switch a.pl.Repeat() {
	case player.RepeatAll:
		titles.repeat = "循环：列表循环"
	case player.RepeatOne:
		titles.repeat = "循环：单曲循环"
	}
	a.launcherMenu.setTitles(titles)
}

func (m *launcherTaskMenu) setTitles(titles launcherModeTitles) {
	if m.titles == titles {
		return
	}
	tasks := make([]mygo.JumpListTask, 0, len(launcherPlaybackTasks))
	for _, task := range launcherPlaybackTasks {
		title := task.title
		switch task.action {
		case "shuffle":
			if titles.shuffle != "" {
				title = titles.shuffle
			}
		case "repeat":
			if titles.repeat != "" {
				title = titles.repeat
			}
		}
		tasks = append(tasks, mygo.JumpListTask{Title: title, Args: []string{playbackActionFlag + task.action}})
	}
	// A failed shell write is logged once for this presentation, rather than
	// retried on each progress notification. The next mode change tries anew.
	m.titles = titles
	if err := m.list.SetTasks(tasks); err != nil {
		log.Printf("taskbar playback tasks: %v", err)
	}
}

func (a *app) removeLauncherTasks() {
	if a.launcherMenu == nil {
		return
	}
	// Explorer keeps tasks after exit. Leave commands, not stale live state.
	a.launcherMenu.setTitles(launcherModeTitles{})
	a.launcherMenu = nil
}
