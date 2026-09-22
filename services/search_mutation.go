package services

import (
	"strings"

	"pvfine/internal/pvf"
)

// searchIndexMutationImpact separates the cheap path snapshot from the
// semantic records that are backed by .lst relations. It is deliberately an
// internal service type: the Wails API continues to expose IndexStatus only.
type searchIndexMutationImpact struct {
	pathOnly    bool
	semantic    bool
	force       bool
	dirtyFiles  map[int32]struct{}
	pendingList map[int32]struct{}
}

func (c *core) scheduleArchiveMutations(a *pvf.Archive, summary pvf.MutationSummary) {
	if a == nil || len(summary.Files) == 0 {
		return
	}
	impact := c.classifyArchiveMutations(a, summary)
	if !impact.pathOnly && !impact.semantic {
		// An initial build may still be scanning the archive. Keep its
		// candidate aligned with edits that happen during that scan without
		// starting a second build for an otherwise irrelevant payload.
		startInitialBuild := false
		c.mu.Lock()
		if c.archive == a && c.indexStatus.State == IndexStateBuilding {
			if c.indexDirty == nil {
				c.indexDirty = make(map[int32]struct{})
			}
			for _, mutation := range summary.Files {
				if mutation.Kind != pvf.MutationModified {
					continue
				}
				if index, ok := a.Find(mutation.Path); ok {
					c.indexDirty[index] = struct{}{}
				}
			}
		} else if c.archive == a && c.indexStatus.State == IndexStateIdle {
			startInitialBuild = true
		}
		c.mu.Unlock()
		if startInitialBuild {
			c.startSearchIndex()
		}
		return
	}

	c.mu.Lock()
	if c.archive != a {
		c.mu.Unlock()
		return
	}
	if impact.pathOnly || impact.semantic {
		c.searchIndexDeltaPending = true
	}
	if len(impact.dirtyFiles) > 0 {
		if c.indexDirty == nil {
			c.indexDirty = make(map[int32]struct{})
		}
		for index := range impact.dirtyFiles {
			c.indexDirty[index] = struct{}{}
		}
	}
	if len(impact.pendingList) > 0 {
		if c.searchIndexListPending == nil {
			c.searchIndexListPending = make(map[int32]struct{})
		}
		for index := range impact.pendingList {
			c.searchIndexListPending[index] = struct{}{}
		}
	}
	if c.diskIndex != nil && (impact.pathOnly || impact.semantic) {
		c.diskIndex.ready = false
		c.diskIndex.dirty = true
	}
	c.mu.Unlock()

	if impact.force {
		c.startSearchIndexForced()
	} else {
		c.startSearchIndex()
	}
}

func (c *core) classifyArchiveMutations(a *pvf.Archive, summary pvf.MutationSummary) searchIndexMutationImpact {
	impact := searchIndexMutationImpact{
		dirtyFiles:  make(map[int32]struct{}),
		pendingList: make(map[int32]struct{}),
	}
	if a == nil || len(summary.Files) == 0 {
		return impact
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.archive != a {
		return impact
	}
	seen := make(map[string]struct{}, len(summary.Files))
	for _, mutation := range summary.Files {
		path := normalizeSearchPath(mutation.Path)
		if path == "" {
			continue
		}
		key := string(mutation.Kind) + "\x00" + path
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		switch mutation.Kind {
		case pvf.MutationAdded, pvf.MutationRemoved:
			// The directory/path snapshot and the file fallback records must
			// follow structural changes even when no semantic list is touched.
			impact.pathOnly = true
			if strings.HasSuffix(path, ".lst") {
				impact.semantic = true
				impact.force = true
			}
		case pvf.MutationModified:
			if strings.HasSuffix(path, ".lst") {
				impact.semantic = true
				if listIndex, ok := c.searchableListIndexLocked(a, path); ok {
					impact.pendingList[listIndex] = struct{}{}
				} else {
					// String-table maps, NPC lists and unknown list layouts can
					// affect more than one relation, so use a full candidate.
					impact.force = true
				}
				continue
			}

			if strings.HasSuffix(path, ".str") {
				if dirty := c.stringTableMutationFilesLocked(a, path); len(dirty) > 0 {
					impact.semantic = true
					for index := range dirty {
						impact.dirtyFiles[index] = struct{}{}
					}
				}
			}
			if sameSearchPath(path, npcListPath) || isNPCEntryPath(path) {
				if c.npcMutationAffectsSearchLocked(a) {
					impact.semantic = true
					impact.force = true
				}
			}
			if index, ok := c.registeredMetadataIndexLocked(a, path); ok {
				if c.registeredMetadataChangedLocked(a, index, path) {
					impact.semantic = true
					impact.dirtyFiles[index] = struct{}{}
				}
			}
		}
	}
	return impact
}

func (c *core) searchableListIndexLocked(a *pvf.Archive, path string) (int32, bool) {
	for _, spec := range c.searchableListSpecsLocked() {
		index, ok := a.FindList(spec.listPath)
		if !ok || !sameSearchPath(a.Path(index), path) {
			continue
		}
		return index, true
	}
	return 0, false
}

func (c *core) registeredMetadataIndexLocked(a *pvf.Archive, path string) (int32, bool) {
	if a == nil {
		return 0, false
	}
	if index, ok := a.Find(path); ok {
		if c.memoryMetadataForFileLocked(index) || c.diskMetadataForFileLocked(index) {
			return index, true
		}
	}
	for _, value := range c.searchMetadata {
		if sameSearchPath(value.path, path) {
			return value.fileIndex, true
		}
	}
	return 0, false
}

func (c *core) memoryMetadataForFileLocked(index int32) bool {
	for _, value := range c.searchMetadata {
		if value.fileIndex == index && value.category != SearchCategoryFile {
			return true
		}
	}
	return false
}

func (c *core) diskMetadataForFileLocked(index int32) bool {
	if c.diskIndex == nil {
		return false
	}
	return c.diskIndex.hasSemanticFile(index)
}

func (c *core) registeredMetadataChangedLocked(a *pvf.Archive, index int32, path string) bool {
	if index < 0 || index >= a.FileCount() {
		return true
	}
	metadata, err := readIndexedMetadataFromArchive(a, index, path, nil)
	if err != nil {
		return true
	}
	name := metadata.Name
	visuals := fileVisuals{
		icon:       imageReferenceFromPVF(metadata.Icon),
		fieldImage: imageReferenceFromPVF(metadata.FieldImage),
	}
	matched := false
	for _, value := range c.searchMetadata {
		if value.fileIndex != index && !sameSearchPath(value.path, path) {
			continue
		}
		if value.category == SearchCategoryFile {
			continue
		}
		matched = true
		if value.name != name || !imageReferencesEqual(value.icon, visuals.icon) || !imageReferencesEqual(value.fieldImage, visuals.fieldImage) {
			return true
		}
	}
	if c.diskIndex != nil && !matched {
		oldNames, nameErr := c.diskIndex.indexedNameValues(index)
		oldVisuals := c.diskIndex.visuals(index)
		if nameErr == nil && (len(oldNames) > 0 || c.diskIndex.hasSemanticFile(index)) {
			matched = true
			for _, oldName := range oldNames {
				if oldName != name {
					return true
				}
			}
			if !imageReferencesEqual(oldVisuals.icon, visuals.icon) || !imageReferencesEqual(oldVisuals.fieldImage, visuals.fieldImage) {
				return true
			}
		}
	}
	return false
}

func (c *core) stringTableMutationFilesLocked(a *pvf.Archive, path string) map[int32]struct{} {
	result := make(map[int32]struct{})
	indexes := make([]int32, 0)
	seen := make(map[int32]struct{})
	for _, value := range c.searchMetadata {
		if _, exists := seen[value.fileIndex]; exists {
			continue
		}
		seen[value.fileIndex] = struct{}{}
		indexes = append(indexes, value.fileIndex)
	}
	if len(indexes) == 0 && c.diskIndex != nil {
		indexes, _ = c.diskIndex.indexedFileIndexes()
	}
	for _, fileIndex := range indexes {
		metadata, err := a.ScriptMetadata(fileIndex)
		if err != nil {
			continue
		}
		usesPath := false
		for _, reference := range metadata.StringTableReferences {
			for _, candidate := range a.StringTablePaths(reference.Index) {
				if sameSearchPath(candidate, path) {
					usesPath = true
					break
				}
			}
			if usesPath {
				break
			}
		}
		if !usesPath {
			continue
		}
		if len(c.searchMetadata) > 0 {
			var previous *indexedMetadata
			for metadataIndex := range c.searchMetadata {
				value := &c.searchMetadata[metadataIndex]
				if value.fileIndex == fileIndex && value.category != SearchCategoryFile {
					previous = value
					break
				}
			}
			if previous == nil {
				continue
			}
			updated, err := readIndexedMetadataFromArchive(a, fileIndex, previous.listPath, nil)
			if err != nil || updated.Name != previous.name ||
				!imageReferencesEqual(imageReferenceFromPVF(updated.Icon), previous.icon) ||
				!imageReferencesEqual(imageReferenceFromPVF(updated.FieldImage), previous.fieldImage) {
				result[fileIndex] = struct{}{}
			}
			continue
		}
		if c.diskIndex != nil {
			oldNames, _ := c.diskIndex.indexedNameValues(fileIndex)
			updated, metadataErr := readIndexedMetadataFromArchive(a, fileIndex, a.Path(fileIndex), nil)
			changed := metadataErr != nil
			if !changed {
				if len(oldNames) == 0 || len(oldNames) > 0 && oldNames[0] != updated.Name {
					changed = true
				}
				oldVisuals := c.diskIndex.visuals(fileIndex)
				changed = changed || !imageReferencesEqual(oldVisuals.icon, imageReferenceFromPVF(updated.Icon)) ||
					!imageReferencesEqual(oldVisuals.fieldImage, imageReferenceFromPVF(updated.FieldImage))
			}
			if changed {
				result[fileIndex] = struct{}{}
			}
		}
	}
	return result
}

func (c *core) npcMutationAffectsSearchLocked(a *pvf.Archive) bool {
	if len(c.searchMetadata) == 0 && c.diskIndex != nil {
		// The disk index does not keep a separate NPC dependency table. Compare
		// the current resolved display name with the names actually indexed for
		// each item-shop file instead of refreshing merely because an item-shop
		// record exists.
		indexes, err := c.diskIndex.indexedFileIndexes()
		if err != nil {
			return true
		}
		for _, fileIndex := range indexes {
			if fileIndex < 0 || fileIndex >= a.FileCount() || !isItemShopEntry("", a.Path(fileIndex)) {
				continue
			}
			metadata, metadataErr := readIndexedMetadataFromArchive(a, fileIndex, itemShopListPath, nil)
			oldNames, namesErr := c.diskIndex.indexedNameValues(fileIndex)
			if metadataErr != nil || namesErr != nil {
				return true
			}
			matched := false
			for _, oldName := range oldNames {
				if oldName == metadata.Name {
					matched = true
					break
				}
			}
			if !matched && (metadata.Name != "" || len(oldNames) > 0) {
				return true
			}
		}
		return false
	}
	for _, value := range c.searchMetadata {
		if value.category == SearchCategoryFile || !isItemShopEntry("", value.path) {
			continue
		}
		metadata, err := readIndexedMetadataFromArchive(a, value.fileIndex, itemShopListPath, nil)
		if err == nil && metadata.Name != value.name {
			return true
		}
	}
	return false
}
