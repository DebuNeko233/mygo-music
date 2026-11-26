//go:build nyago_debug && !windows

/*
读取非 Windows 平台的进程累计 CPU 时间，供性能观测使用。
*/

package app

import "syscall"

// cpuTime is the process's CPU time so far, in ms, all its threads.
func cpuTime() int64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return (ru.Utime.Nano() + ru.Stime.Nano()) / 1e6
}
