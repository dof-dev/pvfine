package pvf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
)

// StringPoolScanner decodes one pool entry at a time without copying the pool.
// Like Archive, scanners require external synchronization with archive edits.
// Callers may release their read lock between Next calls and abort on edits.
type StringPoolScanner struct {
	a    *Archive
	wide bool
	pos  int
}

func (a *Archive) NewStringPoolScanner() *StringPoolScanner { return &StringPoolScanner{a: a} }

func (s *StringPoolScanner) Next(ctx context.Context) (StringPoolEntry, bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return StringPoolEntry{}, false, err
		}
		if !s.wide {
			if s.pos >= len(s.a.strA) {
				s.wide = true
				s.pos = 0
				continue
			}
			start := s.pos
			n := bytes.IndexByte(s.a.strA[start:], 0)
			end := len(s.a.strA)
			if n >= 0 {
				end = start + n
			}
			s.pos = end + 1
			if end > start {
				return StringPoolEntry{Pool: "sTrA", Offset: int32(start << 1), Value: string(s.a.strA[start:end])}, true, nil
			}
		} else {
			if s.pos+1 >= len(s.a.strW) {
				return StringPoolEntry{}, false, nil
			}
			start := s.pos
			end := start
			for end+1 < len(s.a.strW) && !(s.a.strW[end] == 0 && s.a.strW[end+1] == 0) {
				end += 2
			}
			s.pos = end + 2
			if end > start {
				return StringPoolEntry{Pool: "sTrW", Offset: int32(start) | 1, Value: readUTF16(s.a.strW, start)}, true, nil
			}
		}
	}
}

// StringReference is one pool offset referenced by a file. The pool iterator
// determines which offsets are valid; consumers join references to its entries.
type StringReference struct {
	Offset      int32
	Occurrences int
	TokenTypes  []int32
	FileFields  []string
}

// StringReferenceScanner retains only the current decompressed chunk and one
// file's references. It never populates the interactive decompression cache.
type StringReferenceScanner struct {
	a          *Archive
	next       int32
	chunkIndex int32
	chunk      []byte
}

func (a *Archive) NewStringReferenceScanner() *StringReferenceScanner {
	return &StringReferenceScanner{a: a, chunkIndex: -1}
}

func (s *StringReferenceScanner) Next(ctx context.Context) (int32, []StringReference, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, false, err
	}
	if s.next >= int32(len(s.a.items)) {
		s.chunk = nil
		return 0, nil, false, nil
	}
	index := s.next
	s.next++
	item := s.a.items[index]
	var raw []byte
	if item.typ == TypeScript {
		var ok bool
		raw, ok = s.a.overlay[index]
		if !ok && item.chunk >= 0 {
			if s.chunkIndex != item.chunk {
				// Release the previous buffer before decoding the next chunk.
				s.chunk = nil
				var err error
				s.chunk, err = s.a.decompressChunk(item.chunk)
				if err != nil {
					return 0, nil, false, err
				}
				s.chunkIndex = item.chunk
			}
			if item.off >= 0 && item.size >= 0 && int64(item.off)+int64(item.size) <= int64(len(s.chunk)) {
				raw = s.chunk[item.off : item.off+item.size]
			}
		}
	}
	refs := make(map[int32]*StringReference)
	add := func(offset int32) *StringReference {
		r := refs[offset]
		if r == nil {
			r = &StringReference{Offset: offset}
			refs[offset] = r
		}
		r.Occurrences++
		return r
	}
	r := add(item.nameOff)
	r.FileFields = appendUniqueString(r.FileFields, "name")
	r = add(item.pathOff)
	r.FileFields = appendUniqueString(r.FileFields, "path")
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		if pos%40960 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, nil, false, err
			}
		}
		switch raw[pos] {
		case 3, 5, 6, 7:
			r = add(int32(binary.LittleEndian.Uint32(raw[pos+1:])))
			r.TokenTypes = appendUniqueInt32(r.TokenTypes, int32(raw[pos]))
		}
	}
	result := make([]StringReference, 0, len(refs))
	for _, ref := range refs {
		result = append(result, *ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Offset < result[j].Offset })
	return index, result, true, nil
}

// SavedContentHash identifies the exact loaded bytes only when there are no
// unsaved changes. It does not reread a possibly replaced source file.
func (a *Archive) SavedContentHash() (string, bool) {
	if a.Modified() || len(a.data) == 0 {
		return "", false
	}
	sum := sha256.Sum256(a.data)
	return hex.EncodeToString(sum[:]), true
}
