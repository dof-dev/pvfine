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

const (
	itemShopListPath = "itemshop/itemshop.lst"
	npcListPath      = "npc/npc.lst"
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
	ChangeKind      string                      `json:"changeKind,omitempty"`
	Annotations     []TreeAnnotation            `json:"annotations,omitempty"`
	PathAnnotations map[string][]TreeAnnotation `json:"pathAnnotations,omitempty"`
	Icon            *ImageReference             `json:"icon,omitempty"`
	FieldImage      *ImageReference             `json:"fieldImage,omitempty"`
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
	name       string
	id         string
	category   string
	listPath   string
	path       string
	fileIndex  int32
	size       int32
	dataType   int32
	icon       *ImageReference
	fieldImage *ImageReference
}

type fileVisuals struct {
	icon       *ImageReference
	fieldImage *ImageReference
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

	specs := c.searchableListSpecs()
	for _, spec := range specs {
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
			target, ok := c.findListTarget(a, gen, ctx, spec.listPath, pair.Path)
			if !ok {
				skipped++
				continue
			}
			refs = append(refs, indexedMetadata{
				id:       pair.ID,
				category: spec.category,
				listPath: spec.listPath,
				path:     target,
			})
		}
	}

	npcNames := make(map[string]string)
	for _, spec := range specs {
		if !sameSearchPath(spec.listPath, itemShopListPath) {
			continue
		}
		var ok bool
		npcNames, ok = c.buildNPCNameIndex(a, gen, ctx)
		if !ok {
			return
		}
		break
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
	metadataByIndex := make(map[int32]pvf.ScriptMetadata)
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
		scriptMetadata, metadataCached := metadataByIndex[fileIndex]
		var metadataErr error
		if !metadataCached {
			scriptMetadata, metadataErr = c.readIndexedMetadata(a, gen, ctx, fileIndex, ref.listPath, npcNames)
			if metadataErr == nil {
				metadataByIndex[fileIndex] = scriptMetadata
			}
		}
		if metadataErr != nil {
			// The id/path mapping remains useful even when the target is not
			// a parseable script or has malformed content.
			scriptMetadata = pvf.ScriptMetadata{}
		}
		ref.name = scriptMetadata.Name
		ref.icon = imageReferenceFromPVF(scriptMetadata.Icon)
		ref.fieldImage = imageReferenceFromPVF(scriptMetadata.FieldImage)
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
	visualsByFile := make(map[int32]fileVisuals, len(metadataByIndex))
	for fileIndex, scriptMetadata := range metadataByIndex {
		visualsByFile[fileIndex] = fileVisuals{
			icon:       imageReferenceFromPVF(scriptMetadata.Icon),
			fieldImage: imageReferenceFromPVF(scriptMetadata.FieldImage),
		}
	}

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
		if isNPCEntryPath(a.Path(fileIndex)) {
			npcNames = buildNPCNameIndexFromArchive(a)
			break
		}
	}
	for fileIndex := range c.indexDirty {
		scriptMetadata, err := readIndexedMetadataFromArchive(a, fileIndex, a.Path(fileIndex), npcNames)
		if err != nil {
			scriptMetadata = pvf.ScriptMetadata{}
		}
		name := scriptMetadata.Name
		visualsByFile[fileIndex] = fileVisuals{
			icon:       imageReferenceFromPVF(scriptMetadata.Icon),
			fieldImage: imageReferenceFromPVF(scriptMetadata.FieldImage),
		}
		for _, recordIndex := range recordsByFile[fileIndex] {
			if records[recordIndex].hit.Category == SearchCategoryFile {
				continue
			}
			records[recordIndex].hit.Name = name
			records[recordIndex].lowerName = strings.ToLower(name)
			records[recordIndex].hit.Icon = cloneImageReference(visualsByFile[fileIndex].icon)
			records[recordIndex].hit.FieldImage = cloneImageReference(visualsByFile[fileIndex].fieldImage)
		}
	}
	c.searchRecords = records
	c.searchByFile = recordsByFile
	c.treeTagsByFile = treeTagsByFile
	c.visualsByFile = visualsByFile
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
	previousVisuals, hadPreviousVisuals := c.visualsByFile[index]
	delete(c.visualsByFile, index)
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
	metadata, err := readIndexedMetadataFromArchive(c.archive, index, c.archive.Path(index), nil)
	if err != nil {
		metadata = pvf.ScriptMetadata{}
	}
	name := metadata.Name
	visuals := fileVisuals{icon: imageReferenceFromPVF(metadata.Icon), fieldImage: imageReferenceFromPVF(metadata.FieldImage)}
	c.visualsByFile[index] = visuals
	updated := false
	for _, recordIndex := range recordIndexes {
		record := &c.searchRecords[recordIndex]
		if record.hit.Category != SearchCategoryFile {
			record.hit.Name = name
			record.lowerName = strings.ToLower(name)
		}
		if !imageReferencesEqual(record.hit.Icon, visuals.icon) || !imageReferencesEqual(record.hit.FieldImage, visuals.fieldImage) {
			updated = true
		}
		record.hit.Icon = cloneImageReference(visuals.icon)
		record.hit.FieldImage = cloneImageReference(visuals.fieldImage)
		if record.hit.Category != SearchCategoryFile {
			updated = true
		}
	}
	c.treeTagsByFile[index] = treeTagsForRecords(c.searchRecords, recordIndexes)
	if !hadPreviousVisuals || !imageReferencesEqual(previousVisuals.icon, visuals.icon) || !imageReferencesEqual(previousVisuals.fieldImage, visuals.fieldImage) {
		if visuals.icon != nil || visuals.fieldImage != nil || hadPreviousVisuals {
			updated = true
		}
	}
	if isNPCEntryPath(path) && c.refreshItemShopNamesLocked() {
		updated = true
	}
	c.mu.Unlock()
	emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
	if versioned {
		emitVersionState(c, "edited")
	}
	if updated {
		emitEvent("archive:index-updated", map[string]any{
			"fileIndex":  index,
			"name":       name,
			"icon":       cloneImageReference(visuals.icon),
			"fieldImage": cloneImageReference(visuals.fieldImage),
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

func (c *core) findListTarget(a *pvf.Archive, gen uint64, ctx context.Context, listPath, relativePath string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return "", false
	}
	targetPath, _, ok := findListTargetInArchive(a, listPath, relativePath)
	return targetPath, ok
}

func (c *core) readFileMetadata(a *pvf.Archive, gen uint64, ctx context.Context, index int32) (pvf.File, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return pvf.File{}, "", false
	}
	return a.File(index), a.Path(index), true
}

func (c *core) readIndexedMetadata(a *pvf.Archive, gen uint64, ctx context.Context, index int32, listPath string, npcNames map[string]string) (pvf.ScriptMetadata, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return pvf.ScriptMetadata{}, context.Canceled
	}
	return readIndexedMetadataFromArchive(a, index, listPath, npcNames)
}

func (c *core) readIndexedName(a *pvf.Archive, gen uint64, ctx context.Context, index int32, listPath string, npcNames map[string]string) (string, bool, error) {
	metadata, err := c.readIndexedMetadata(a, gen, ctx, index, listPath, npcNames)
	return metadata.Name, metadata.HasName, err
}

func (c *core) buildNPCNameIndex(a *pvf.Archive, gen uint64, ctx context.Context) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return nil, false
	}
	return buildNPCNameIndexFromArchive(a), true
}

func readIndexedNameFromArchive(a *pvf.Archive, index int32, listPath string, npcNames map[string]string) (string, bool, error) {
	metadata, err := readIndexedMetadataFromArchive(a, index, listPath, npcNames)
	return metadata.Name, metadata.HasName, err
}

func readIndexedMetadataFromArchive(a *pvf.Archive, index int32, listPath string, npcNames map[string]string) (pvf.ScriptMetadata, error) {
	metadata, err := a.ScriptMetadata(index)
	if err != nil {
		return pvf.ScriptMetadata{}, err
	}
	if metadata.HasName && strings.TrimSpace(metadata.Name) != "" {
		return metadata, nil
	}
	if !isItemShopEntry(listPath, a.Path(index)) {
		return metadata, nil
	}
	if npcNames == nil {
		npcNames = buildNPCNameIndexFromArchive(a)
	}
	text, err := a.Text(index)
	if err != nil {
		return metadata, err
	}
	npcID := firstSectionValue(text, "npc")
	if npcID == "" {
		return metadata, nil
	}
	name, ok := npcNames[npcID]
	if ok && strings.TrimSpace(name) != "" {
		metadata.Name = name
		metadata.HasName = true
	}
	return metadata, nil
}

func imageReferenceFromPVF(reference *pvf.ScriptImageReference) *ImageReference {
	if reference == nil || strings.TrimSpace(reference.Path) == "" || reference.Index < 0 {
		return nil
	}
	return &ImageReference{Path: reference.Path, Index: reference.Index}
}

func cloneImageReference(reference *ImageReference) *ImageReference {
	if reference == nil {
		return nil
	}
	copy := *reference
	return &copy
}

func imageReferencesEqual(left, right *ImageReference) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Path == right.Path && left.Index == right.Index
}

func (c *core) fileVisualsLocked(index int32) fileVisuals {
	if c.visualsByFile == nil {
		c.visualsByFile = make(map[int32]fileVisuals)
	}
	if visuals, ok := c.visualsByFile[index]; ok {
		return fileVisuals{icon: cloneImageReference(visuals.icon), fieldImage: cloneImageReference(visuals.fieldImage)}
	}
	if c.archive == nil || index < 0 || index >= c.archive.FileCount() || c.archive.File(index).DataType != pvf.TypeScript {
		return fileVisuals{}
	}
	metadata, err := c.archive.ScriptMetadata(index)
	if err != nil {
		return fileVisuals{}
	}
	visuals := fileVisuals{icon: imageReferenceFromPVF(metadata.Icon), fieldImage: imageReferenceFromPVF(metadata.FieldImage)}
	c.visualsByFile[index] = visuals
	return fileVisuals{icon: cloneImageReference(visuals.icon), fieldImage: cloneImageReference(visuals.fieldImage)}
}

func buildNPCNameIndexFromArchive(a *pvf.Archive) map[string]string {
	result := make(map[string]string)
	listIndex, ok := a.Find(npcListPath)
	if !ok {
		return result
	}
	pairs, err := a.ScriptListPairs(listIndex)
	if err != nil {
		return result
	}
	for _, pair := range pairs {
		_, targetIndex, ok := findListTargetInArchive(a, npcListPath, pair.Path)
		if !ok {
			continue
		}
		name, ok, err := a.ScriptName(targetIndex)
		if err != nil || !ok || strings.TrimSpace(name) == "" {
			continue
		}
		if _, exists := result[pair.ID]; !exists {
			result[pair.ID] = name
		}
	}
	return result
}

func isItemShopEntry(listPath, entryPath string) bool {
	if sameSearchPath(listPath, itemShopListPath) {
		return true
	}
	entryPath = normalizeSearchPath(entryPath)
	return strings.HasPrefix(entryPath, "itemshop/") && strings.EqualFold(path.Ext(entryPath), ".shp")
}

func isNPCEntryPath(entryPath string) bool {
	entryPath = normalizeSearchPath(entryPath)
	return entryPath != npcListPath && strings.HasPrefix(entryPath, "npc/")
}

func (c *core) refreshItemShopNamesLocked() bool {
	npcNames := buildNPCNameIndexFromArchive(c.archive)
	changed := false
	affectedFiles := make(map[int32]struct{})
	for index := range c.searchRecords {
		record := &c.searchRecords[index]
		if record.hit.Category == SearchCategoryFile || !isItemShopEntry("", record.hit.Path) {
			continue
		}
		name, _, err := readIndexedNameFromArchive(c.archive, record.hit.FileIndex, itemShopListPath, npcNames)
		if err != nil {
			name = ""
		}
		if record.hit.Name == name {
			continue
		}
		record.hit.Name = name
		record.lowerName = strings.ToLower(name)
		affectedFiles[record.hit.FileIndex] = struct{}{}
		changed = true
	}
	for fileIndex := range affectedFiles {
		c.treeTagsByFile[fileIndex] = treeTagsForRecords(c.searchRecords, c.searchByFile[fileIndex])
	}
	return changed
}

func sameSearchPath(left, right string) bool {
	return normalizeSearchPath(left) == normalizeSearchPath(right)
}

func normalizeSearchPath(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/"))
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
				Name:       pathBase(p.path),
				Path:       p.path,
				Category:   SearchCategoryFile,
				Size:       p.size,
				DataType:   p.typ,
				FileIndex:  p.idx,
				ChangeKind: p.changeKind,
			})
			continue
		}
		for _, metadataIndex := range metadataIndexes {
			entry := metadata[metadataIndex]
			appendSearchRecord(&records, &recordsByFile, SearchHit{
				Name:       entry.name,
				ID:         entry.id,
				Path:       entry.path,
				Category:   entry.category,
				Size:       entry.size,
				DataType:   entry.dataType,
				FileIndex:  entry.fileIndex,
				ChangeKind: p.changeKind,
				Icon:       cloneImageReference(entry.icon),
				FieldImage: cloneImageReference(entry.fieldImage),
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

func findListTargetInArchive(a *pvf.Archive, listPath, relative string) (string, int32, bool) {
	candidates, ok := listPathCandidates(listPath, relative)
	if !ok {
		return "", 0, false
	}
	for _, candidate := range candidates {
		index, ok := a.Find(candidate)
		if ok {
			return a.Path(index), index, true
		}
	}
	return "", 0, false
}

func listPathCandidates(listPath, relative string) ([]string, bool) {
	targetPath, ok := resolveListPath(listPath, relative)
	if !ok {
		return nil, false
	}
	candidates := []string{targetPath}
	base := path.Base(targetPath)
	if !strings.HasPrefix(strings.ToLower(base), "(r)") {
		candidates = append(candidates, path.Join(path.Dir(targetPath), "(r)"+base))
	}
	return candidates, true
}

func pathBase(p string) string {
	if slash := strings.LastIndexByte(p, '/'); slash >= 0 {
		return p[slash+1:]
	}
	return p
}
