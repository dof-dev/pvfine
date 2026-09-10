package script

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"pvfine/internal/pvf"
)

// Transaction is an isolated mutable archive view. It snapshots only the
// entries touched by the script; the full archive remains owned by services.
type Transaction struct {
	base  *pvf.Archive
	stage *pvf.Archive

	original     map[int32][]byte
	originalText map[int32]string
}

// NewTransaction creates an isolated stage from the current archive. Callers
// must hold the service read lock while cloning the archive.
func NewTransaction(base *pvf.Archive) *Transaction {
	if base == nil {
		return &Transaction{
			original:     make(map[int32][]byte),
			originalText: make(map[int32]string),
		}
	}
	return &Transaction{
		base:         base,
		stage:        base.CloneForBatch(),
		original:     make(map[int32][]byte),
		originalText: make(map[int32]string),
	}
}

// Stage returns the isolated archive used by host file handles.
func (t *Transaction) Stage() *pvf.Archive {
	if t == nil {
		return nil
	}
	return t.stage
}

// Mark records the staged payload before the first mutation of an entry.
func (t *Transaction) Mark(index int32) error {
	if t == nil || t.stage == nil {
		return fmt.Errorf("脚本事务不可用")
	}
	if index < 0 || index >= t.stage.FileCount() {
		return pvf.ErrBadIndex
	}
	if _, exists := t.original[index]; exists {
		return nil
	}
	raw, err := t.stage.RawBytes(index)
	if err != nil {
		return err
	}
	t.original[index] = append([]byte(nil), raw...)
	if text, textErr := t.stage.Text(index); textErr == nil {
		t.originalText[index] = text
	}
	return nil
}

// SetText stages a text edit without changing the live archive.
func (t *Transaction) SetText(index int32, text string) error {
	if err := t.Mark(index); err != nil {
		return err
	}
	return t.stage.SetText(index, text)
}

// SetRawBytes stages a binary payload produced by a parsed script document.
func (t *Transaction) SetRawBytes(index int32, raw []byte) error {
	if err := t.Mark(index); err != nil {
		return err
	}
	return t.stage.SetRawBytes(index, raw)
}

// ChangedIndexes returns entries whose final payload differs from the stage
// snapshot. Results are stable by normalized path and then file index.
func (t *Transaction) ChangedIndexes() ([]int32, error) {
	if t == nil || t.stage == nil {
		return nil, fmt.Errorf("脚本事务不可用")
	}
	type entry struct {
		index int32
		path  string
	}
	entries := make([]entry, 0, len(t.original))
	for index, before := range t.original {
		after, err := t.stage.RawBytes(index)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(before, after) {
			continue
		}
		entries = append(entries, entry{index: index, path: t.stage.Path(index)})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		leftPath := strings.ToLower(entries[left].path)
		rightPath := strings.ToLower(entries[right].path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return entries[left].index < entries[right].index
	})
	result := make([]int32, len(entries))
	for index, item := range entries {
		result[index] = item.index
	}
	return result, nil
}

// OriginalRaw returns the snapshot payload for one marked entry.
func (t *Transaction) OriginalRaw(index int32) ([]byte, bool) {
	if t == nil {
		return nil, false
	}
	raw, ok := t.original[index]
	return append([]byte(nil), raw...), ok
}

// OriginalText returns the text snapshot for one marked text entry.
func (t *Transaction) OriginalText(index int32) (string, error) {
	if t == nil {
		return "", fmt.Errorf("脚本事务不可用")
	}
	text, ok := t.originalText[index]
	if !ok {
		return "", fmt.Errorf("文件 %d 没有可用的文本快照", index)
	}
	return text, nil
}

// Commit copies selected staged payloads into the live archive. The caller
// must hold the service write lock and validate its revision first.
func (t *Transaction) Commit(selected map[int32]struct{}) error {
	if t == nil || t.base == nil || t.stage == nil {
		return fmt.Errorf("脚本事务不可用")
	}
	return t.base.CommitBatch(t.stage, selected)
}

// Rollback releases the isolated stage. It never mutates the live archive.
func (t *Transaction) Rollback() {
	if t == nil {
		return
	}
	t.stage = nil
	t.original = nil
	t.originalText = nil
}
