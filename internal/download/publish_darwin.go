package download

import (
	"golang.org/x/sys/unix"
	"os"
)

// A same-volume no-replace rename also works on removable volumes without hard links.
func publish(from, to string) error {
	if err := unix.RenamexNp(from, to, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
