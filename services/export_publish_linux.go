package services

import (
	"os"

	"golang.org/x/sys/unix"
)

func publishModDirectory(temp, dest string) error {
	// Publish atomically without replacing a destination created after the
	// initial existence check. os.Rename rejects even an empty target directory.
	if err := unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, dest, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: temp, New: dest, Err: err}
	}
	return nil
}
