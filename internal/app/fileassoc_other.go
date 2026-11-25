//go:build !windows

/*
提供非 Windows 平台的文件关联注册占位实现。
*/

package app

func registerFileAssociations() error { return nil }
