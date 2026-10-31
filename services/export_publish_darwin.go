package services

import (
	"os"

	"golang.org/x/sys/unix"
)

func publishModDirectory(temp, dest string) error {
	if err := unix.RenamexNp(temp, dest, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: temp, New: dest, Err: err}
	}
	return nil
}
