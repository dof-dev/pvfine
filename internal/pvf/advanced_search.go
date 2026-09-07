package pvf

import (
	"bytes"
	"context"
	"encoding/binary"
	"regexp"
	"sort"
	"strings"
)

// StringPoolEntry is one decoded value in sTrA or sTrW.
type StringPoolEntry struct {
	Pool   string `json:"pool"`
	Offset int32  `json:"offset"`
	Value  string `json:"value"`
}

// StringPoolFileReference describes how one file references a pool entry.
type StringPoolFileReference struct {
	FileIndex   int32
	Occurrences int
	TokenTypes  []int32
	FileFields  []string
}

// StringPoolMatch is one matching pool value referenced by one file.
type StringPoolMatch struct {
	FileIndex   int32
	Pool        string
	Offset      int32
	Value       string
	Occurrences int
	TokenTypes  []int32
	FileFields  []string
}

type stringPoolEntry struct {
	StringPoolEntry
}

// StringPoolIndex is an in-memory reverse index from string-pool offsets to
// archive file indexes. It is built from the current archive overlay.
type StringPoolIndex struct {
	entries    []stringPoolEntry
	filesByOff map[int32][]StringPoolFileReference
}

// MatchFiles returns file indexes whose referenced pool values match query.
// Text matching is case-insensitive substring matching. Regex matching uses
// Go's RE2-compatible regexp engine and is case-sensitive by default.
func (idx *StringPoolIndex) MatchFiles(query string, regex bool) ([]int32, error) {
	matches, err := idx.MatchDetails(query, regex)
	if err != nil {
		return nil, err
	}
	seen := make(map[int32]struct{}, len(matches))
	for _, match := range matches {
		seen[match.FileIndex] = struct{}{}
	}
	files := make([]int32, 0, len(seen))
	for fileIndex := range seen {
		files = append(files, fileIndex)
	}
	sort.Slice(files, func(i, j int) bool { return files[i] < files[j] })
	return files, nil
}

// MatchDetails returns matching pool values together with their file-level
// reference details. A file may have multiple entries when duplicate values
// exist at different pool offsets.
func (idx *StringPoolIndex) MatchDetails(query string, regex bool) ([]StringPoolMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []StringPoolMatch{}, nil
	}

	var match func(string) bool
	if regex {
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, err
		}
		match = re.MatchString
	} else {
		lower := strings.ToLower(query)
		match = func(value string) bool { return strings.Contains(strings.ToLower(value), lower) }
	}

	matches := make([]StringPoolMatch, 0)
	for _, entry := range idx.entries {
		if !match(entry.Value) {
			continue
		}
		for _, ref := range idx.filesByOff[entry.Offset] {
			matches = append(matches, StringPoolMatch{
				FileIndex:   ref.FileIndex,
				Pool:        entry.Pool,
				Offset:      entry.Offset,
				Value:       entry.Value,
				Occurrences: ref.Occurrences,
				TokenTypes:  append([]int32(nil), ref.TokenTypes...),
				FileFields:  append([]string(nil), ref.FileFields...),
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].FileIndex != matches[j].FileIndex {
			return matches[i].FileIndex < matches[j].FileIndex
		}
		return matches[i].Offset < matches[j].Offset
	})
	return matches, nil
}

// BuildStringPoolIndex scans the current archive payloads without retaining
// decompressed chunks in the regular chunk cache.
func (a *Archive) BuildStringPoolIndex(ctx context.Context) (*StringPoolIndex, error) {
	entries := a.stringPoolEntries()
	validOffsets := make(map[int32]struct{}, len(entries))
	for _, entry := range entries {
		validOffsets[entry.Offset] = struct{}{}
	}

	refsByOffset := make(map[int32]map[int32]*StringPoolFileReference, len(entries))
	err := a.ForEachRawFile(ctx, func(index int32, _ string, _ File, raw []byte) bool {
		item := &a.items[index]
		addReference := func(offset int32) *StringPoolFileReference {
			if _, ok := validOffsets[offset]; !ok {
				return nil
			}
			byFile := refsByOffset[offset]
			if byFile == nil {
				byFile = make(map[int32]*StringPoolFileReference)
				refsByOffset[offset] = byFile
			}
			ref := byFile[index]
			if ref == nil {
				ref = &StringPoolFileReference{FileIndex: index}
				byFile[index] = ref
			}
			ref.Occurrences++
			return ref
		}
		if ref := addReference(item.nameOff); ref != nil {
			ref.FileFields = appendUniqueString(ref.FileFields, "name")
		}
		if ref := addReference(item.pathOff); ref != nil {
			ref.FileFields = appendUniqueString(ref.FileFields, "path")
		}
		if item.typ == TypeScript {
			for pos := 0; pos+5 <= len(raw); pos += 5 {
				switch raw[pos] {
				case 3, 5, 6, 7:
					offset := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
					if ref := addReference(offset); ref != nil {
						ref.TokenTypes = appendUniqueInt32(ref.TokenTypes, int32(raw[pos]))
					}
				}
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	refs := make(map[int32][]StringPoolFileReference, len(refsByOffset))
	for offset, byFile := range refsByOffset {
		list := make([]StringPoolFileReference, 0, len(byFile))
		for _, ref := range byFile {
			copyRef := *ref
			copyRef.TokenTypes = append([]int32(nil), ref.TokenTypes...)
			copyRef.FileFields = append([]string(nil), ref.FileFields...)
			list = append(list, copyRef)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].FileIndex < list[j].FileIndex })
		refs[offset] = list
	}

	return &StringPoolIndex{entries: entries, filesByOff: refs}, nil
}

// EncodeScriptQuery compiles readable script text using only existing string
// pool offsets. It never appends to the archive string pools.
func (a *Archive) EncodeScriptQuery(text string) ([]byte, error) {
	return a.encodeScriptWithResolver(text, func(value string) (int32, error) {
		a.cacheMu.Lock()
		defer a.cacheMu.Unlock()
		a.ensureStringIndexesLocked()
		if offset, ok := a.strAIdx[value]; ok {
			return offset, nil
		}
		if offset, ok := a.strWIdx[value]; ok {
			return offset, nil
		}
		return 0, ErrQueryStringNotInPool
	})
}

// ForEachRawFile visits every file with a decompressed payload. A chunk is
// decompressed once for the duration of its callbacks and is not added to the
// regular chunk cache. The callback must not retain raw after returning.
func (a *Archive) ForEachRawFile(ctx context.Context, fn func(index int32, path string, file File, raw []byte) bool) error {
	byChunk := make([][]int32, len(a.groups))
	var detached []int32
	for i, item := range a.items {
		index := int32(i)
		if item.chunk >= 0 && item.chunk < int32(len(byChunk)) {
			byChunk[item.chunk] = append(byChunk[item.chunk], index)
		} else {
			detached = append(detached, index)
		}
	}

	visit := func(index int32, chunk []byte) bool {
		item := &a.items[index]
		raw := a.overlay[index]
		if raw == nil && chunk != nil && item.off >= 0 && item.size >= 0 &&
			int64(item.off)+int64(item.size) <= int64(len(chunk)) {
			raw = chunk[item.off : item.off+item.size]
		}
		return fn(index, a.Path(index), a.File(index), raw)
	}

	for chunkIndex, indexes := range byChunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(indexes) == 0 {
			continue
		}
		chunk, err := a.decompressChunk(int32(chunkIndex))
		if err != nil {
			return err
		}
		for _, index := range indexes {
			if !visit(index, chunk) {
				return nil
			}
		}
	}

	for _, index := range detached {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !visit(index, nil) {
			return nil
		}
	}
	return nil
}

func (a *Archive) decompressChunk(ci int32) ([]byte, error) {
	if ci < 0 || ci >= int32(len(a.groups)) {
		return nil, nil
	}
	prev := int64(0)
	if ci > 0 {
		prev = int64(a.groups[ci-1].compSize)
	}
	cur := int64(a.groups[ci].compSize)
	size := cur - prev
	if size <= 0 || a.data == nil {
		return nil, nil
	}
	start := a.bodyOff + int(prev)
	end := a.bodyOff + int(cur)
	if start < 0 || start > end || end > len(a.data) {
		return nil, ErrOverflow
	}
	enc := make([]byte, size)
	copy(enc, a.data[start:end])
	crypt(keyBody, magicMain, enc)
	return zlibDecompress(enc)
}

func (a *Archive) stringPoolEntries() []stringPoolEntry {
	entries := make([]stringPoolEntry, 0)
	for pos := 0; pos < len(a.strA); {
		end := bytes.IndexByte(a.strA[pos:], 0)
		valueEnd := len(a.strA)
		if end >= 0 {
			valueEnd = pos + end
		}
		if valueEnd > pos {
			value := string(a.strA[pos:valueEnd])
			entries = append(entries, stringPoolEntry{
				StringPoolEntry: StringPoolEntry{Pool: "sTrA", Offset: int32(pos << 1), Value: value},
			})
		}
		if end < 0 {
			break
		}
		pos = valueEnd + 1
	}

	for pos := 0; pos+1 < len(a.strW); {
		end := pos
		for end+1 < len(a.strW) && !(a.strW[end] == 0 && a.strW[end+1] == 0) {
			end += 2
		}
		if end > pos {
			value := readUTF16(a.strW, pos)
			entries = append(entries, stringPoolEntry{
				StringPoolEntry: StringPoolEntry{Pool: "sTrW", Offset: int32((pos>>1)<<1) | 1, Value: value},
			})
		}
		pos = end + 2
	}
	return entries
}

func appendUniqueInt32(values []int32, value int32) []int32 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
