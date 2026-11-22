//go:build !darwin && !linux && !windows

package download

import "os"

func publish(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	return nil // the private staging tree is removed by the caller
}
