package script

import (
	"bytes"
	"fmt"
	"sort"

	"pvfine/internal/pvf"
)

// Change is one staged archive mutation: an edited payload, a newly created
// file, or a deletion. Normalized is the stable identity because structural
// edits shift entry indexes.
type Change struct {
	Kind       string
	Path       string
	Normalized string
	BeforeText string
	AfterText  string
	DataType   int32
	raw        []byte
}

// Transaction is an isolated mutable archive view. It snapshots only the
// entries touched by the script; the full archive remains owned by services.
type Transaction struct {
	base  *pvf.Archive
	stage *pvf.Archive

	// original holds the pre-mutation payload and text of every touched
	// existing entry, keyed by normalized path.
	original     map[string][]byte
	originalText map[string]string
	// paths keeps the display path last seen for a normalized key, which is
	// the only way to name an entry after it has been removed from the stage.
	paths map[string]string

	// created and deleted record structural intents by normalized path. A
	// created entry keeps its payload in the stage overlay.
	created map[string]struct{}
	deleted map[string]struct{}

	// deleteRevision advances whenever a removal shifts entry indexes, which
	// is the only staged mutation that invalidates outstanding file handles.
	deleteRevision int
}

// NewTransaction creates an isolated stage from the current archive. Callers
// must hold the service read lock while cloning the archive.
func NewTransaction(base *pvf.Archive) *Transaction {
	tx := &Transaction{
		original:     make(map[string][]byte),
		originalText: make(map[string]string),
		paths:        make(map[string]string),
		created:      make(map[string]struct{}),
		deleted:      make(map[string]struct{}),
	}
	if base == nil {
		return tx
	}
	tx.base = base
	tx.stage = base.CloneForBatch()
	return tx
}

// Stage returns the isolated archive used by host file handles.
func (t *Transaction) Stage() *pvf.Archive {
	if t == nil {
		return nil
	}
	return t.stage
}

// DeleteRevision reports how many removals were staged. File handles captured
// before a removal must not be reused afterwards.
func (t *Transaction) DeleteRevision() int {
	if t == nil {
		return 0
	}
	return t.deleteRevision
}

// Mark records the staged payload before the first mutation of an entry.
func (t *Transaction) Mark(index int32) error {
	if t == nil || t.stage == nil {
		return fmt.Errorf("脚本事务不可用")
	}
	if index < 0 || index >= t.stage.FileCount() {
		return pvf.ErrBadIndex
	}
	return t.markPath(t.stage.Path(index))
}

func (t *Transaction) markPath(path string) error {
	key := pvf.NormalizePath(path)
	if key == "" {
		return fmt.Errorf("文件路径无效")
	}
	if _, exists := t.original[key]; exists {
		return nil
	}
	index, ok := t.resolve(path)
	if !ok {
		return fmt.Errorf("文件不存在: %s", path)
	}
	raw, err := t.stage.RawBytes(index)
	if err != nil {
		return err
	}
	t.original[key] = append([]byte(nil), raw...)
	t.paths[key] = t.stage.Path(index)
	if text, textErr := t.stage.Text(index); textErr == nil {
		t.originalText[key] = text
	}
	return nil
}

// resolve maps a path to its current staged index.
func (t *Transaction) resolve(path string) (int32, bool) {
	if t == nil || t.stage == nil {
		return 0, false
	}
	return t.stage.Find(path)
}

// SetText stages a text edit addressed by archive path. Paths are the stable
// identity because staged removals renumber entry indexes.
func (t *Transaction) SetText(path, text string) error {
	if err := t.markPath(path); err != nil {
		return err
	}
	index, ok := t.resolve(path)
	if !ok {
		return fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.SetText(index, text)
}

// SetRawBytes stages a binary payload produced by a parsed script document.
func (t *Transaction) SetRawBytes(path string, raw []byte) error {
	if err := t.markPath(path); err != nil {
		return err
	}
	index, ok := t.resolve(path)
	if !ok {
		return fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.SetRawBytes(index, raw)
}

// ListPairs reads the staged id/path pairs of a .lst file.
func (t *Transaction) ListPairs(path string) ([]pvf.ListPair, error) {
	index, ok := t.resolve(path)
	if !ok {
		return nil, fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.ListPairs(index)
}

// SetListPairs stages an insert-or-update of id/path pairs in a .lst file.
func (t *Transaction) SetListPairs(path string, pairs []pvf.ListPair) error {
	if len(pairs) == 0 {
		return nil
	}
	if err := t.markPath(path); err != nil {
		return err
	}
	index, ok := t.resolve(path)
	if !ok {
		return fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.SetListPairs(index, pairs)
}

// UnsetListIDs stages removal of every entry matching the supplied ids.
func (t *Transaction) UnsetListIDs(path string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if err := t.markPath(path); err != nil {
		return 0, err
	}
	index, ok := t.resolve(path)
	if !ok {
		return 0, fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.RemoveListIDs(index, ids)
}

// ListID resolves the id registered for a path in a .lst file.
func (t *Transaction) ListID(path, entryPath string) (string, bool, error) {
	index, ok := t.resolve(path)
	if !ok {
		return "", false, fmt.Errorf("文件不存在: %s", path)
	}
	return t.stage.ListID(index, entryPath)
}

// CreateFile stages a new entry. It fails when the path already exists so a
// typo cannot silently overwrite content the script never read.
func (t *Transaction) CreateFile(rawPath string, dataType int32, raw []byte) (string, error) {
	if t == nil || t.stage == nil {
		return "", fmt.Errorf("脚本事务不可用")
	}
	path, err := pvf.NormalizeNewFilePath(rawPath)
	if err != nil {
		return "", err
	}
	if _, exists := t.resolve(path); exists {
		return "", fmt.Errorf("文件已存在: %s", path)
	}
	if dataType != pvf.TypeScript && dataType != pvf.TypeUnicode {
		return "", fmt.Errorf("不支持的文件类型: %d", dataType)
	}
	t.stage.AddFile(path, append([]byte(nil), raw...), dataType)
	key := pvf.NormalizePath(path)
	// Recreating a path deleted earlier in the same run cancels the removal.
	delete(t.deleted, key)
	t.created[key] = struct{}{}
	t.paths[key] = path
	return path, nil
}

// CopyFile stages a copy of an existing entry. The final staged payload is
// copied, so a file edited earlier in the same run carries its new content.
func (t *Transaction) CopyFile(from, to string, overwrite bool) (string, error) {
	if t == nil || t.stage == nil {
		return "", fmt.Errorf("脚本事务不可用")
	}
	source, ok := t.resolve(from)
	if !ok {
		return "", fmt.Errorf("源文件不存在: %s", from)
	}
	target, err := pvf.NormalizeNewFilePath(to)
	if err != nil {
		return "", err
	}
	if pvf.NormalizePath(target) == pvf.NormalizePath(from) {
		return "", fmt.Errorf("源文件和目标文件不能相同: %s", target)
	}
	raw, err := t.stage.RawBytes(source)
	if err != nil {
		return "", err
	}
	dataType := t.stage.File(source).DataType

	if existing, exists := t.resolve(target); exists {
		if !overwrite {
			return "", fmt.Errorf("目标文件已存在: %s", target)
		}
		if err := t.markPath(target); err != nil {
			return "", err
		}
		if err := t.stage.SetRawBytes(existing, append([]byte(nil), raw...)); err != nil {
			return "", err
		}
		t.paths[pvf.NormalizePath(target)] = t.stage.Path(existing)
		return t.stage.Path(existing), nil
	}

	t.stage.AddFile(target, append([]byte(nil), raw...), dataType)
	key := pvf.NormalizePath(target)
	// Recreating a path deleted earlier in the same run cancels the removal.
	delete(t.deleted, key)
	t.created[key] = struct{}{}
	t.paths[key] = target
	return target, nil
}

// DeleteFile stages removal of one entry and reports whether it existed.
func (t *Transaction) DeleteFile(path string) (bool, error) {
	if t == nil || t.stage == nil {
		return false, fmt.Errorf("脚本事务不可用")
	}
	index, ok := t.resolve(path)
	if !ok {
		return false, nil
	}
	displayPath := t.stage.Path(index)
	if err := t.markPath(displayPath); err != nil {
		return false, err
	}
	if _, err := t.stage.RemoveFiles([]int32{index}); err != nil {
		return false, err
	}
	key := pvf.NormalizePath(displayPath)
	// A path created and then deleted in the same run leaves no trace: the
	// base archive never had it, so there is nothing to remove at commit and
	// no entry index shifts. Discard the snapshot markPath just recorded.
	_, wasCreated := t.created[key]
	delete(t.created, key)
	if wasCreated {
		delete(t.paths, key)
		delete(t.deleted, key)
		delete(t.original, key)
		delete(t.originalText, key)
		return true, nil
	}
	t.deleted[key] = struct{}{}
	t.deleteRevision++
	return true, nil
}

// Changes returns every staged mutation in stable path order. Entries whose
// payload is unchanged and that were not created or deleted are omitted.
func (t *Transaction) Changes() ([]Change, error) {
	if t == nil || t.stage == nil {
		return nil, fmt.Errorf("脚本事务不可用")
	}
	keys := make(map[string]struct{}, len(t.original)+len(t.created))
	for key := range t.original {
		keys[key] = struct{}{}
	}
	for key := range t.created {
		keys[key] = struct{}{}
	}

	changes := make([]Change, 0, len(keys))
	for key := range keys {
		change := Change{Normalized: key, Path: t.paths[key], DataType: pvf.TypeScript}
		if _, isDeleted := t.deleted[key]; isDeleted {
			change.Kind = pvf.ChangeKindDeleted
			change.BeforeText = t.originalText[key]
			change.raw = t.original[key]
			changes = append(changes, change)
			continue
		}
		index, ok := t.resolve(key)
		if !ok {
			continue
		}
		afterRaw, err := t.stage.RawBytes(index)
		if err != nil {
			return nil, err
		}
		if _, isCreated := t.created[key]; isCreated {
			change.Kind = pvf.ChangeKindCreated
		} else {
			if bytes.Equal(t.original[key], afterRaw) {
				continue
			}
			change.Kind = pvf.ChangeKindChanged
			change.BeforeText = t.originalText[key]
		}
		afterText, err := t.stage.Text(index)
		if err != nil {
			return nil, err
		}
		change.Path = t.stage.Path(index)
		change.AfterText = afterText
		change.DataType = t.stage.File(index).DataType
		change.raw = append([]byte(nil), afterRaw...)
		changes = append(changes, change)
	}

	sort.SliceStable(changes, func(left, right int) bool {
		return changes[left].Normalized < changes[right].Normalized
	})
	return changes, nil
}

// Commit copies the selected staged mutations into the live archive and
// reports whether any of them changed the entry table. The caller must hold
// the service write lock and validate its revision first.
func (t *Transaction) Commit(selected map[string]struct{}) (bool, error) {
	if t == nil || t.base == nil || t.stage == nil {
		return false, fmt.Errorf("脚本事务不可用")
	}
	if len(selected) == 0 {
		return false, nil
	}
	changes, err := t.Changes()
	if err != nil {
		return false, err
	}
	applied := make([]pvf.ScriptChange, 0, len(selected))
	structural := false
	for _, change := range changes {
		if _, ok := selected[change.Normalized]; !ok {
			continue
		}
		if change.Kind == pvf.ChangeKindCreated || change.Kind == pvf.ChangeKindDeleted {
			structural = true
		}
		applied = append(applied, pvf.ScriptChange{
			Kind:     change.Kind,
			Path:     change.Normalized,
			Raw:      change.raw,
			DataType: change.DataType,
		})
	}
	if len(applied) == 0 {
		return false, nil
	}
	return structural, t.base.ApplyScriptChanges(t.stage, applied)
}

// Rollback releases the isolated stage. It never mutates the live archive.
func (t *Transaction) Rollback() {
	if t == nil {
		return
	}
	t.stage = nil
	t.original = nil
	t.originalText = nil
	t.paths = nil
	t.created = nil
	t.deleted = nil
}
