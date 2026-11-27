/*
将固定播放任务注册到 Windows 任务栏 Jump List，复用现有单实例命令入口。
*/

package app

import (
	"log"

	"github.com/egoist/mygo"
)

func (a *app) installLauncherTasks() {
	tasks := make([]mygo.JumpListTask, 0, len(launcherPlaybackTasks))
	for _, task := range launcherPlaybackTasks {
		tasks = append(tasks, mygo.JumpListTask{Title: task.title, Args: []string{playbackActionFlag + task.action}})
	}
	if err := mygo.App.JumpList.SetTasks(tasks); err != nil {
		log.Printf("taskbar playback tasks: %v", err)
	}
}
