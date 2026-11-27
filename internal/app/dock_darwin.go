/*
注册 macOS 原生 Dock 菜单；窗口隐藏与图标策略变化不改变菜单的应用级生命周期。
*/

package app

import "github.com/egoist/mygo"

func (a *app) installDockMenu() {
	if a.dockMenu != nil || a.quitting {
		return
	}
	a.dockMenu = a.newDockMenu()
	a.syncDockMenu()
	mygo.App.Dock.SetMenu(a.dockMenu)
}

func (a *app) removeDockMenu() {
	if a.dockMenu != nil {
		mygo.App.Dock.SetMenu(nil)
		a.dockMenu = nil
	}
}
