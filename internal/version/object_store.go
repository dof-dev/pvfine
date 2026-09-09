package version

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ObjectStore stores raw logical file payloads by SHA-256.
type ObjectStore struct {
	root string
}

func newObjectStore(root string) *ObjectStore { return &ObjectStore{root: root} }

// Put atomically writes raw content and returns its content hash.  Existing
// objects are immutable and are reused.
func (s *ObjectStore) Put(raw []byte) (string, error) {
	if s == nil {
		return "", errors.New("对象存储为空")
	}
	hash := HashBytes(raw)
	target, err := s.pathFor(hash)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(target); err == nil {
		return hash, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".object-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		// Another writer may have installed the immutable object first.
		if _, statErr := os.Stat(target); statErr == nil {
			return hash, nil
		}
		return "", err
	}
	removeTemp = false
	return hash, nil
}

// Read returns an immutable object payload.
func (s *ObjectStore) Read(hash string) ([]byte, error) {
	path, err := s.pathFor(hash)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *ObjectStore) pathFor(hash string) (string, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 64 {
		return "", fmt.Errorf("对象 hash 长度无效: %q", hash)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", fmt.Errorf("对象 hash 无效: %w", err)
	}
	return filepath.Join(s.root, hash[:2], hash[2:]), nil
}

func copyFileAtomic(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".copy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, input); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	removeTemp = false
	return nil
}
