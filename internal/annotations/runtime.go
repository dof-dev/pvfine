package annotations

import (
	"fmt"
	"os"
	"path/filepath"
)

const sourceRelativePath = "internal/annotations/default.json"

// LoadFile validates and compiles a rule document from disk.
func LoadFile(path string) (*Engine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取标注规则失败: %w", err)
	}
	document, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return Compile(document)
}

// RuntimePath is the writable rule mirror used by packaged applications.
func RuntimePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	return filepath.Join(configDir, "pvfine", "annotations.json"), nil
}

// FindSourcePath locates the repository rule file for local development.
func FindSourcePath() (string, bool) {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				candidate := filepath.Join(dir, filepath.FromSlash(sourceRelativePath))
				if fileExists(filepath.Join(dir, "go.mod")) && fileExists(candidate) {
					return candidate, true
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
