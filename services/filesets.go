package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const fileSetDocumentVersion = 1

// StoredFileSetEntry 是文件集落盘格式。fileIndex 不持久化,因为它只对当前 PVF 有效。
type StoredFileSetEntry struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	IDs      []string `json:"ids,omitempty"`
	Size     int32    `json:"size"`
	DataType int32    `json:"dataType"`
}

type StoredFileSet struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Entries []StoredFileSetEntry `json:"entries"`
}

// FileSetDocument 是全部文件集的持久化文档,与当前打开的 PVF 无关。
type FileSetDocument struct {
	Version     int             `json:"version"`
	FileSets    []StoredFileSet `json:"fileSets"`
	ActiveSetID string          `json:"activeSetId"`
}

type FileSetService struct {
	mu      sync.Mutex
	path    string
	initErr error
}

func NewFileSetService() *FileSetService {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return &FileSetService{initErr: fmt.Errorf("获取用户配置目录失败: %w", err)}
	}
	return newFileSetService(filepath.Join(configDir, "pvfine", "file-sets.json"))
}

func newFileSetService(path string) *FileSetService {
	return &FileSetService{path: path}
}

// LoadFileSets 从用户配置目录读取已保存的文件集。文件不存在时返回空文档。
func (s *FileSetService) LoadFileSets() (FileSetDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return FileSetDocument{}, s.initErr
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return emptyFileSetDocument(), nil
	}
	if err != nil {
		return FileSetDocument{}, fmt.Errorf("读取文件集失败: %w", err)
	}

	var document FileSetDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return FileSetDocument{}, fmt.Errorf("解析文件集失败: %w", err)
	}
	if err := normalizeFileSetDocument(&document); err != nil {
		return FileSetDocument{}, err
	}
	return document, nil
}

// SaveFileSets 将全部文件集以原子方式保存到用户配置目录。
func (s *FileSetService) SaveFileSets(document FileSetDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	if err := normalizeFileSetDocument(&document); err != nil {
		return err
	}
	if len(document.FileSets) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除空文件集文件失败: %w", err)
		}
		return nil
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("编码文件集失败: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建文件集目录失败: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".file-sets-*.tmp")
	if err != nil {
		return fmt.Errorf("创建文件集临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("写入文件集失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("同步文件集失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭文件集文件失败: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("替换文件集文件失败: %w", err)
	}
	return nil
}

func emptyFileSetDocument() FileSetDocument {
	return FileSetDocument{Version: fileSetDocumentVersion}
}

func normalizeFileSetDocument(document *FileSetDocument) error {
	if document.Version == 0 {
		document.Version = fileSetDocumentVersion
	}
	if document.Version != fileSetDocumentVersion {
		return fmt.Errorf("不支持的文件集版本: %d", document.Version)
	}
	document.ActiveSetID = strings.TrimSpace(document.ActiveSetID)

	seenIDs := make(map[string]struct{}, len(document.FileSets))
	activeExists := false
	for i := range document.FileSets {
		fileSet := &document.FileSets[i]
		fileSet.ID = strings.TrimSpace(fileSet.ID)
		fileSet.Name = strings.TrimSpace(fileSet.Name)
		if fileSet.ID == "" || fileSet.Name == "" {
			return fmt.Errorf("文件集名称或 id 为空")
		}
		if _, exists := seenIDs[fileSet.ID]; exists {
			return fmt.Errorf("文件集 id 重复: %q", fileSet.ID)
		}
		seenIDs[fileSet.ID] = struct{}{}
		if fileSet.ID == document.ActiveSetID {
			activeExists = true
		}

		entries := fileSet.Entries[:0]
		seenPaths := make(map[string]struct{}, len(fileSet.Entries))
		for _, entry := range fileSet.Entries {
			entry.Path = normalizeFileSetPath(entry.Path)
			if entry.Path == "" {
				continue
			}
			if _, exists := seenPaths[entry.Path]; exists {
				continue
			}
			seenPaths[entry.Path] = struct{}{}
			entry.IDs = uniqueStrings(entry.IDs)
			entries = append(entries, entry)
		}
		fileSet.Entries = entries
	}
	if document.ActiveSetID != "" && !activeExists {
		document.ActiveSetID = ""
	}
	if document.ActiveSetID == "" && len(document.FileSets) > 0 {
		document.ActiveSetID = document.FileSets[0].ID
	}
	return nil
}

func normalizeFileSetPath(path string) string {
	return strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
