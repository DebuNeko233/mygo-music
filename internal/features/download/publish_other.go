//go:build !darwin && !linux && !windows

/*
为其他平台提供不覆盖已有目标的下载文件发布实现。
*/

package download

import "os"

func publish(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	return nil // the private staging tree is removed by the caller
}
