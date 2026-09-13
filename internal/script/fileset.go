package script

import (
	"fmt"
	"sort"
	"strings"
)

// FileSetEntry is one entry inside a file set. Only Path is meaningful to a
// script; the remaining fields preserve the metadata the persistence layer
// stores so a rewritten set does not lose names or ids.
type FileSetEntry struct {
	Path     string
	Name     string
	IDs      []string
	Size     int32
	DataType int32
}

// FileSet is an immutable snapshot of one file set.
type FileSet struct {
	ID      string
	Name    string
	Entries []FileSetEntry
}

// FileSetChange is one staged file set mutation. Name addresses the target set;
// Created reports whether the script introduced a set absent from the baseline.
// IDs are assigned by the persistence layer when the plan is applied, so a
// change never carries one.
type FileSetChange struct {
	Name    string
	Created bool
	Entries []FileSetEntry
}

// FileSetStage holds the file sets a run may read plus the mutations it stages.
// It is name-addressed on purpose: ids belong to the persistence layer, and the
// stage must not invent them.
type FileSetStage struct {
	baseline map[string]FileSet
	current  map[string]FileSet
	created  map[string]struct{}
	order    []string
}

// NewFileSetStage builds a stage over the supplied baseline snapshot. Later sets
// with a duplicate name are ignored so lookup stays deterministic.
func NewFileSetStage(sets []FileSet) *FileSetStage {
	stage := &FileSetStage{
		baseline: make(map[string]FileSet, len(sets)),
		current:  make(map[string]FileSet, len(sets)),
		created:  make(map[string]struct{}),
	}
	for _, fileSet := range sets {
		name := NormalizeFileSetName(fileSet.Name)
		if name == "" {
			continue
		}
		if _, exists := stage.baseline[name]; exists {
			continue
		}
		snapshot := FileSet{ID: fileSet.ID, Name: name, Entries: cloneFileSetEntries(fileSet.Entries)}
		stage.baseline[name] = snapshot
		stage.current[name] = snapshot
		stage.order = append(stage.order, name)
	}
	return stage
}

// Lookup returns the current state of a named file set.
func (s *FileSetStage) Lookup(name string) (FileSet, bool) {
	if s == nil {
		return FileSet{}, false
	}
	fileSet, ok := s.current[NormalizeFileSetName(name)]
	return fileSet, ok
}

// Create adds a new file set. It fails when the name is already taken by the
// baseline or by an earlier call in the same run.
func (s *FileSetStage) Create(name string, paths []string) (FileSet, error) {
	if s == nil {
		return FileSet{}, fmt.Errorf("文件集暂存区不可用")
	}
	normalized, err := normalizeFileSetNameStrict(name)
	if err != nil {
		return FileSet{}, err
	}
	if _, exists := s.current[normalized]; exists {
		return FileSet{}, fmt.Errorf("文件集已存在: %s", normalized)
	}
	fileSet := FileSet{Name: normalized, Entries: buildFileSetEntries(paths, nil)}
	s.current[normalized] = fileSet
	s.created[normalized] = struct{}{}
	s.order = append(s.order, normalized)
	return fileSet, nil
}

// Replace overwrites the entry list of an existing file set, keeping the
// metadata of entries whose path survives.
func (s *FileSetStage) Replace(name string, paths []string) (FileSet, error) {
	if s == nil {
		return FileSet{}, fmt.Errorf("文件集暂存区不可用")
	}
	normalized, err := normalizeFileSetNameStrict(name)
	if err != nil {
		return FileSet{}, err
	}
	existing, exists := s.current[normalized]
	if !exists {
		return FileSet{}, fmt.Errorf("文件集不存在: %s", normalized)
	}
	existing.Entries = buildFileSetEntries(paths, existing.Entries)
	s.current[normalized] = existing
	return existing, nil
}

// Baseline returns the snapshot state of a named file set, reporting false when
// the set did not exist before the run.
func (s *FileSetStage) Baseline(name string) (FileSet, bool) {
	if s == nil {
		return FileSet{}, false
	}
	fileSet, ok := s.baseline[NormalizeFileSetName(name)]
	return fileSet, ok
}

// Changes returns every staged mutation in stable name order, omitting sets
// whose entry list is byte-for-byte equivalent to the baseline.
func (s *FileSetStage) Changes() []FileSetChange {
	if s == nil {
		return nil
	}
	changes := make([]FileSetChange, 0)
	for name, current := range s.current {
		baseline, existed := s.baseline[name]
		if existed && fileSetEntriesEqual(baseline.Entries, current.Entries) {
			continue
		}
		_, isCreated := s.created[name]
		changes = append(changes, FileSetChange{
			Name:    name,
			Created: isCreated || !existed,
			Entries: cloneFileSetEntries(current.Entries),
		})
	}
	sort.SliceStable(changes, func(left, right int) bool {
		return changes[left].Name < changes[right].Name
	})
	return changes
}

// buildFileSetEntries normalizes a path list into entries, reusing the metadata
// of a previous entry with the same path so a rewrite keeps names and ids.
func buildFileSetEntries(paths []string, previous []FileSetEntry) []FileSetEntry {
	known := make(map[string]FileSetEntry, len(previous))
	for _, entry := range previous {
		key := NormalizeFileSetPath(entry.Path)
		if key == "" {
			continue
		}
		if _, exists := known[key]; !exists {
			known[key] = entry
		}
	}
	entries := make([]FileSetEntry, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		key := NormalizeFileSetPath(raw)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entry, ok := known[key]
		if !ok {
			entry = FileSetEntry{Name: fileSetPathBase(key)}
		}
		entry.Path = key
		if entry.Name == "" {
			entry.Name = fileSetPathBase(key)
		}
		entry.IDs = uniqueFileSetStrings(entry.IDs)
		entries = append(entries, entry)
	}
	return entries
}

// NormalizeFileSetName trims a set name for lookup. An empty result means the
// name is unusable.
func NormalizeFileSetName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeFileSetNameStrict(name string) (string, error) {
	normalized := NormalizeFileSetName(name)
	if normalized == "" {
		return "", fmt.Errorf("文件集名称不能为空")
	}
	return normalized, nil
}

// NormalizeFileSetPath trims an entry path to its storage spelling, mirroring
// the persistence layer's normalization.
func NormalizeFileSetPath(path string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
}

func fileSetPathBase(path string) string {
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		return path[slash+1:]
	}
	return path
}

// fileSetEntriesEqual reports whether a rewrite is semantically a no-op. Only
// the path order and the ids decide that: name, size and data type are
// archive-derived metadata the sidebar refreshes on its own, so a rebuild that
// merely backfills them must not look like a script change.
func fileSetEntriesEqual(left, right []FileSetEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Path != right[index].Path ||
			!fileSetStringsEqual(left[index].IDs, right[index].IDs) {
			return false
		}
	}
	return true
}

func fileSetStringsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneFileSetEntries(entries []FileSetEntry) []FileSetEntry {
	if len(entries) == 0 {
		return nil
	}
	result := make([]FileSetEntry, len(entries))
	for index, entry := range entries {
		result[index] = entry
		result[index].IDs = append([]string(nil), entry.IDs...)
	}
	return result
}

func uniqueFileSetStrings(values []string) []string {
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
