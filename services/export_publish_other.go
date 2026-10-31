//go:build !linux && !windows && !darwin

package services

import (
	"fmt"
	"os"
)

func publishModDirectory(temp, dest string) error {
	if err := os.Mkdir(dest, 0o755); err != nil {
		return fmt.Errorf("无法创建 mod 目录（可能已存在）: %w", err)
	}
	if err := os.Rename(temp, dest); err != nil {
		_ = os.Remove(dest) // Remove refuses a nonempty directory.
		return err
	}
	return nil
}
