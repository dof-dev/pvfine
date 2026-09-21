package services

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

// ListRegistrationTarget describes a list that can register one archive file.
type ListRegistrationTarget struct {
	ListPath     string `json:"listPath"`
	Category     string `json:"category"`
	EntryPath    string `json:"entryPath"`
	SuggestedID  string `json:"suggestedId"`
	HasIndexHash bool   `json:"hasIndexHash"`
}

// FileRegistrationOptions contains the list choices for the current file.
type FileRegistrationOptions struct {
	FileIndex int32                     `json:"fileIndex"`
	FilePath  string                    `json:"filePath"`
	Targets   []*ListRegistrationTarget `json:"targets"`
}

// IndexHashTarget describes a list whose companion index-hash file exists.
type IndexHashTarget struct {
	ListPath      string `json:"listPath"`
	IndexHashPath string `json:"indexHashPath"`
}

// IndexHashRegistrationResult summarizes a batch hash registration.
type IndexHashRegistrationResult struct {
	ListPath      string   `json:"listPath"`
	IndexHashPath string   `json:"indexHashPath"`
	Requested     int      `json:"requested"`
	Added         int      `json:"added"`
	Existing      int      `json:"existing"`
	InvalidIDs    []string `json:"invalidIds,omitempty"`
}

// ListRegistrationOptions returns the configured lists that can point to a
// file. Existing registrations are supplied by the search index and displayed
// beside the editor file, so this endpoint only supplies a target list and a
// collision-free default id.
func (s *ArchiveService) ListRegistrationOptions(fileIndex int32) (*FileRegistrationOptions, error) {
	specs := s.c.searchableListSpecs()
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateFileIndexes(a, []int32{fileIndex}); err != nil {
		return nil, err
	}
	if s.c.indexStatus.State == IndexStateReady {
		tags := s.c.treeTagsByFile[fileIndex]
		if s.c.diskIndex != nil {
			tags, _ = s.c.diskIndex.tags(fileIndex)
		}
		for _, tag := range tags {
			if id := strings.TrimSpace(tag.ID); id != "" {
				return nil, fmt.Errorf("当前文件已有索引 id: %s", id)
			}
		}
	}
	filePath := a.Path(fileIndex)
	result := &FileRegistrationOptions{
		FileIndex: fileIndex,
		FilePath:  filePath,
		Targets:   make([]*ListRegistrationTarget, 0, len(specs)),
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		listIndex, ok := a.FindList(spec.listPath)
		if !ok {
			continue
		}
		listPath := a.Path(listIndex)
		key := strings.ToLower(listPath)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		pairs, err := a.ListPairs(listIndex)
		if err != nil {
			continue
		}
		suggestedID, err := nextListID(pairs, a.ContentRules().RequiresIndexHash)
		if err != nil {
			continue
		}
		hasIndexHash := false
		if companion, ok := pvf.IndexHashCompanionPath(listPath); ok {
			_, hasIndexHash = a.Find(companion)
		}
		result.Targets = append(result.Targets, &ListRegistrationTarget{
			ListPath:     listPath,
			Category:     spec.category,
			EntryPath:    listEntryPath(listPath, filePath),
			SuggestedID:  suggestedID,
			HasIndexHash: hasIndexHash,
		})
	}
	if len(result.Targets) == 0 {
		return result, fmt.Errorf("没有找到可用的 lst: %s", filePath)
	}
	sort.SliceStable(result.Targets, func(i, j int) bool {
		left, right := result.Targets[i], result.Targets[j]
		leftScore := listTargetScore(left.ListPath, filePath)
		rightScore := listTargetScore(right.ListPath, filePath)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return left.ListPath < right.ListPath
	})
	return result, nil
}

// RegisterFileToList registers one file in the selected list. Paged110 files
// also receive the generated companion index-hash entry in the same mutation.
func (s *ArchiveService) RegisterFileToList(fileIndex int32, listPath, id string) (*FileRegistration, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id 不能为空")
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
	if err := validateFileIndexes(a, []int32{fileIndex}); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	listIndex, ok := a.FindList(listPath)
	if !ok {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("lst 不存在: %s", listPath)
	}
	actualListPath := a.Path(listIndex)
	entryPath := listEntryPath(actualListPath, a.Path(fileIndex))
	pairs, err := a.ListPairs(listIndex)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	for _, pair := range pairs {
		if strings.EqualFold(strings.TrimSpace(pair.ID), id) {
			_, targetIndex, found := findListTargetInArchive(a, actualListPath, pair.Path)
			if found && targetIndex == fileIndex {
				s.c.mu.Unlock()
				return nil, fmt.Errorf("当前文件已经注册到 %s", actualListPath)
			}
			s.c.mu.Unlock()
			return nil, fmt.Errorf("id 已在 %s 中使用: %s", actualListPath, id)
		}
		_, targetIndex, found := findListTargetInArchive(a, actualListPath, pair.Path)
		if found && targetIndex == fileIndex {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("当前文件已经注册到 %s", actualListPath)
		}
	}

	var numericID uint32
	var hashPath string
	hashIndex := int32(-1)
	if a.ContentRules().RequiresIndexHash {
		parsed, parseErr := strconv.ParseUint(id, 10, 32)
		if parseErr != nil {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("此归档的 indexhash id 必须是 uint32 数字: %s", id)
		}
		numericID = uint32(parsed)
		var hashOK bool
		hashPath, hashOK = pvf.IndexHashCompanionPath(actualListPath)
		if !hashOK {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("无法确定 %s 的 indexhash 文件", actualListPath)
		}
		hashIndex, hashOK = a.Find(hashPath)
		if !hashOK {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("indexhash 不存在: %s", hashPath)
		}
	}

	changedPaths := []string{actualListPath}
	if hashPath != "" {
		changedPaths = append(changedPaths, hashPath)
	}
	before, err := contentSnapshot(a, changedPaths)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := a.SetListPair(listIndex, id, entryPath); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if hashPath != "" {
		if err := a.SetIndexHashEntriesForListIDs(hashPath, actualListPath, []uint32{numericID}); err != nil {
			s.c.mu.Unlock()
			return nil, err
		}
	}
	after, err := contentSnapshot(a, changedPaths)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := s.c.recordVersionMutationLocked("注册到列表", before, after); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	refreshRegistrationEditorTextLocked(s.c, a, listIndex, hashIndex)
	invalidateRegistrationIndexesLocked(s.c)
	info := a.Info()
	versioned := s.c.versionRepo != nil
	registration := &FileRegistration{
		ID:            id,
		FileIndex:     fileIndex,
		FilePath:      a.Path(fileIndex),
		ListFileIndex: listIndex,
		ListPath:      actualListPath,
		EntryPath:     entryPath,
	}
	s.c.mu.Unlock()

	s.c.startSearchIndexForList(listIndex)
	emitEvent("archive:registrations-changed", map[string]any{
		"fileIndexes": changedRegistrationIndexes(listIndex, hashIndex, fileIndex),
	})
	emitEvent("archive:changed", info)
	if versioned {
		emitVersionState(s.c, "registered-to-list")
	}
	return registration, nil
}

// IndexHashTargets lists the available list/indexhash pairs in a Paged110
// archive. Older archive layouts intentionally return an error.
func (s *ArchiveService) IndexHashTargets() ([]*IndexHashTarget, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if !a.ContentRules().RequiresIndexHash {
		return nil, fmt.Errorf("当前归档不使用 indexhash")
	}
	result := make([]*IndexHashTarget, 0)
	for index := int32(0); index < a.FileCount(); index++ {
		listPath := a.Path(index)
		if !strings.EqualFold(path.Ext(listPath), ".lst") {
			continue
		}
		hashPath, ok := pvf.IndexHashCompanionPath(listPath)
		if !ok {
			continue
		}
		if _, exists := a.Find(hashPath); !exists {
			continue
		}
		result = append(result, &IndexHashTarget{ListPath: listPath, IndexHashPath: hashPath})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ListPath < result[j].ListPath })
	return result, nil
}

// RegisterMissingIndexHashes writes generated hashes for ids that are absent
// from the selected companion file, and repairs entries written to the wrong
// string pool. Valid existing entries are left untouched.
func (s *ArchiveService) RegisterMissingIndexHashes(listPath string, rawIDs []string) (*IndexHashRegistrationResult, error) {
	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if !a.ContentRules().RequiresIndexHash {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("当前归档不使用 indexhash")
	}
	listIndex, ok := a.FindList(listPath)
	if !ok {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("lst 不存在: %s", listPath)
	}
	actualListPath := a.Path(listIndex)
	hashPath, ok := pvf.IndexHashCompanionPath(actualListPath)
	if !ok {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("无法确定 %s 的 indexhash 文件", actualListPath)
	}
	hashIndex, ok := a.Find(hashPath)
	if !ok {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("indexhash 不存在: %s", hashPath)
	}
	result := &IndexHashRegistrationResult{
		ListPath:      actualListPath,
		IndexHashPath: hashPath,
		InvalidIDs:    []string{},
	}
	ids := make([]uint32, 0, len(rawIDs))
	seen := make(map[uint32]struct{}, len(rawIDs))
	for _, rawID := range rawIDs {
		trimmed := strings.TrimSpace(rawID)
		if trimmed == "" {
			continue
		}
		id, parseErr := strconv.ParseUint(trimmed, 10, 32)
		if parseErr != nil {
			result.InvalidIDs = append(result.InvalidIDs, trimmed)
			continue
		}
		value := uint32(id)
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	result.Requested = len(ids)
	if len(ids) == 0 {
		s.c.mu.Unlock()
		return result, nil
	}
	pairs, err := a.IndexHashPairs(hashPath)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	have := make(map[uint32]struct{}, len(pairs))
	for _, pair := range pairs {
		have[pair.ID] = struct{}{}
	}
	updates, err := a.IndexHashIDsNeedingUpdate(hashPath, ids)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	for _, id := range ids {
		if _, exists := have[id]; exists {
			continue
		}
		result.Added++
	}
	result.Existing = len(ids) - len(updates)
	if len(updates) == 0 {
		s.c.mu.Unlock()
		return result, nil
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	before, err := contentSnapshot(a, []string{hashPath})
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := a.SetIndexHashEntriesForListIDs(hashPath, actualListPath, updates); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	after, err := contentSnapshot(a, []string{hashPath})
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := s.c.recordVersionMutationLocked("注册 indexhash", before, after); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	refreshRegistrationEditorTextLocked(s.c, a, -1, hashIndex)
	invalidateRegistrationIndexesLocked(s.c)
	result.Added = len(updates)
	info := a.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	emitEvent("archive:registrations-changed", map[string]any{"fileIndexes": []int32{hashIndex}})
	emitEvent("archive:changed", info)
	if versioned {
		emitVersionState(s.c, "registered-indexhash")
	}
	return result, nil
}

func contentSnapshot(a *pvf.Archive, paths []string) (pvfversion.ContentSnapshot, error) {
	unique := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, filePath := range paths {
		if filePath == "" {
			continue
		}
		if _, exists := seen[filePath]; exists {
			continue
		}
		seen[filePath] = struct{}{}
		unique = append(unique, filePath)
	}
	return pvfversion.ContentSnapshotFromArchive(a, unique)
}

func listEntryPath(listPath, filePath string) string {
	listPath = strings.Trim(strings.ReplaceAll(listPath, "\\", "/"), "/")
	filePath = strings.Trim(strings.ReplaceAll(filePath, "\\", "/"), "/")
	dir := path.Dir(listPath)
	if dir != "." && !strings.EqualFold(dir, "list") {
		prefix := dir + "/"
		if len(filePath) >= len(prefix) && strings.EqualFold(filePath[:len(prefix)], prefix) {
			return filePath[len(prefix):]
		}
	}
	return filePath
}

func nextListID(pairs []pvf.ListPair, requiresIndexHash bool) (string, error) {
	used := make(map[string]struct{}, len(pairs))
	var next uint64 = 1
	for _, pair := range pairs {
		id := strings.TrimSpace(pair.ID)
		used[id] = struct{}{}
		if parsed, err := strconv.ParseUint(id, 10, 64); err == nil && parsed >= next {
			next = parsed + 1
		}
	}
	for {
		if requiresIndexHash && next > uint64(^uint32(0)) {
			return "", fmt.Errorf("列表没有可用的 uint32 id")
		}
		candidate := strconv.FormatUint(next, 10)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
		next++
	}
}

func listTargetScore(listPath, filePath string) int {
	listPath = strings.ToLower(strings.Trim(strings.ReplaceAll(listPath, "\\", "/"), "/"))
	filePath = strings.ToLower(strings.Trim(strings.ReplaceAll(filePath, "\\", "/"), "/"))
	base := strings.TrimSuffix(path.Base(listPath), path.Ext(listPath))
	if strings.HasPrefix(filePath, base+"/") {
		return 2
	}
	if strings.HasPrefix(filePath, path.Dir(listPath)+"/") {
		return 1
	}
	return 0
}

func refreshRegistrationEditorTextLocked(c *core, a *pvf.Archive, listIndex, hashIndex int32) {
	if c.editorText == nil {
		c.editorText = make(map[int32]string)
	}
	for _, index := range []int32{listIndex, hashIndex} {
		if index < 0 {
			continue
		}
		if text, err := a.Text(index); err == nil {
			c.editorText[index] = text
		}
	}
}

func changedRegistrationIndexes(listIndex, hashIndex, fileIndex int32) []int32 {
	result := make([]int32, 0, 3)
	seen := make(map[int32]struct{}, 3)
	for _, index := range []int32{listIndex, hashIndex, fileIndex} {
		if index < 0 {
			continue
		}
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		result = append(result, index)
	}
	return result
}

func invalidateRegistrationIndexesLocked(c *core) {
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorAnnotation = editorAnnotationCache{}
	c.invalidateAdvancedSearchLocked()
}
