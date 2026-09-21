package services

import (
	"context"
	"fmt"
	"log"
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
	// Refreshing means an older ready snapshot is still serving queries while
	// a newer candidate is being built in the background.
	Refreshing      bool    `json:"refreshing"`
	RefreshError    string  `json:"refreshError"`
	CacheHit        bool    `json:"cacheHit"`
	OpenDurationMs  float64 `json:"openDurationMs"`
	BuildDurationMs float64 `json:"buildDurationMs"`
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.searchableListSpecsLocked()
}

func (c *core) searchableListSpecsLocked() []searchableListSpec {
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
	c.startSearchIndexWithOptions(false)
}

func (c *core) startSearchIndexForced() {
	c.startSearchIndexWithOptions(true)
}

func (c *core) startSearchIndexWithOptions(force bool) {
	lockStartedAt := time.Now()
	c.mu.Lock()
	if waited := time.Since(lockStartedAt).Round(time.Millisecond); waited > 0 {
		log.Printf("[pvfine:index] waited for core lock: elapsed=%s", waited)
	}
	if c.archive == nil {
		c.mu.Unlock()
		log.Printf("[pvfine:index] index request ignored: no archive")
		return
	}
	dirty := c.indexDirty != nil && len(c.indexDirty) > 0
	if c.diskIndex != nil {
		dirty = c.diskIndex.dirty
	}
	log.Printf("[pvfine:index] index request: force=%t files=%d disk=%t ready=%t dirty=%t", force, c.archive.FileCount(), c.diskIndex != nil, c.indexStatus.State == IndexStateReady, dirty)
	if c.indexCancel != nil {
		c.indexCancel()
	}
	c.indexGen++
	gen := c.indexGen
	a := c.archive
	startedAt := time.Now()
	if c.diskIndex != nil {
		index := c.diskIndex
		hasSnapshot := index.ready && !force && !index.dirty
		index.ready = hasSnapshot
		ctx, cancel := context.WithCancel(context.Background())
		c.indexCancel = cancel
		c.indexGen++
		gen := c.indexGen
		c.indexStartedAt = startedAt
		if hasSnapshot {
			log.Printf("[pvfine:index] cache hit: files=%d elapsed=%s", a.FileCount(), time.Since(startedAt).Round(time.Millisecond))
			c.indexStatus = IndexStatus{State: IndexStateReady, Stage: "ready-cache", Done: int(a.FileCount()), Total: int(a.FileCount()), CacheHit: true, OpenDurationMs: c.indexStatus.OpenDurationMs}
			status := c.indexStatus
			c.indexCancel = nil
			c.mu.Unlock()
			cancel()
			emitEvent("archive:index-ready", status)
			return
		}
		c.indexStatus = IndexStatus{State: IndexStateBuilding, Stage: "sqlite", OpenDurationMs: c.indexStatus.OpenDurationMs}
		status := c.indexStatus
		c.mu.Unlock()
		finishTask := c.archiveTasks.begin()
		if !c.indexCurrent(a, gen, ctx) {
			finishTask()
			return
		}
		emitEvent("archive:index-progress", status)
		go func() {
			defer finishTask()
			c.buildSearchIndexSQLite(ctx, gen, a, index, startedAt, force)
		}()
		return
	}
	openDurationMs := c.indexStatus.OpenDurationMs
	hasSnapshot := c.indexStatus.State == IndexStateReady && c.searchRecords != nil
	pendingDirty := c.indexDirty
	cacheEligible := a.SourcePath() != "" && !a.Modified()
	delta := !force && hasSnapshot && (c.searchIndexDeltaPending || len(c.searchIndexListPending) > 0)
	listIndexes := make([]int32, 0, len(c.searchIndexListPending))
	for index := range c.searchIndexListPending {
		listIndexes = append(listIndexes, index)
	}
	c.searchIndexDeltaPending = false
	c.searchIndexListPending = nil
	ctx, cancel := context.WithCancel(context.Background())
	c.indexCancel = cancel
	if !hasSnapshot || pendingDirty == nil {
		c.indexDirty = make(map[int32]struct{})
	}
	c.indexStartedAt = startedAt
	state := IndexStateBuilding
	stage := "preparing"
	if hasSnapshot {
		state = IndexStateReady
		stage = "refreshing"
	}
	c.indexStatus = IndexStatus{
		State:          state,
		Stage:          stage,
		OpenDurationMs: openDurationMs,
		Refreshing:     hasSnapshot,
	}
	status := c.indexStatus
	c.mu.Unlock()

	finishTask := c.archiveTasks.begin()
	if !c.indexCurrent(a, gen, ctx) {
		finishTask()
		return
	}
	emitEvent("archive:index-progress", status)
	if delta {
		go func() {
			defer finishTask()
			c.buildSearchIndexDelta(ctx, gen, a, startedAt, listIndexes)
		}()
		return
	}
	go func() {
		defer finishTask()
		c.buildSearchIndex(ctx, gen, a, startedAt, force, cacheEligible)
	}()
}

func (c *core) buildSearchIndexSQLite(ctx context.Context, gen uint64, a *pvf.Archive, index *sqliteArchiveIndex, startedAt time.Time, force bool) {
	specs := c.searchableListSpecs()
	log.Printf("[pvfine:index] semantic build started: force=%t generation=%d files=%d specs=%d", force, gen, a.FileCount(), len(specs))
	total, skipped, err := index.buildSemantic(ctx, c, a, specs, gen)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[pvfine:index] semantic build cancelled: generation=%d elapsed=%s", gen, time.Since(startedAt).Round(time.Millisecond))
			return
		}
		log.Printf("[pvfine:index] semantic build failed: generation=%d elapsed=%s error=%v", gen, time.Since(startedAt).Round(time.Millisecond), err)
		c.mu.Lock()
		if c.indexIsCurrentLocked(a, gen) {
			c.indexStatus = IndexStatus{State: IndexStateError, Stage: "error", Error: err.Error(), OpenDurationMs: c.indexStatus.OpenDurationMs, BuildDurationMs: elapsedMilliseconds(startedAt)}
			c.indexCancel = nil
			status := c.indexStatus
			c.mu.Unlock()
			emitEvent("archive:index-error", status)
			return
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if !c.indexIsCurrentLocked(a, gen) || c.diskIndex != index || ctx.Err() != nil {
		c.mu.Unlock()
		log.Printf("[pvfine:index] semantic build discarded: generation=%d elapsed=%s", gen, time.Since(startedAt).Round(time.Millisecond))
		return
	}
	index.ready = true
	index.dirty = false
	c.indexStatus = IndexStatus{State: IndexStateReady, Stage: "ready-sqlite", Done: total, Total: total, Skipped: skipped, OpenDurationMs: c.indexStatus.OpenDurationMs, BuildDurationMs: elapsedMilliseconds(startedAt)}
	c.indexCancel = nil
	status := c.indexStatus
	c.mu.Unlock()
	log.Printf("[pvfine:index] semantic build finished: generation=%d records=%d skipped=%d elapsed=%s", gen, total, skipped, time.Since(startedAt).Round(time.Millisecond))
	emitEvent("archive:index-ready", status)
}

func (c *core) buildSearchIndex(ctx context.Context, gen uint64, a *pvf.Archive, startedAt time.Time, force, cacheEligible bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.failSearchIndex(a, gen, ctx, startedAt, fmt.Errorf("搜索索引构建失败: %v", recovered))
		}
	}()

	paths, ok := c.snapshotPaths(a, gen, ctx)
	if !ok {
		return
	}

	specs := c.searchableListSpecs()
	if !force && cacheEligible {
		c.mu.RLock()
		cached, err := loadSearchIndexCache(a, specs, c.searchIndexCachePath)
		c.mu.RUnlock()
		if err == nil {
			metadata := cached.Metadata
			byFile := make(map[int32][]int)
			visuals := make(map[int32]fileVisuals)
			for index := range metadata {
				value := &metadata[index]
				byFile[value.fileIndex] = append(byFile[value.fileIndex], index)
				visuals[value.fileIndex] = fileVisuals{
					icon:       cloneImageReference(value.icon),
					fieldImage: cloneImageReference(value.fieldImage),
				}
			}
			records, recordsByFile := buildSearchRecords(paths, metadata, byFile)
			treeTagsByFile := buildTreeTags(records, recordsByFile)
			if c.publishSearchCandidate(a, gen, ctx, startedAt, records, recordsByFile, metadata, treeTagsByFile, visuals, cached.Total, cached.Skipped, cached.SpecsFingerprint, false, true) {
				return
			}
			return
		}
	}

	refs := make([]indexedMetadata, 0)
	skipped := 0
	for _, spec := range specs {
		if !c.indexCurrent(a, gen, ctx) {
			return
		}
		listIndex, ok := c.findArchiveList(a, gen, ctx, spec.listPath)
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

	if !c.publishSearchCandidate(a, gen, ctx, startedAt, records, recordsByFile, metadata, treeTagsByFile, visualsByFile, len(refs), skipped, searchIndexSpecFingerprint(specs), cacheEligible, false) {
		return
	}
}

// startSearchIndexForList schedules a local semantic refresh for one archive
// list. If there is no ready snapshot yet, startSearchIndex naturally falls
// back to the initial full build.
func (c *core) startSearchIndexForList(listIndex int32) {
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return
	}
	if c.searchIndexListPending == nil {
		c.searchIndexListPending = make(map[int32]struct{})
	}
	c.searchIndexListPending[listIndex] = struct{}{}
	c.mu.Unlock()
	c.startSearchIndex()
}

func (c *core) buildSearchIndexDelta(ctx context.Context, gen uint64, a *pvf.Archive, startedAt time.Time, listIndexes []int32) {
	paths, ok := c.snapshotPaths(a, gen, ctx)
	if !ok {
		return
	}
	c.mu.RLock()
	metadata := cloneIndexedMetadata(c.searchMetadata)
	oldSkipped := c.indexStatus.Skipped
	dirtyIndexes := make([]int32, 0, len(c.indexDirty))
	for index := range c.indexDirty {
		dirtyIndexes = append(dirtyIndexes, index)
	}
	c.mu.RUnlock()

	specs := c.searchableListSpecs()
	affectedLists := make(map[int32]struct{}, len(listIndexes))
	for _, listIndex := range listIndexes {
		affectedLists[listIndex] = struct{}{}
	}

	// A structural delta keeps existing list registrations and only rebinds
	// their paths/file indexes against the new file table. A list delta replaces
	// the rows for the affected relation specs below.
	if len(affectedLists) == 0 {
		filtered := make([]indexedMetadata, 0, len(metadata))
		for _, value := range metadata {
			if !c.indexCurrent(a, gen, ctx) {
				return
			}
			fileIndex, found := c.findArchiveEntry(a, gen, ctx, value.path)
			if !found {
				continue
			}
			file, canonicalPath, found := c.readFileMetadata(a, gen, ctx, fileIndex)
			if !found {
				continue
			}
			value.fileIndex = fileIndex
			value.path = canonicalPath
			value.size = file.DataSize
			value.dataType = file.DataType
			filtered = append(filtered, value)
		}
		metadata = filtered
	} else {
		kept := make([]indexedMetadata, 0, len(metadata))
		for _, value := range metadata {
			listIndex, found := c.findArchiveList(a, gen, ctx, value.listPath)
			if found {
				if _, affected := affectedLists[listIndex]; affected {
					continue
				}
			}
			fileIndex, found := c.findArchiveEntry(a, gen, ctx, value.path)
			if !found {
				continue
			}
			file, canonicalPath, found := c.readFileMetadata(a, gen, ctx, fileIndex)
			if !found {
				continue
			}
			value.fileIndex = fileIndex
			value.path = canonicalPath
			value.size = file.DataSize
			value.dataType = file.DataType
			kept = append(kept, value)
		}
		metadata = kept

		npcNames := map[string]string(nil)
		for _, spec := range specs {
			listIndex, found := c.findArchiveList(a, gen, ctx, spec.listPath)
			if !found {
				continue
			}
			if _, affected := affectedLists[listIndex]; !affected {
				continue
			}
			if sameSearchPath(spec.listPath, itemShopListPath) {
				var namesOK bool
				npcNames, namesOK = c.buildNPCNameIndex(a, gen, ctx)
				if !namesOK {
					return
				}
			}
			pairs, err := c.readListPairs(a, gen, ctx, listIndex)
			if err != nil {
				oldSkipped++
				continue
			}
			metadataByPath := make(map[string]pvf.ScriptMetadata)
			for _, pair := range pairs {
				targetPath, targetIndex, file, found := c.findListTargetFile(a, gen, ctx, spec.listPath, pair.Path)
				if !found {
					oldSkipped++
					continue
				}
				scriptMetadata, cached := metadataByPath[targetPath]
				if !cached {
					var metadataErr error
					scriptMetadata, metadataErr = c.readIndexedMetadata(a, gen, ctx, targetIndex, spec.listPath, npcNames)
					if metadataErr != nil {
						scriptMetadata = pvf.ScriptMetadata{}
					}
					metadataByPath[targetPath] = scriptMetadata
				}
				metadata = append(metadata, indexedMetadata{
					name:       scriptMetadata.Name,
					id:         pair.ID,
					category:   spec.category,
					listPath:   spec.listPath,
					path:       targetPath,
					fileIndex:  targetIndex,
					size:       file.DataSize,
					dataType:   file.DataType,
					icon:       imageReferenceFromPVF(scriptMetadata.Icon),
					fieldImage: imageReferenceFromPVF(scriptMetadata.FieldImage),
				})
			}
		}
	}

	// Payload-only edits keep the record set but need fresh display metadata.
	// Read each changed target once and apply it to every list registration of
	// that target. The expensive work is intentionally outside the request that
	// staged the edit.
	for _, dirtyIndex := range dirtyIndexes {
		file, canonicalPath, ok := c.readFileMetadata(a, gen, ctx, dirtyIndex)
		if !ok {
			continue
		}
		listPath := ""
		for _, value := range metadata {
			if value.fileIndex == dirtyIndex || value.path == canonicalPath {
				listPath = value.listPath
				break
			}
		}
		if listPath == "" {
			for index := range paths {
				if paths[index].idx == dirtyIndex {
					paths[index].size = file.DataSize
					paths[index].typ = file.DataType
				}
			}
			continue
		}
		scriptMetadata, err := c.readIndexedMetadata(a, gen, ctx, dirtyIndex, listPath, nil)
		if err != nil {
			scriptMetadata = pvf.ScriptMetadata{}
		}
		visuals := fileVisuals{
			icon:       imageReferenceFromPVF(scriptMetadata.Icon),
			fieldImage: imageReferenceFromPVF(scriptMetadata.FieldImage),
		}
		for index := range metadata {
			if metadata[index].fileIndex != dirtyIndex && metadata[index].path != canonicalPath {
				continue
			}
			metadata[index].path = canonicalPath
			metadata[index].fileIndex = dirtyIndex
			metadata[index].size = file.DataSize
			metadata[index].dataType = file.DataType
			metadata[index].name = scriptMetadata.Name
			metadata[index].icon = cloneImageReference(visuals.icon)
			metadata[index].fieldImage = cloneImageReference(visuals.fieldImage)
		}
		for index := range paths {
			if paths[index].idx == dirtyIndex {
				paths[index].size = file.DataSize
				paths[index].typ = file.DataType
			}
		}
	}

	byFile := make(map[int32][]int)
	visuals := make(map[int32]fileVisuals)
	for index := range metadata {
		value := &metadata[index]
		byFile[value.fileIndex] = append(byFile[value.fileIndex], index)
		visuals[value.fileIndex] = fileVisuals{
			icon:       cloneImageReference(value.icon),
			fieldImage: cloneImageReference(value.fieldImage),
		}
	}
	records, recordsByFile := buildSearchRecords(paths, metadata, byFile)
	treeTagsByFile := buildTreeTags(records, recordsByFile)
	total := len(metadata) + oldSkipped
	if !c.publishSearchCandidate(a, gen, ctx, startedAt, records, recordsByFile, metadata, treeTagsByFile, visuals, total, oldSkipped, searchIndexSpecFingerprint(specs), false, false) {
		return
	}
}

func cloneIndexedMetadata(values []indexedMetadata) []indexedMetadata {
	if len(values) == 0 {
		return nil
	}
	result := make([]indexedMetadata, len(values))
	for index, value := range values {
		result[index] = value
		result[index].icon = cloneImageReference(value.icon)
		result[index].fieldImage = cloneImageReference(value.fieldImage)
	}
	return result
}

// publishSearchCandidate atomically swaps a completed candidate into the
// currently active snapshot. A ready snapshot remains queryable until this
// point, so a slow refresh never turns the normal UI flow into a loading gate.
func (c *core) publishSearchCandidate(
	a *pvf.Archive,
	gen uint64,
	ctx context.Context,
	startedAt time.Time,
	records []searchRecord,
	recordsByFile map[int32][]int,
	metadata []indexedMetadata,
	treeTagsByFile map[int32][]TreeTag,
	visualsByFile map[int32]fileVisuals,
	total, skipped int,
	specsFingerprint string,
	cacheEligible bool,
	cacheHit bool,
) bool {
	c.mu.Lock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		c.mu.Unlock()
		return false
	}

	// A payload edit may land while the candidate is being built. Refreshing
	// those files here is cheap and prevents the first published snapshot from
	// exposing the text that existed when the background scan started.
	for fileIndex := range c.indexDirty {
		scriptMetadata, err := readIndexedMetadataFromArchive(a, fileIndex, a.Path(fileIndex), nil)
		if err != nil {
			scriptMetadata = pvf.ScriptMetadata{}
		}
		visuals := fileVisuals{
			icon:       imageReferenceFromPVF(scriptMetadata.Icon),
			fieldImage: imageReferenceFromPVF(scriptMetadata.FieldImage),
		}
		visualsByFile[fileIndex] = visuals
		for metadataIndex := range metadata {
			if metadata[metadataIndex].fileIndex != fileIndex {
				continue
			}
			metadata[metadataIndex].name = scriptMetadata.Name
			metadata[metadataIndex].size = a.File(fileIndex).DataSize
			metadata[metadataIndex].dataType = a.File(fileIndex).DataType
			metadata[metadataIndex].icon = cloneImageReference(visuals.icon)
			metadata[metadataIndex].fieldImage = cloneImageReference(visuals.fieldImage)
		}
		for _, recordIndex := range recordsByFile[fileIndex] {
			if recordIndex < 0 || recordIndex >= len(records) {
				continue
			}
			record := &records[recordIndex]
			record.hit.Size = a.File(fileIndex).DataSize
			record.hit.DataType = a.File(fileIndex).DataType
			if record.hit.Category == SearchCategoryFile {
				continue
			}
			record.hit.Name = scriptMetadata.Name
			record.lowerName = strings.ToLower(scriptMetadata.Name)
			record.hit.Icon = cloneImageReference(visuals.icon)
			record.hit.FieldImage = cloneImageReference(visuals.fieldImage)
		}
	}

	c.searchRecords = records
	c.searchByFile = recordsByFile
	c.searchMetadata = cloneIndexedMetadata(metadata)
	c.searchSpecFingerprint = specsFingerprint
	c.treeTagsByFile = treeTagsByFile
	c.visualsByFile = visualsByFile
	openDurationMs := c.indexStatus.OpenDurationMs
	c.indexStatus = IndexStatus{
		State:           IndexStateReady,
		Stage:           "ready",
		Done:            total,
		Total:           total,
		Skipped:         skipped,
		CacheHit:        cacheHit,
		OpenDurationMs:  openDurationMs,
		BuildDurationMs: elapsedMilliseconds(startedAt),
	}
	if cacheHit {
		c.indexStatus.Stage = "ready-cache"
	}
	c.indexDirty = make(map[int32]struct{})
	c.indexCancel = nil
	status := c.indexStatus
	c.mu.Unlock()

	emitEvent("archive:index-ready", status)
	if cacheEligible && !cacheHit {
		c.persistSearchIndexCacheAsync(a, gen, metadata, total, skipped)
	}
	return true
}

func (c *core) failSearchIndex(a *pvf.Archive, gen uint64, ctx context.Context, startedAt time.Time, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	if !c.indexIsCurrentLocked(a, gen) {
		c.mu.Unlock()
		return
	}
	if c.indexStatus.State == IndexStateReady && c.searchRecords != nil {
		c.indexStatus.Refreshing = false
		c.indexStatus.Stage = "refresh-error"
		c.indexStatus.RefreshError = err.Error()
		c.indexStatus.BuildDurationMs = elapsedMilliseconds(startedAt)
	} else {
		c.indexStatus = IndexStatus{
			State:           IndexStateError,
			Stage:           "error",
			Error:           err.Error(),
			OpenDurationMs:  c.indexStatus.OpenDurationMs,
			BuildDurationMs: elapsedMilliseconds(startedAt),
		}
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
	c.invalidateScriptLocked()
	versioned := c.versionRepo != nil
	if c.editorText == nil {
		c.editorText = make(map[int32]string)
	}
	c.editorText[index] = text
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorAnnotation = editorAnnotationCache{}
	c.invalidateAdvancedSearchLocked()
	if c.diskIndex != nil {
		if err := c.diskIndex.refreshFileMetadata(c.archive, map[int32]struct{}{index: {}}); err != nil {
			c.mu.Unlock()
			return false, "", err
		}
		c.diskIndex.ready = false
		c.diskIndex.dirty = true
		c.mu.Unlock()
		c.startSearchIndexForced()
		emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
		if versioned {
			emitVersionState(c, "edited")
		}
		return false, "", nil
	}
	delete(c.visualsByFile, index)
	ready := c.indexStatus.State == IndexStateReady && c.searchRecords != nil
	if !ready {
		c.queueSearchIndexMutationLocked(index)
		c.mu.Unlock()
		emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
		if versioned {
			emitVersionState(c, "edited")
		}
		return false, "", nil
	}
	listIndex, fullRefresh := c.queueSearchIndexMutationLocked(index)
	c.mu.Unlock()
	if fullRefresh {
		c.startSearchIndexForced()
	} else if listIndex >= 0 {
		c.startSearchIndexForList(listIndex)
	} else {
		c.startSearchIndex()
	}
	emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
	if versioned {
		emitVersionState(c, "edited")
	}
	return false, "", nil
}

// searchMutationClassLocked classifies a payload edit without reading the
// file. List edits can be refreshed locally; shared string/NPC dependencies
// conservatively use a forced full candidate build.
func (c *core) searchMutationClassLocked(index int32) (listIndex int32, full bool) {
	listIndex = -1
	if c.archive == nil || index < 0 || index >= c.archive.FileCount() {
		return listIndex, true
	}
	filePath := c.archive.Path(index)
	lowerPath := strings.ToLower(strings.Trim(strings.ReplaceAll(filePath, "\\", "/"), "/"))
	if strings.HasSuffix(lowerPath, ".str") || isNPCEntryPath(filePath) || lowerPath == npcListPath {
		return listIndex, true
	}
	for _, spec := range c.searchableListSpecsLocked() {
		candidate, ok := c.archive.FindList(spec.listPath)
		if ok && candidate == index {
			return candidate, false
		}
	}
	return listIndex, false
}

func (c *core) queueSearchIndexMutationLocked(index int32) (listIndex int32, full bool) {
	if c.indexDirty == nil {
		c.indexDirty = make(map[int32]struct{})
	}
	c.indexDirty[index] = struct{}{}
	listIndex, full = c.searchMutationClassLocked(index)
	if listIndex >= 0 {
		if c.searchIndexListPending == nil {
			c.searchIndexListPending = make(map[int32]struct{})
		}
		c.searchIndexListPending[listIndex] = struct{}{}
	}
	c.searchIndexDeltaPending = true
	return listIndex, full
}

// setPlaceholderText rewrites the string-table text behind one `<table::key>`
// placeholder and refreshes the name of the file that carries it, so the
// explorer and the search results pick the new text up immediately.
//
// The script that holds the placeholder is untouched: only the `.str` payload
// changes, which is where this client keeps the display text.
func (c *core) setPlaceholderText(index int32, tableIndex int32, key, text string) error {
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return ErrNoArchive
	}
	if err := c.ensureVersionReadyLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	if index < 0 || index >= c.archive.FileCount() {
		err := fmt.Errorf("文件索引越界: %d", index)
		c.mu.Unlock()
		return err
	}
	tableFileIndex, ok := c.archive.StringTableEntryIndex(int(tableIndex), key)
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("找不到字符串表条目 <%d::%s>", tableIndex, key)
	}
	tablePath := c.archive.Path(tableFileIndex)
	var before pvfversion.ContentSnapshot
	if c.versionRepo != nil {
		var err error
		before, err = pvfversion.ContentSnapshotFromArchive(c.archive, []string{tablePath})
		if err != nil {
			c.mu.Unlock()
			return err
		}
	}
	if err := c.archive.SetStringTableEntryAt(tableFileIndex, key, text); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.versionRepo != nil {
		after, err := pvfversion.ContentSnapshotFromArchive(c.archive, []string{tablePath})
		if err != nil {
			c.mu.Unlock()
			return err
		}
		if err := c.recordVersionMutationLocked("编辑字符串表", before, after); err != nil {
			c.mu.Unlock()
			return err
		}
	}
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	versioned := c.versionRepo != nil
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorAnnotation = editorAnnotationCache{}
	c.invalidateAdvancedSearchLocked()
	delete(c.visualsByFile, index)
	ready := c.indexStatus.State == IndexStateReady && c.searchRecords != nil
	if c.indexDirty == nil {
		c.indexDirty = make(map[int32]struct{})
	}
	c.indexDirty[index] = struct{}{}
	c.indexDirty[tableFileIndex] = struct{}{}
	c.mu.Unlock()
	if ready {
		c.startSearchIndexForced()
	}
	emitEvent("archive:advanced-search-stale", map[string]any{"fileIndex": index})
	if versioned {
		emitVersionState(c, "edited")
	}
	return nil
}

// refreshIndexedRecordsLocked re-reads one file's name and images from the
// archive and updates its search records and tree tags. It reports whether
// anything changed. The caller must hold c.mu and must already have deleted any
// stale entry from c.visualsByFile.
func (c *core) refreshIndexedRecordsLocked(index int32, previousVisuals fileVisuals, hadPreviousVisuals bool) (bool, string, fileVisuals) {
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
	if isNPCEntryPath(c.archive.Path(index)) && c.refreshItemShopNamesLocked() {
		updated = true
	}
	return updated, name, visuals
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
	if c.indexStatus.State == IndexStateReady && c.indexStatus.Refreshing {
		// Keep the old snapshot queryable while a candidate reports progress.
		status.State = IndexStateReady
		status.Refreshing = true
	}
	status.OpenDurationMs = c.indexStatus.OpenDurationMs
	status.BuildDurationMs = elapsedMilliseconds(c.indexStartedAt)
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
	return c.archive == a && c.indexGen == gen &&
		(c.indexStatus.State == IndexStateBuilding ||
			(c.indexStatus.State == IndexStateReady && c.indexStatus.Refreshing))
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

// findArchiveList resolves a `.lst` relation path, tolerating the list layout
// difference between client generations (see Archive.FindList).
func (c *core) findArchiveList(a *pvf.Archive, gen uint64, ctx context.Context, listPath string) (int32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return 0, false
	}
	return a.FindList(listPath)
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

func (c *core) findListTargetFile(a *pvf.Archive, gen uint64, ctx context.Context, listPath, relativePath string) (string, int32, pvf.File, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.indexIsCurrentLocked(a, gen) || ctx.Err() != nil {
		return "", 0, pvf.File{}, false
	}
	targetPath, index, ok := findListTargetInArchive(a, listPath, relativePath)
	if !ok {
		return "", 0, pvf.File{}, false
	}
	return targetPath, index, a.File(index), true
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

// markedName is the name shown by the explorer, the search results and the
// editor's tree tags. It is the placeholder-resolved text, flagged when this
// client's own localization has no text for the key and only a language overlay
// could answer, so a Korean name is never mistaken for the client's own text.
func markedName(metadata pvf.ScriptMetadata) string {
	if metadata.Name == "" || !metadata.NameFallback {
		return metadata.Name
	}
	return metadata.Name + untranslatedMark
}

func readIndexedMetadataFromArchive(a *pvf.Archive, index int32, listPath string, npcNames map[string]string) (pvf.ScriptMetadata, error) {
	metadata, err := a.ScriptMetadata(index)
	if err != nil {
		return pvf.ScriptMetadata{}, err
	}
	metadata.Name = markedName(metadata)
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
	if c.diskIndex != nil {
		return c.diskIndex.visuals(index)
	}
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
	listIndex, ok := a.FindList(npcListPath)
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
		metadata, err := a.ScriptMetadata(targetIndex)
		if err != nil || !metadata.HasName || strings.TrimSpace(metadata.Name) == "" {
			continue
		}
		if _, exists := result[pair.ID]; !exists {
			result[pair.ID] = markedName(metadata)
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

// listPathCandidates returns the archive paths a `.lst` entry may denote. The
// 90US layout stores entry paths relative to the list's own directory
// (`equipment/equipment.lst` + `character/a.equ`), while the 110US layout
// stores archive-root-relative paths (`list/equipment.lst` +
// `equipment/character/a.equ`). Both readings are tried, each also with the
// `(r)` override file name the client falls back to.
func listPathCandidates(listPath, relative string) ([]string, bool) {
	targetPath, ok := resolveListPath(listPath, relative)
	if !ok {
		return nil, false
	}
	candidates := []string{targetPath}
	appendOverride := func(candidate string) {
		base := path.Base(candidate)
		if strings.HasPrefix(strings.ToLower(base), "(r)") {
			return
		}
		candidates = append(candidates, path.Join(path.Dir(candidate), "(r)"+base))
	}
	appendOverride(targetPath)
	root := strings.Trim(strings.ReplaceAll(strings.TrimSpace(relative), "\\", "/"), "/")
	if root != "" && !strings.Contains(root, "..") {
		if cleaned := path.Clean(root); cleaned != "." && cleaned != targetPath {
			candidates = append(candidates, cleaned)
			appendOverride(cleaned)
		}
	}
	return candidates, true
}

func pathBase(p string) string {
	if slash := strings.LastIndexByte(p, '/'); slash >= 0 {
		return p[slash+1:]
	}
	return p
}
