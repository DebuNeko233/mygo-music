//go:build !darwin

/*
保留非 macOS 平台已有的托盘入口，不注册 Dock 菜单。
*/

package app

func (a *app) installDockMenu() {}
func (a *app) removeDockMenu()  {}
