package services

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

// ArchiveService: 打开/关闭归档、状态查询、资源树懒加载与搜索。
type ArchiveService struct{ c *core }

func NewArchiveService(c *core) *ArchiveService { return &ArchiveService{c: c} }

// ArchiveInfo 是前端可观察的归档状态快照。
type ArchiveInfo = pvf.ArchiveInfoView

// FileRegistration describes one indexed id/path entry in an archive list.
type FileRegistration struct {
	ID            string `json:"id"`
	Category      string `json:"category"`
	FileIndex     int32  `json:"fileIndex"`
	FilePath      string `json:"filePath"`
	ListFileIndex int32  `json:"listFileIndex"`
	ListPath      string `json:"listPath"`
	EntryPath     string `json:"entryPath"`
}

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
	// Version repository discovery/recovery is deliberately detached from the
	// normal open path. The raw PVF and its tree are usable immediately; the
	// background task will replace the in-memory archive only when recovery is
	// actually needed.
	s.c.startVersionLoad(path, a)
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
		copyNode.Annotations = cloneTreeAnnotations(s.c.pathAnnotations[copyNode.Path])
		if !copyNode.IsDir {
			copyNode.Tags = cloneTreeTags(s.c.treeTagsByFile[copyNode.FileIndex])
		}
		result[i] = &copyNode
	}
	return result, nil
}

// ListDescendantFiles returns all files below a directory path. An empty path
// returns every file in the archive. Results preserve the archive path order.
func (s *ArchiveService) ListDescendantFiles(scopePath string) ([]*TreeNode, error) {
	scopePath = strings.Trim(strings.ReplaceAll(scopePath, "\\", "/"), "/")

	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}

	prefix := scopePath
	start := 0
	if scopePath != "" {
		prefix += "/"
		start = sort.Search(len(s.c.sortedPaths), func(i int) bool {
			return s.c.sortedPaths[i].path >= prefix
		})
	}

	result := make([]*TreeNode, 0)
	for _, entry := range s.c.sortedPaths[start:] {
		if scopePath != "" && !strings.HasPrefix(entry.path, prefix) {
			break
		}
		result = append(result, &TreeNode{
			Name:        pathBase(entry.path),
			Path:        entry.path,
			Size:        entry.size,
			DataType:    entry.typ,
			FileIndex:   entry.idx,
			Tags:        cloneTreeTags(s.c.treeTagsByFile[entry.idx]),
			Annotations: cloneTreeAnnotations(s.c.pathAnnotations[entry.path]),
		})
	}
	return result, nil
}

// ResolveFiles resolves archive files by their normalized paths. Missing
// paths are omitted and duplicate input paths are returned only once.
func (s *ArchiveService) ResolveFiles(paths []string) ([]*TreeNode, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}

	result := make([]*TreeNode, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, rawPath := range paths {
		filePath := strings.Trim(strings.ReplaceAll(rawPath, "\\", "/"), "/")
		if filePath == "" {
			continue
		}
		if _, ok := seen[filePath]; ok {
			continue
		}
		seen[filePath] = struct{}{}

		index := sort.Search(len(s.c.sortedPaths), func(i int) bool {
			return s.c.sortedPaths[i].path >= filePath
		})
		if index >= len(s.c.sortedPaths) || s.c.sortedPaths[index].path != filePath {
			continue
		}
		entry := s.c.sortedPaths[index]
		result = append(result, &TreeNode{
			Name:        pathBase(entry.path),
			Path:        entry.path,
			Size:        entry.size,
			DataType:    entry.typ,
			FileIndex:   entry.idx,
			Tags:        cloneTreeTags(s.c.treeTagsByFile[entry.idx]),
			Annotations: cloneTreeAnnotations(s.c.pathAnnotations[entry.path]),
		})
	}
	return result, nil
}

// FindFileRegistrations returns configured .lst entries that point to the
// supplied file indexes. It uses the same relation definitions as the search
// index, including contextual skill lists.
func (s *ArchiveService) FindFileRegistrations(fileIndexes []int32) ([]*FileRegistration, error) {
	specs := s.c.searchableListSpecs()
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := validateFileIndexes(s.c.archive, fileIndexes); err != nil {
		return nil, err
	}
	return findFileRegistrationsLocked(s.c.archive, specs, fileIndexes), nil
}

// CreateFile adds an empty editable file to the current archive. dataType is
// pvf.TypeScript or pvf.TypeUnicode; the caller can fill its content through
// EditorService.SetText afterwards.
func (s *ArchiveService) CreateFile(path string, dataType int32) (*TreeNode, error) {
	path, err := normalizeNewFilePath(path)
	if err != nil {
		return nil, err
	}
	if dataType != pvf.TypeScript && dataType != pvf.TypeUnicode {
		return nil, fmt.Errorf("不支持的新文件类型: %d", dataType)
	}

	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if _, exists := a.Find(path); exists {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("文件已存在: %s", path)
	}
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before = make(pvfversion.ContentSnapshot)
	}
	index := a.AddFile(path, []byte{}, dataType)
	if err := s.c.rebuildArchiveIndexesLocked(a); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo != nil {
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(a, []string{path})
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return nil, snapshotErr
		}
		if recordErr := s.c.recordVersionMutationLocked("新建文件", before, after); recordErr != nil {
			s.c.mu.Unlock()
			return nil, recordErr
		}
	}
	node := &TreeNode{
		Name:        a.File(index).Name,
		Path:        a.Path(index),
		Size:        0,
		DataType:    dataType,
		FileIndex:   index,
		Annotations: cloneTreeAnnotations(s.c.pathAnnotations[a.Path(index)]),
	}
	info := a.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	s.c.startSearchIndex()
	emitEvent("archive:changed", info)
	if versioned {
		emitVersionState(s.c, "file-created")
	}
	return node, nil
}

// DeleteFiles removes one or more file entries from the current archive.
func (s *ArchiveService) DeleteFiles(fileIndexes []int32) ([]string, error) {
	return s.deleteFiles(fileIndexes, false)
}

// DeleteFilesWithRegistrations removes files and, when requested, the
// configured .lst entries that register those files.
func (s *ArchiveService) DeleteFilesWithRegistrations(fileIndexes []int32, syncRegistrations bool) ([]string, error) {
	return s.deleteFiles(fileIndexes, syncRegistrations)
}

func (s *ArchiveService) deleteFiles(fileIndexes []int32, syncRegistrations bool) ([]string, error) {
	var specs []searchableListSpec
	if syncRegistrations {
		specs = s.c.searchableListSpecs()
	}

	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := validateFileIndexes(a, fileIndexes); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	mutationPaths := make([]string, 0, len(fileIndexes))
	for _, index := range fileIndexes {
		mutationPaths = append(mutationPaths, a.Path(index))
	}
	var registrations []*FileRegistration
	if syncRegistrations {
		registrations = findFileRegistrationsLocked(a, specs, fileIndexes)
		for _, registration := range registrations {
			mutationPaths = append(mutationPaths, registration.ListPath)
		}
	}
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		var snapshotErr error
		before, snapshotErr = pvfversion.ContentSnapshotFromArchive(a, mutationPaths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return nil, snapshotErr
		}
	}
	if syncRegistrations {
		byList := make(map[int32][]pvf.ListPair)
		for _, registration := range registrations {
			byList[registration.ListFileIndex] = append(
				byList[registration.ListFileIndex],
				pvf.ListPair{ID: registration.ID, Path: registration.EntryPath},
			)
		}
		for listIndex, entries := range byList {
			if _, err := a.RemoveListPairs(listIndex, entries); err != nil {
				s.c.mu.Unlock()
				return nil, err
			}
		}
	}
	paths, err := a.RemoveFiles(fileIndexes)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if len(paths) == 0 {
		s.c.mu.Unlock()
		return []string{}, nil
	}
	if err := s.c.rebuildArchiveIndexesLocked(a); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo != nil {
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(a, mutationPaths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return nil, snapshotErr
		}
		if recordErr := s.c.recordVersionMutationLocked("删除文件", before, after); recordErr != nil {
			s.c.mu.Unlock()
			return nil, recordErr
		}
	}
	info := a.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	s.c.startSearchIndex()
	emitEvent("archive:changed", info)
	if versioned {
		emitVersionState(s.c, "files-deleted")
	}
	return paths, nil
}

func validateFileIndexes(a *pvf.Archive, indexes []int32) error {
	for _, index := range indexes {
		if index < 0 || index >= a.FileCount() {
			return fmt.Errorf("文件索引越界: %d", index)
		}
	}
	return nil
}

func findFileRegistrationsLocked(a *pvf.Archive, specs []searchableListSpec, fileIndexes []int32) []*FileRegistration {
	targets := make(map[int32]string, len(fileIndexes))
	for _, index := range fileIndexes {
		if index < 0 || index >= a.FileCount() {
			continue
		}
		targets[index] = a.Path(index)
	}
	if len(targets) == 0 {
		return []*FileRegistration{}
	}

	registrations := make([]*FileRegistration, 0)
	for _, spec := range specs {
		listIndex, ok := a.Find(spec.listPath)
		if !ok {
			continue
		}
		pairs, err := a.ScriptListPairs(listIndex)
		if err != nil {
			continue
		}
		listPath := a.Path(listIndex)
		for _, pair := range pairs {
			targetPath, ok := resolveListPath(spec.listPath, pair.Path)
			if !ok {
				continue
			}
			targetIndex, ok := a.Find(targetPath)
			if !ok {
				continue
			}
			filePath, selected := targets[targetIndex]
			if !selected {
				continue
			}
			registrations = append(registrations, &FileRegistration{
				ID:            pair.ID,
				Category:      spec.category,
				FileIndex:     targetIndex,
				FilePath:      filePath,
				ListFileIndex: listIndex,
				ListPath:      listPath,
				EntryPath:     pair.Path,
			})
		}
	}
	sort.SliceStable(registrations, func(i, j int) bool {
		left, right := registrations[i], registrations[j]
		if left.FilePath != right.FilePath {
			return left.FilePath < right.FilePath
		}
		if left.ListPath != right.ListPath {
			return left.ListPath < right.ListPath
		}
		return left.ID < right.ID
	})
	return registrations
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

func normalizeNewFilePath(raw string) (string, error) {
	path := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if path == "" {
		return "", errors.New("文件名不能为空")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("文件路径无效: %q", raw)
		}
	}
	return path, nil
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
	return s.search(query, cursor, limit, false)
}

// SearchExact 在路径、语义名称和 id 中做不区分大小写的全量匹配。
// cursor 传上次返回的 NextCursor(首次传 0),limit 为本页上限(1..1000)。
func (s *ArchiveService) SearchExact(query string, cursor int, limit int) (*SearchResult, error) {
	return s.search(query, cursor, limit, true)
}

func (s *ArchiveService) search(query string, cursor int, limit int, exact bool) (*SearchResult, error) {
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
		matched := false
		if exact {
			matched = record.lowerPath == q || record.lowerName == q || record.lowerID == q
		} else {
			matched = strings.Contains(record.lowerPath, q) ||
				strings.Contains(record.lowerName, q) ||
				strings.Contains(record.lowerID, q)
		}
		if matched {
			hit := record.hit
			hit.Annotations = cloneTreeAnnotations(s.c.pathAnnotations[hit.Path])
			hit.PathAnnotations = clonePathAnnotationChain(s.c.pathAnnotations, hit.Path)
			res.Hits = append(res.Hits, &hit)
		}
	}
	if i < len(records) {
		res.NextCursor = i
	}
	res.Scanned = i
	return res, nil
}

func clonePathAnnotationChain(values map[string][]TreeAnnotation, filePath string) map[string][]TreeAnnotation {
	result := make(map[string][]TreeAnnotation)
	current := filePath
	for current != "" {
		if annotations := cloneTreeAnnotations(values[current]); len(annotations) > 0 {
			result[current] = annotations
		}
		current, _ = splitParent(current)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
