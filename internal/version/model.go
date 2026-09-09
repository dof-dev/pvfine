// Package version implements pvfine's local, logical-file version store.
//
// A version repository stores PVF entry payloads and paths.  The packed PVF
// file remains an export artifact and is never used as a binary diff target.
package version

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

// Operation describes how a path changed between two logical snapshots.
type Operation string

const (
	OperationAdd    Operation = "add"
	OperationModify Operation = "modify"
	OperationDelete Operation = "delete"
	OperationRename Operation = "rename"
)

// Entry is the versioned state of one logical PVF file.
type Entry struct {
	Path     string
	DataType int32
	Hash     string
}

// Snapshot is keyed by a canonical, case-insensitive PVF path.
type Snapshot map[string]Entry

// Content is an entry together with its logical payload.  Script and Unicode
// files use rendered text because their raw token bytes refer to PVF string
// pools; unknown types retain their raw bytes. It is used by the in-memory
// undo stack, while committed content is wrapped and stored in ObjectStore.
type Content struct {
	Entry
	Raw []byte
}

// ContentSnapshot contains only the paths affected by one mutation.
type ContentSnapshot map[string]Content

// FileChange is the compact change record stored in a commit.
type FileChange struct {
	Path           string
	DisplayPath    string
	OldPath        string
	OldDisplayPath string
	Operation      Operation
	BeforeHash     string
	AfterHash      string
	BeforeType     int32
	AfterType      int32
}

// Commit is the public repository commit model.
type Commit struct {
	ID          string
	ParentID    string
	Message     string
	CreatedAt   int64
	ChangeCount int
}

// ArtifactState identifies the last PVF artifact written by the editor.
// ArtifactCommitID is the HEAD at the time of that save; it may differ from
// the repository's current HEAD when a commit was created without saving.
type ArtifactState struct {
	PVFHash  string
	TreeHash string
	CommitID string
}

// Meta describes repository compatibility and recovery information.
type Meta struct {
	Format        int
	BaseFile      string
	BaseHash      string
	HashAlgorithm string
	Artifact      ArtifactState
}

// CanonicalPath returns the path identity used by the repository.  PVF path
// lookup is case-insensitive, so case-only differences are intentionally not
// treated as distinct files in V1.
func CanonicalPath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	path = strings.Trim(path, "/")
	return strings.ToLower(path)
}

// DisplayPath normalizes separators while retaining the original casing for
// UI display and newly-created archive entries.
func DisplayPath(path string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
}

// HashBytes returns the V1 content hash format used in the object store.
func HashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// EncodeContent makes an object self-describing. The four-byte type prefix is
// part of the hashed object, so identical text in different PVF data types is
// never confused during checkout.
func EncodeContent(dataType int32, data []byte) []byte {
	result := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(result, uint32(dataType))
	copy(result[4:], data)
	return result
}

// DecodeContent validates and unwraps an object payload.
func DecodeContent(raw []byte) (int32, []byte, error) {
	if len(raw) < 4 {
		return 0, nil, fmt.Errorf("版本对象内容过短")
	}
	return int32(binary.LittleEndian.Uint32(raw)), append([]byte(nil), raw[4:]...), nil
}

func contentFromArchive(a *pvf.Archive, index int32) (Content, error) {
	file := a.File(index)
	var data []byte
	var err error
	switch file.DataType {
	case pvf.TypeScript, pvf.TypeUnicode:
		var text string
		text, err = a.Text(index)
		data = []byte(text)
	default:
		var raw []byte
		raw, err = a.RawBytes(index)
		data = append([]byte(nil), raw...)
	}
	if err != nil {
		return Content{}, err
	}
	return Content{
		Entry: Entry{Path: DisplayPath(a.Path(index)), DataType: file.DataType},
		Raw:   data,
	}, nil
}

func finalizeContent(content Content) Content {
	content.Entry.Hash = HashBytes(EncodeContent(content.DataType, content.Raw))
	content.Raw = append([]byte(nil), content.Raw...)
	return content
}

// HashFile hashes a packed PVF without retaining a second copy in memory.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SnapshotFromArchive reads raw payloads from an archive and returns a
// logical snapshot.  It rejects duplicate canonical paths because an index is
// not a stable identity across PVF structural edits.
func SnapshotFromArchive(a *pvf.Archive) (Snapshot, error) {
	if a == nil {
		return nil, fmt.Errorf("版本快照的归档为空")
	}
	result := make(Snapshot, a.FileCount())
	for index := int32(0); index < a.FileCount(); index++ {
		path := DisplayPath(a.Path(index))
		key := CanonicalPath(path)
		if key == "" {
			return nil, fmt.Errorf("文件 %d 的路径为空,无法建立版本身份", index)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("归档包含重复路径 %q, V1 版本控制暂不支持", path)
		}
		content, err := contentFromArchive(a, index)
		if err != nil {
			return nil, fmt.Errorf("读取 %q 失败: %w", path, err)
		}
		content.Entry.Path = path
		result[key] = finalizeContent(content).Entry
	}
	return result, nil
}

// ContentSnapshotFromArchive captures selected paths for an undoable
// mutation.  Missing paths are intentionally omitted.
func ContentSnapshotFromArchive(a *pvf.Archive, paths []string) (ContentSnapshot, error) {
	if a == nil {
		return nil, fmt.Errorf("版本快照的归档为空")
	}
	result := make(ContentSnapshot, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, rawPath := range paths {
		key := CanonicalPath(rawPath)
		if key == "" {
			continue
		}
		if _, done := seen[key]; done {
			continue
		}
		seen[key] = struct{}{}
		index, ok := a.Find(key)
		if !ok {
			continue
		}
		path := DisplayPath(a.Path(index))
		content, err := contentFromArchive(a, index)
		if err != nil {
			return nil, fmt.Errorf("读取 %q 失败: %w", path, err)
		}
		content.Entry.Path = path
		result[key] = finalizeContent(content)
	}
	return result, nil
}

// ContentObject returns the self-describing object bytes for a content value.
func ContentObject(content Content) []byte {
	return EncodeContent(content.DataType, content.Raw)
}

// SameContent reports whether two content values represent the same logical
// object, including the PVF data type.
func SameContent(left, right Content) bool {
	return left.DataType == right.DataType && bytes.Equal(left.Raw, right.Raw)
}

// SnapshotFromContent converts a partial content snapshot to a regular
// snapshot, preserving only present entries.
func SnapshotFromContent(values ContentSnapshot) Snapshot {
	result := make(Snapshot, len(values))
	for key, value := range values {
		result[key] = value.Entry
	}
	return result
}

// CloneSnapshot makes a detached snapshot copy.
func CloneSnapshot(values Snapshot) Snapshot {
	result := make(Snapshot, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

// CloneContentSnapshot makes a detached content snapshot copy.
func CloneContentSnapshot(values ContentSnapshot) ContentSnapshot {
	result := make(ContentSnapshot, len(values))
	for key, value := range values {
		result[key] = Content{Entry: value.Entry, Raw: append([]byte(nil), value.Raw...)}
	}
	return result
}

// TreeHash is a deterministic hash of a logical snapshot, independent of
// map iteration order and PVF binary layout.
func TreeHash(values Snapshot) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		entry := values[key]
		// Length-prefix strings so unusual PVF paths cannot create ambiguous
		// concatenations.
		_, _ = h.Write([]byte(strconv.Itoa(len(key))))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(strconv.FormatInt(int64(entry.DataType), 10)))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(entry.Hash))
		_, _ = h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Diff returns deterministic path-level changes from before to after.
func Diff(before, after Snapshot) []FileChange {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)

	changes := make([]FileChange, 0, len(ordered))
	for _, key := range ordered {
		oldEntry, hadOld := before[key]
		newEntry, hasNew := after[key]
		switch {
		case !hadOld && hasNew:
			changes = append(changes, FileChange{
				Path:        key,
				DisplayPath: newEntry.Path,
				Operation:   OperationAdd,
				AfterHash:   newEntry.Hash,
				AfterType:   newEntry.DataType,
			})
		case hadOld && !hasNew:
			changes = append(changes, FileChange{
				Path:        key,
				DisplayPath: oldEntry.Path,
				Operation:   OperationDelete,
				BeforeHash:  oldEntry.Hash,
				BeforeType:  oldEntry.DataType,
			})
		case hadOld && hasNew && (oldEntry.Hash != newEntry.Hash || oldEntry.DataType != newEntry.DataType):
			changes = append(changes, FileChange{
				Path:        key,
				DisplayPath: newEntry.Path,
				Operation:   OperationModify,
				BeforeHash:  oldEntry.Hash,
				AfterHash:   newEntry.Hash,
				BeforeType:  oldEntry.DataType,
				AfterType:   newEntry.DataType,
			})
		}
	}
	return changes
}
