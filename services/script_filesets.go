package services

import (
	"fmt"
	"strings"

	scriptengine "pvfine/internal/script"
)

const (
	// fileSetChangeKey is the stable selection key for every staged file set
	// mutation. Archive change keys are normalized paths, which can never look
	// like this, so one checkbox can cover the whole file set group.
	fileSetChangeKey = "fileset:*"

	ScriptFileSetChanged = "changed"
	ScriptFileSetAdded   = "added"

	// defaultFileSetName is the built-in set the sidebar always shows. The
	// frontend reserves the id "default" for it and rewrites any other set that
	// claims that id, so a scripted set with this name must own both the name and
	// the id — otherwise reloading shows two sets with the same name.
	defaultFileSetName = "默认文件集"
	defaultFileSetID   = "default"
)

// buildFileSetPreviewRows diffs the staged file sets against the baseline so the
// preview panel can show what each setAll actually changed.
func buildFileSetPreviewRows(stage *scriptengine.FileSetStage) []*ScriptFileSetPreview {
	if stage == nil {
		return nil
	}
	changes := stage.Changes()
	if len(changes) == 0 {
		return nil
	}
	rows := make([]*ScriptFileSetPreview, 0, len(changes))
	for _, change := range changes {
		row := &ScriptFileSetPreview{
			ChangeKey: fileSetChangeKey,
			Name:      change.Name,
			Status:    ScriptFileSetChanged,
			Count:     len(change.Entries),
		}
		if change.Created {
			row.Status = ScriptFileSetAdded
		}
		baseline, existed := stage.Baseline(change.Name)
		if existed {
			row.Added, row.Removed = diffFileSetPaths(baseline.Entries, change.Entries)
		} else {
			for _, entry := range change.Entries {
				row.Added = append(row.Added, entry.Path)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// diffFileSetPaths reports which paths the change introduced and removed,
// preserving the stored order of each list.
func diffFileSetPaths(before, after []scriptengine.FileSetEntry) (added, removed []string) {
	beforePaths := make(map[string]struct{}, len(before))
	for _, entry := range before {
		beforePaths[entry.Path] = struct{}{}
	}
	afterPaths := make(map[string]struct{}, len(after))
	for _, entry := range after {
		afterPaths[entry.Path] = struct{}{}
		if _, exists := beforePaths[entry.Path]; !exists {
			added = append(added, entry.Path)
		}
	}
	for _, entry := range before {
		if _, exists := afterPaths[entry.Path]; !exists {
			removed = append(removed, entry.Path)
		}
	}
	return added, removed
}

// loadFileSetSnapshot reads the persisted file sets for one script run.
//
// The set list mirrors the sidebar exactly, so a name the user sees in the UI is
// the name a script reads and writes. The built-in 「默认文件集」 is exposed even
// when it was never saved (the sidebar also shows it empty), which makes
// pvf.fileset("默认文件集") meaningful instead of returning null. If that set was
// persisted under a different name, the persisted name wins and the built-in
// placeholder is not added — matching what the sidebar displays.
//
// A read failure still exposes the built-in default so a script can use it
// instead of the whole run failing.
func (s *ScriptService) loadFileSetSnapshot() []scriptengine.FileSet {
	sets := make([]scriptengine.FileSet, 0)
	defaultSet := scriptengine.FileSet{ID: defaultFileSetID, Name: defaultFileSetName}
	if s == nil || s.fileSets == nil {
		return append(sets, defaultSet)
	}
	document, err := s.fileSets.LoadFileSets()
	if err != nil {
		return append(sets, defaultSet)
	}
	for _, stored := range document.FileSets {
		entries := make([]scriptengine.FileSetEntry, 0, len(stored.Entries))
		for _, entry := range stored.Entries {
			entries = append(entries, scriptengine.FileSetEntry{
				Path:     entry.Path,
				Name:     entry.Name,
				IDs:      append([]string(nil), entry.IDs...),
				Size:     entry.Size,
				DataType: entry.DataType,
			})
		}
		fileSet := scriptengine.FileSet{
			ID:      stored.ID,
			Name:    strings.TrimSpace(stored.Name),
			Entries: entries,
		}
		// The reserved id belongs to the built-in slot, so the persisted record
		// replaces the placeholder rather than appearing beside it.
		if stored.ID == defaultFileSetID {
			defaultSet = fileSet
			continue
		}
		sets = append(sets, fileSet)
	}
	return append([]scriptengine.FileSet{defaultSet}, sets...)
}

// applyFileSetChanges merges the staged file set mutations into the persisted
// document and saves it. Changes are matched by name because the script cannot
// know the ids the persistence layer assigned.
func (s *ScriptService) applyFileSetChanges(changes []scriptengine.FileSetChange) (int, error) {
	if s == nil || s.fileSets == nil {
		if len(changes) == 0 {
			return 0, nil
		}
		return 0, fmt.Errorf("文件集不可用")
	}
	if len(changes) == 0 {
		return 0, nil
	}
	document, err := s.fileSets.LoadFileSets()
	if err != nil {
		return 0, err
	}

	byName := make(map[string]int, len(document.FileSets))
	for index := range document.FileSets {
		byName[strings.TrimSpace(document.FileSets[index].Name)] = index
	}
	applied := 0
	for _, change := range changes {
		name := strings.TrimSpace(change.Name)
		if name == "" {
			return 0, fmt.Errorf("文件集名称不能为空")
		}
		entries := make([]StoredFileSetEntry, 0, len(change.Entries))
		for _, entry := range change.Entries {
			entries = append(entries, StoredFileSetEntry{
				Path:     entry.Path,
				Name:     entry.Name,
				IDs:      append([]string(nil), entry.IDs...),
				Size:     entry.Size,
				DataType: entry.DataType,
			})
		}
		if index, exists := byName[name]; exists {
			document.FileSets[index].Entries = entries
			applied++
			continue
		}
		document.FileSets = append(document.FileSets, StoredFileSet{
			ID:      fileSetIDForName(document.FileSets, name),
			Name:    name,
			Entries: entries,
		})
		byName[name] = len(document.FileSets) - 1
		applied++
	}
	if document.ActiveSetID == "" && len(document.FileSets) > 0 {
		document.ActiveSetID = document.FileSets[0].ID
	}
	if err := s.fileSets.SaveFileSets(document); err != nil {
		return 0, err
	}
	return applied, nil
}

// fileSetIDForName picks an id the document does not use yet. The sidebar
// reserves the id "default" for its built-in set and remaps a persisted set that
// claims it, so a scripted "默认文件集" takes that id and reappears as the
// built-in set instead of duplicating its name. Ids are opaque to scripts.
func fileSetIDForName(sets []StoredFileSet, name string) string {
	used := make(map[string]struct{}, len(sets))
	for _, fileSet := range sets {
		used[fileSet.ID] = struct{}{}
	}
	if name == defaultFileSetName {
		if _, exists := used[defaultFileSetID]; !exists {
			return defaultFileSetID
		}
	}
	for number := 1; ; number++ {
		candidate := fmt.Sprintf("script-set-%d", number)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}
