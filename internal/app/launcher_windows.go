/*
将播放任务注册到 Windows 任务栏 Jump List，由 App 同步播放器模式文字。
*/

package app

import "github.com/egoist/mygo"

func (a *app) installLauncherTasks() {
	a.launcherMenu = &launcherTaskMenu{list: mygo.App.JumpList}
	a.syncLauncherTasks()
}
