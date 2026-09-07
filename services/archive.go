package services

import (
	"errors"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
)

// ArchiveService: 打开/关闭归档、状态查询、资源树懒加载与搜索。
type ArchiveService struct{ c *core }

func NewArchiveService(c *core) *ArchiveService { return &ArchiveService{c: c} }

// ArchiveInfo 是前端可观察的归档状态快照。
type ArchiveInfo = pvf.ArchiveInfoView

// OpenDialog 弹出文件选择框并加载归档。
func (s *ArchiveService) OpenDialog() (*ArchiveInfo, error) {
	path, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("PVF 归档", "*.pvf").
		AddFilter("所有文件", "*").
		SetTitle("打开 PVF 归档").
		PromptForSingleSelection()
	if err != nil {
		return nil, err // 用户取消等
	}
	if path == "" {
		return nil, nil
	}
	info, err := s.Open(path)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Open 加载指定路径的归档并构建目录索引。
func (s *ArchiveService) Open(path string) (ArchiveInfo, error) {
	if _, err := os.Stat(path); err != nil {
		return ArchiveInfo{}, err
	}
	a, err := pvf.Open(path)
	if err != nil {
		return ArchiveInfo{}, err
	}
	if err := s.c.setArchive(a); err != nil {
		return ArchiveInfo{}, err
	}
	info := a.Info()
	emitEvent("archive:opened", info)
	s.c.startSearchIndex()
	return info, nil
}

// Close 关闭当前归档,丢弃未保存的内存修改。
func (s *ArchiveService) Close() {
	s.c.closeArchive()
	emitEvent("archive:closed")
}

// Info 返回当前归档状态;未打开时 Path 为空。
func (s *ArchiveService) Info() ArchiveInfo {
	info, _ := s.c.archiveInfo()
	return info
}

// IndexStatus 返回当前归档的语义搜索索引状态。
func (s *ArchiveService) IndexStatus() IndexStatus {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return s.c.indexStatus
}

// ListChildren 懒加载某目录的直接子节点;path 为空表示根。
func (s *ArchiveService) ListChildren(path string) ([]*TreeNode, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	list := s.c.dirChildren[path]
	if list == nil {
		return []*TreeNode{}, nil
	}
	result := make([]*TreeNode, len(list))
	for i, node := range list {
		copyNode := *node
		if !copyNode.IsDir {
			copyNode.Tags = cloneTreeTags(s.c.treeTagsByFile[copyNode.FileIndex])
		}
		result[i] = &copyNode
	}
	return result, nil
}

// SuggestDirectories returns directory paths with a case-insensitive prefix
// match. An empty prefix returns no suggestions to avoid flooding the UI.
func (s *ArchiveService) SuggestDirectories(prefix string, limit int) ([]string, error) {
	prefix = normalizeAdvancedScope(prefix)
	if prefix == "" {
		return []string{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	result := make([]string, 0, limit)
	for _, path := range s.c.directories {
		if !strings.HasPrefix(strings.ToLower(path), prefix) {
			continue
		}
		result = append(result, path)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

// SearchResult 是一页搜索命中;NextCursor < 0 表示已扫完。
type SearchResult struct {
	Hits       []*SearchHit `json:"hits"`
	NextCursor int          `json:"nextCursor"`
	Scanned    int          `json:"scanned"`
}

// Search 在路径、语义名称和 id 中做不区分大小写的子串匹配。
// cursor 传上次返回的 NextCursor(首次传 0),limit 为本页上限(1..1000)。
func (s *ArchiveService) Search(query string, cursor int, limit int) (*SearchResult, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	res := &SearchResult{Hits: []*SearchHit{}, NextCursor: -1}
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return res, nil
	}
	if s.c.indexStatus.State == IndexStateBuilding || s.c.indexStatus.State == IndexStateIdle {
		return nil, ErrSearchIndexing
	}
	if s.c.indexStatus.State == IndexStateError {
		return nil, errors.New(s.c.indexStatus.Error)
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}
	records := s.c.searchRecords
	i := cursor
	for ; i < len(records) && len(res.Hits) < limit; i++ {
		record := &records[i]
		if strings.Contains(record.lowerPath, q) ||
			strings.Contains(record.lowerName, q) ||
			strings.Contains(record.lowerID, q) {
			hit := record.hit
			res.Hits = append(res.Hits, &hit)
		}
	}
	if i < len(records) {
		res.NextCursor = i
	}
	res.Scanned = i
	return res, nil
}
