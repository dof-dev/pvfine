package services

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

const (
	IndexStateIdle     = "idle"
	IndexStateBuilding = "building"
	IndexStateReady    = "ready"
	IndexStateError    = "error"

	SearchCategoryFile      = "file"
	SearchCategoryEquipment = "equipment"
	SearchCategoryStackable = "stackable"
	SearchCategorySkill     = "skill"
)

// IndexStatus is the current state of the semantic search index.
type IndexStatus struct {
	State   string `json:"state"`
	Stage   string `json:"stage"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Skipped int    `json:"skipped"`
	Error   string `json:"error"`
}

// SearchHit is one searchable file/list record.
type SearchHit struct {
	Name            string                      `json:"name"`
	ID              string                      `json:"id"`
	Path            string                      `json:"path"`
	Category        string                      `json:"category"`
	Size            int32                       `json:"size"`
	DataType        int32                       `json:"dataType"`
	FileIndex       int32                       `json:"fileIndex"`
	Annotations     []TreeAnnotation            `json:"annotations,omitempty"`
	PathAnnotations map[string][]TreeAnnotation `json:"pathAnnotations,omitempty"`
}

// TreeTag is one list mapping displayed after a file name in the explorer.
type TreeTag struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type searchRecord struct {
	hit       SearchHit
	lowerName string
	lowerID   string
	lowerPath string
}

type indexedMetadata struct {
	name      string
	id        string
	category  string
	path      string
	fileIndex int32
	size      int32
	dataType  int32
}

type searchableListSpec struct {
	listPath string
	category string
}

func (c *core) searchableListSpecs() []searchableListSpec {
	specs := make([]searchableListSpec, 0, 16)
	seen := make(map[string]struct{})
	appendSpec := func(spec searchableListSpec) {
		key := strings.ToLower(spec.category + "\x00" + spec.listPath)
		if spec.listPath == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		specs = append(specs, spec)
	}
	c.mu.RLock()
	engine := c.annotationEngine
	if engine != nil {
		for name, relation := range engine.Document().Relations {
			category := relationSearchCategory(name)
			kind := relation.Kind
			if kind == "" {
				kind = "list"
			}
			if kind == "list" {
				appendSpec(searchableListSpec{listPath: relation.ListPath, category: category})
				continue
			}
			if kind != "contextual" {
				continue
			}
			for _, listPath := range relation.ContextPaths {
				appendSpec(searchableListSpec{listPath: listPath, category: category})
			}
		}
	}
	c.mu.RUnlock()

	sort.SliceStable(specs, func(i, j int) bool {
		if specs[i].category != specs[j].category {
			return specs[i].category < specs[j].category
		}
		return specs[i].listPath < specs[j].listPath
	})
	return specs
}

func relationSearchCategory(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "装备", "equipment":
		return SearchCategoryEquipment
	case "道具", "stackable":
		return SearchCategoryStackable
	case "技能", "skill":
		return SearchCategorySkill
	default:
		return "relation:" + strings.TrimSpace(name)
	}
}

// startSearchIndex starts a new metadata indexing generation for the current
// archive. The path/tree index is already available when this runs.
func (c *core) startSearchIndex() {
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return
	}
	if c.indexCancel != nil {
		c.indexCancel()
	}
	c.indexGen++
	gen := c.indexGen
	a := c.archive
	ctx, cancel := context.WithCancel(context.Background())
	c.indexCancel = cancel
	c.indexDirty = make(map[int32]struct{})
	c.indexStatus = IndexStatus{State: IndexStateBuilding, Stage: "preparing"}
	c.mu.Unlock()

	emitEvent("archive:index-progress", IndexStatus{State: IndexStateBuilding, Stage: "preparing"})
	go c.buildSearchIndex(ctx, gen, a)
}

func (c *core) buildSearchIndex(ctx context.Context, gen uint64, a *pvf.Archive) {
	locked := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if locked {
				c.mu.Unlock()
			}
			c.failSearchIndex(a, gen, ctx, fmt.Errorf("搜索索引构建失败: %v", recovered))
		}
	}()

	refs := make([]indexedMetadata, 0)
	skipped := 0
	paths, ok := c.snapshotPaths(a, gen, ctx)
	if !ok {
		return
	}

	for _, spec := range c.searchableListSpecs() {
		if !c.indexCurrent(a, gen, ctx) {
			return
		}
		listIndex, ok := c.findArchiveEntry(a, gen, ctx, spec.listPath)
		if !ok {
			continue
		}

		pairs, err := c.readListPairs(a, gen, ctx, listIndex)
		if err != nil {
			skipped++
			continue
		}
		for _, pair := range pairs {
			target, ok := resolveListPath(spec.listPath, pair.Path)
			if !ok {
				skipped++
				continue
			}
			refs = append(refs, indexedMetadata{
				id:       pair.ID,
				category: spec.category,
				path:     target,
			})
		}
	}

	if !c.publishIndexStatus(a, gen, ctx, IndexStatus{
		State:   IndexStateBuilding,
		Stage:   "metadata",
		Total:   len(refs),
		Skipped: skipped,
	}) {
		return
	}

	metadata := make([]indexedMetadata, 0, len(refs))
	byFile := make(map[int32][]int)
	lastProgress := time.Now()
	for i, ref := range refs {
		if !c.indexCurrent(a, gen, ctx) {
			return
		}

		fileIndex, ok := c.findArchiveEntry(a, gen, ctx, ref.path)
		if !ok {
			skipped++
			c.publishIndexProgress(a, gen, ctx, i+1, len(refs), skipped, &lastProgress)
			continue
		}

		f, canonicalPath, ok := c.readFileMetadata(a, gen, ctx, fileIndex)
		if !ok {
			return
		}
		name, _, err := c.readScriptName(a, gen, ctx, fileIndex)
		if err != nil {
			// The id/path mapping remains useful even when the target is not
			// a parseable script or has malformed content.
			name = ""
		}
		ref.name = name
		ref.fileIndex = fileIndex
		ref.path = canonicalPath
		ref.size = f.DataSize
		ref.dataType = f.DataType
		byFile[fileIndex] = append(byFile[fileIndex], len(metadata))
		metadata = append(metadata, ref)
		c.publishIndexProgress(a, gen, ctx, i+1, len(refs), skipped, &lastProgress)
	}

	records, recordsByFile := buildSearchRecords(paths, metadata, byFile)
	treeTagsByFile := buildTreeTags(records, recordsByFile)

	c.mu.Lock()
	locked = true
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		c.mu.Unlock()
		locked = false
		return
	}
	// A SetText may have landed after a file was scanned. Re-read only those
	// files before publishing so the first ready snapshot cannot be stale.
	for fileIndex := range c.indexDirty {
		name, _, err := a.ScriptName(fileIndex)
		if err != nil {
			name = ""
		}
		for _, recordIndex := range recordsByFile[fileIndex] {
			if records[recordIndex].hit.Category == SearchCategoryFile {
				continue
			}
			records[recordIndex].hit.Name = name
			records[recordIndex].lowerName = strings.ToLower(name)
		}
	}
	c.searchRecords = records
	c.searchByFile = recordsByFile
	c.treeTagsByFile = treeTagsByFile
	c.indexStatus = IndexStatus{
		State:   IndexStateReady,
		Stage:   "ready",
		Done:    len(refs),
		Total:   len(refs),
		Skipped: skipped,
	}
	c.indexDirty = make(map[int32]struct{})
	c.indexCancel = nil
	status := c.indexStatus
	c.mu.Unlock()
	locked = false

	emitEvent("archive:index-ready", status)
}

func (c *core) failSearchIndex(a *pvf.Archive, gen uint64, ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	if !c.indexIsCurrentLocked(a, gen) {
		c.mu.Unlock()
		return
	}
	c.indexStatus = IndexStatus{
		State: IndexStateError,
		Stage: "error",
		Error: err.Error(),
	}
	c.indexCancel = nil
	status := c.indexStatus
	c.mu.Unlock()
	emitEvent("archive:index-error", status)
}

// setText updates an archive script and synchronizes its semantic search name
// when the file is already represented by the completed index.
func (c *core) setText(index int32, text string) (bool, string, error) {
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return false, "", ErrNoArchive
	}
	if err := c.ensureVersionReadyLocked(); err != nil {
		c.mu.Unlock()
		return false, "", err
	}
	if index < 0 || index >= c.archive.FileCount() {
		err := fmt.Errorf("文件索引越界: %d", index)
		c.mu.Unlock()
		return false, "", err
	}
	path := c.archive.Path(index)
	var before pvfversion.ContentSnapshot
	if c.versionRepo != nil {
		var err error
		before, err = pvfversion.ContentSnapshotFromArchive(c.archive, []string{path})
		if err != nil {
			c.mu.Unlock()
			return false, "", err
		}
	}
	if err := c.archive.SetText(index, text); err != nil {
		c.mu.Unlock()
		return false, "", err
	}
	if c.versionRepo != nil {
		after, err := pvfversion.ContentSnapshotFromArchive(c.archive, []string{path})
		if err != nil {
			c.mu.Unlock()
			return false, "", err
		}
		if err := c.recordVersionMutationLocked("编辑文件", before, after); err != nil {
			c.mu.Unlock()
			return false, "", err
		}
	}
	c.batchRevision++
	c.batchPlan = nil
	versioned := c.versionRepo != nil
	if c.editorText == nil {
		c.editorText = make(map[int32]string)
	}
	c.editorText[index] = text
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorAnnotation = editorAnnotationCache{}
	c.invalidateAdvancedSearchLocked()
	if c.indexStatus.State != IndexStateReady {
		if c.indexDirty == nil {
			c.indexDirty = make(map[int32]struct{})
		}
		c.indexDirty[index] = struct{}{}
		c.mu.Unlock()
		emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
		if versioned {
			emitVersionState(c, "edited")
		}
		return false, "", nil
	}

	recordIndexes := c.searchByFile[index]
	if len(recordIndexes) == 0 {
		c.mu.Unlock()
		emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
		if versioned {
			emitVersionState(c, "edited")
		}
		return false, "", nil
	}
	name, _, err := c.archive.ScriptName(index)
	if err != nil {
		name = ""
	}
	updated := false
	for _, recordIndex := range recordIndexes {
		if c.searchRecords[recordIndex].hit.Category == SearchCategoryFile {
			continue
		}
		c.searchRecords[recordIndex].hit.Name = name
		c.searchRecords[recordIndex].lowerName = strings.ToLower(name)
		updated = true
	}
	c.treeTagsByFile[index] = treeTagsForRecords(c.searchRecords, recordIndexes)
	c.mu.Unlock()
	emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
	if versioned {
		emitVersionState(c, "edited")
	}
	if updated {
		emitEvent("archive:index-updated", map[string]any{
			"fileIndex": index,
			"name":      name,
		})
	}
	return updated, name, nil
}

func (c *core) publishIndexProgress(a *pvf.Archive, gen uint64, ctx context.Context, done, total, skipped int, last *time.Time) {
	if done != total && done%1000 != 0 && time.Since(*last) < 100*time.Millisecond {
		return
	}
	status := IndexStatus{
		State:   IndexStateBuilding,
		Stage:   "metadata",
		Done:    done,
		Total:   total,
		Skipped: skipped,
	}
	if c.publishIndexStatus(a, gen, ctx, status) {
		*last = time.Now()
	}
}

func (c *core) publishIndexStatus(a *pvf.Archive, gen uint64, ctx context.Context, status IndexStatus) bool {
	c.mu.Lock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		c.mu.Unlock()
		return false
	}
	c.indexStatus = status
	c.mu.Unlock()
	emitEvent("archive:index-progress", status)
	return true
}

func (c *core) indexCurrent(a *pvf.Archive, gen uint64, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.indexIsCurrentLocked(a, gen)
}

func (c *core) indexIsCurrentLocked(a *pvf.Archive, gen uint64) bool {
	return c.archive == a && c.indexGen == gen && c.indexStatus.State == IndexStateBuilding
}

func (c *core) readListPairs(a *pvf.Archive, gen uint64, ctx context.Context, index int32) ([]pvf.ListPair, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return nil, context.Canceled
	}
	return a.ScriptListPairs(index)
}

func (c *core) findArchiveEntry(a *pvf.Archive, gen uint64, ctx context.Context, name string) (int32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return 0, false
	}
	return a.Find(name)
}

func (c *core) readFileMetadata(a *pvf.Archive, gen uint64, ctx context.Context, index int32) (pvf.File, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return pvf.File{}, "", false
	}
	return a.File(index), a.Path(index), true
}

func (c *core) readScriptName(a *pvf.Archive, gen uint64, ctx context.Context, index int32) (string, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return "", false, context.Canceled
	}
	return a.ScriptName(index)
}

func (c *core) snapshotPaths(a *pvf.Archive, gen uint64, ctx context.Context) ([]pathEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return nil, false
	}
	paths := make([]pathEntry, len(c.sortedPaths))
	copy(paths, c.sortedPaths)
	return paths, true
}

func buildSearchRecords(paths []pathEntry, metadata []indexedMetadata, metadataByFile map[int32][]int) ([]searchRecord, map[int32][]int) {
	if paths == nil {
		paths = make([]pathEntry, 0, len(metadata))
		seen := make(map[int32]bool)
		for _, entry := range metadata {
			if seen[entry.fileIndex] {
				continue
			}
			seen[entry.fileIndex] = true
			paths = append(paths, pathEntry{
				path:  entry.path,
				lower: strings.ToLower(entry.path),
				idx:   entry.fileIndex,
				size:  entry.size,
				typ:   entry.dataType,
			})
		}
		sort.Slice(paths, func(i, j int) bool { return paths[i].path < paths[j].path })
	}

	records := make([]searchRecord, 0, len(paths)+len(metadata))
	recordsByFile := make(map[int32][]int)
	for _, p := range paths {
		metadataIndexes := metadataByFile[p.idx]
		if len(metadataIndexes) == 0 {
			appendSearchRecord(&records, &recordsByFile, SearchHit{
				Name:      pathBase(p.path),
				Path:      p.path,
				Category:  SearchCategoryFile,
				Size:      p.size,
				DataType:  p.typ,
				FileIndex: p.idx,
			})
			continue
		}
		for _, metadataIndex := range metadataIndexes {
			entry := metadata[metadataIndex]
			appendSearchRecord(&records, &recordsByFile, SearchHit{
				Name:      entry.name,
				ID:        entry.id,
				Path:      entry.path,
				Category:  entry.category,
				Size:      entry.size,
				DataType:  entry.dataType,
				FileIndex: entry.fileIndex,
			})
		}
	}
	return records, recordsByFile
}

func appendSearchRecord(records *[]searchRecord, recordsByFile *map[int32][]int, hit SearchHit) {
	record := searchRecord{
		hit:       hit,
		lowerName: strings.ToLower(hit.Name),
		lowerID:   strings.ToLower(hit.ID),
		lowerPath: strings.ToLower(hit.Path),
	}
	*records = append(*records, record)
	index := len(*records) - 1
	(*recordsByFile)[hit.FileIndex] = append((*recordsByFile)[hit.FileIndex], index)
}

func buildTreeTags(records []searchRecord, recordsByFile map[int32][]int) map[int32][]TreeTag {
	tagsByFile := make(map[int32][]TreeTag)
	for fileIndex, recordIndexes := range recordsByFile {
		tags := treeTagsForRecords(records, recordIndexes)
		if len(tags) > 0 {
			tagsByFile[fileIndex] = tags
		}
	}
	return tagsByFile
}

func treeTagsForRecords(records []searchRecord, recordIndexes []int) []TreeTag {
	var tags []TreeTag
	for _, recordIndex := range recordIndexes {
		if recordIndex < 0 || recordIndex >= len(records) {
			continue
		}
		hit := records[recordIndex].hit
		if hit.Category == SearchCategoryFile {
			continue
		}
		tags = append(tags, TreeTag{
			ID:       hit.ID,
			Name:     hit.Name,
			Category: hit.Category,
		})
	}
	return tags
}

func cloneTreeTags(tags []TreeTag) []TreeTag {
	if len(tags) == 0 {
		return nil
	}
	cloned := make([]TreeTag, len(tags))
	copy(cloned, tags)
	return cloned
}

func resolveListPath(listPath, relative string) (string, bool) {
	relative = strings.TrimSpace(strings.ReplaceAll(relative, "\\", "/"))
	if relative == "" || strings.HasPrefix(relative, "/") {
		return "", false
	}
	resolved := path.Clean(path.Join(path.Dir(listPath), relative))
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", false
	}
	return resolved, true
}

func pathBase(p string) string {
	if slash := strings.LastIndexByte(p, '/'); slash >= 0 {
		return p[slash+1:]
	}
	return p
}
