//go:build !nyago_debug

/*
为普通构建提供可内联的空调试入口，不链接截图巡览和性能采样代码。
*/

package app

func (*app) runDebugShots() {}
