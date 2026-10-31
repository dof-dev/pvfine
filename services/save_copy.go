package services

import (
	"fmt"
	"os"
	"path/filepath"

	pvfversion "pvfine/internal/version"
)

// saveCopy serializes an isolated clone so copying never marks the source saved.
func (s *EditorService) saveCopy(path string, overwrite bool, expectedDigest string) (string, error) {
	finish := s.c.archiveTasks.begin()
	defer finish()
	s.c.mu.Lock()
	if s.c.archive == nil {
		s.c.mu.Unlock()
		return "", ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return "", err
	}
	source := s.c.archive.SourcePath()
	if sameDiskPath(source, path) {
		s.c.mu.Unlock()
		return "", fmt.Errorf("SAME_AS_SOURCE: 请使用保存")
	}
	clone := s.c.archive.CloneForBatch()
	s.c.mu.Unlock()
	defer clone.Release()
	f, err := os.CreateTemp(filepath.Dir(path), ".pvfine-copy-*")
	if err != nil {
		return "", err
	}
	temp := f.Name()
	defer os.Remove(temp)
	err = clone.SaveToWithProgress(f, archiveSaveProgressEmitter())
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if overwrite {
		if expectedDigest != "" {
			actual, err := pvfversion.HashFile(path)
			if err != nil || actual != expectedDigest {
				return "", fmt.Errorf("TARGET_CHANGED")
			}
		}
		err = os.Rename(temp, path)
	} else {
		// Linking is atomic and fails if a competing writer created the destination.
		err = os.Link(temp, path)
	}
	if err != nil {
		return "", err
	}
	emitEvent("archive:copy-saved", map[string]any{"writtenPath": path, "sourcePath": source})
	return path, nil
}
