//go:build !windows

/*
非 Windows 平台的启动器由系统菜单或随安装包生成的桌面入口提供。
*/

package app

func (a *app) installLauncherTasks() {}
