package pvf

import (
	"errors"
	"fmt"
	"strings"
)

// Script change kinds describe a staged mutation relative to the archive entry
// table. They mirror the preview status vocabulary of the script workspace.
const (
	ChangeKindChanged = "changed"
	ChangeKindCreated = "created"
	ChangeKindDeleted = "deleted"
)

// NormalizePath exposes the archive's canonical, case-insensitive lookup key.
func NormalizePath(path string) string { return normalizePath(path) }

// NormalizeNewFilePath validates a user-supplied archive path. The result uses
// forward slashes and never contains empty, "." or ".." components.
func NormalizeNewFilePath(raw string) (string, error) {
	path := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if path == "" {
		return "", errors.New("文件路径不能为空")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("文件路径无效: %q", raw)
		}
	}
	return path, nil
}

// ScriptChange is one staged mutation. Raw is the final payload, already
// resolved against the staging archive so it stays valid after entry indexes
// shift. Removals carry no payload.
type ScriptChange struct {
	Kind     string
	Path     string
	Raw      []byte
	DataType int32
}

// ApplyScriptChanges commits the supplied mutations into the receiver, taking
// string pools from stage so staged payloads keep resolving their string
// offsets.
//
// Every change is validated before the first mutation, so a rejected batch
// leaves the archive untouched. Removals run first, which lets a batch reuse a
// path it just freed.
func (a *Archive) ApplyScriptChanges(stage *Archive, changes []ScriptChange) error {
	if a == nil {
		return errors.New("pvf: 归档为空")
	}
	if stage == nil {
		return errors.New("pvf: 暂存状态为空")
	}
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		key := normalizePath(change.Path)
		if key == "" {
			return fmt.Errorf("pvf: 文件路径无效: %q", change.Path)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("pvf: 路径存在重复变更: %s", change.Path)
		}
		seen[key] = struct{}{}
		switch change.Kind {
		case ChangeKindChanged, ChangeKindDeleted:
		case ChangeKindCreated:
			if _, err := NormalizeNewFilePath(change.Path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pvf: 未知变更类型: %q", change.Kind)
		}
		if change.Kind == ChangeKindDeleted {
			continue
		}
		if change.DataType != TypeScript && change.DataType != TypeUnicode {
			return fmt.Errorf("pvf: 不支持的文件类型: %d", change.DataType)
		}
	}

	removals := make([]int32, 0, len(changes))
	for _, change := range changes {
		if change.Kind != ChangeKindDeleted {
			continue
		}
		index, ok := a.Find(change.Path)
		if !ok {
			return fmt.Errorf("pvf: 文件不存在: %s", change.Path)
		}
		removals = append(removals, index)
	}

	a.cacheMu.Lock()
	a.strA = append([]byte(nil), stage.strA...)
	a.strW = append([]byte(nil), stage.strW...)
	a.strAIdx = cloneStringOffsetMap(stage.strAIdx)
	a.strWIdx = cloneStringOffsetMap(stage.strWIdx)
	a.poolsDirty = stage.poolsDirty
	a.resolveCache = cloneStringMap(stage.resolveCache)
	a.resolveCacheOrder = nil
	a.resolveCacheBytes = 0
	a.cacheMu.Unlock()

	if len(removals) > 0 {
		if _, err := a.RemoveFiles(removals); err != nil {
			return err
		}
	}

	for _, change := range changes {
		if change.Kind == ChangeKindDeleted {
			continue
		}
		if index, ok := a.Find(change.Path); ok {
			if err := a.SetDataType(index, change.DataType); err != nil {
				return err
			}
			if err := a.SetRawBytes(index, change.Raw); err != nil {
				return err
			}
			continue
		}
		a.AddFile(change.Path, change.Raw, change.DataType)
	}
	return nil
}
