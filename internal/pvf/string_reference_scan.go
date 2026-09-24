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

// StringReferenceScanner visits files in chunk order, retaining a compact file
// index list, the current decompressed chunk and one file's references. Each
// chunk is decoded at most once, without populating the interactive cache.
type StringReferenceScanner struct {
	a          *Archive
	next       int32
	order      []int32
	chunkIndex int32
	chunk      []byte
}

func (a *Archive) NewStringReferenceScanner() *StringReferenceScanner {
	// Counting sort costs O(files + chunks) and four bytes per file. Detached
	// files follow the chunks; returned file identities remain archive indexes.
	bucket := func(item fileItem) int {
		if item.chunk >= 0 && int(item.chunk) < len(a.groups) {
			return int(item.chunk)
		}
		return len(a.groups)
	}
	starts := make([]int, len(a.groups)+1)
	for _, item := range a.items {
		starts[bucket(item)]++
	}
	total := 0
	for i, count := range starts {
		starts[i] = total
		total += count
	}
	order := make([]int32, len(a.items))
	for index, item := range a.items {
		group := bucket(item)
		order[starts[group]] = int32(index)
		starts[group]++
	}
	return &StringReferenceScanner{a: a, order: order, chunkIndex: -1}
}

func (s *StringReferenceScanner) Next(ctx context.Context) (int32, []StringReference, bool, error) {
	index, input, ok, err := s.NextInput(ctx)
	if err != nil || !ok {
		return index, nil, ok, err
	}
	refs, err := input.References(ctx)
	return index, refs, err == nil, err
}

// StringReferenceInput is an immutable file snapshot. References can run on
// different inputs concurrently, without holding the archive's read lock.
// Retaining an input can retain its decompressed chunk; callers must bound
// queued inputs instead of collecting snapshots for the entire archive.
type StringReferenceInput struct {
	raw              []byte
	nameOff, pathOff int32
}

// NextInput reads/decompresses a file without parsing its token references.
// Like Next, it requires external synchronization with archive edits. Inputs
// remain valid after subsequent NextInput calls and after archive edits.
func (s *StringReferenceScanner) NextInput(ctx context.Context) (int32, StringReferenceInput, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, StringReferenceInput{}, false, err
	}
	if s.next >= int32(len(s.order)) {
		s.chunk = nil
		return 0, StringReferenceInput{}, false, nil
	}
	index := s.order[s.next]
	s.next++
	item := s.a.items[index]
	var raw []byte
	if item.typ == TypeScript {
		var ok bool
		raw, ok = s.a.overlay[index]
		if ok {
			// Overlay storage is exposed by RawBytes; isolate worker reads from
			// edits even if a caller modifies an existing payload in place.
			raw = bytes.Clone(raw)
		}
		if !ok && item.chunk >= 0 {
			if s.chunkIndex != item.chunk {
				// Release the previous buffer before decoding the next chunk.
				s.chunk = nil
				var err error
				s.chunk, err = s.a.decompressChunk(item.chunk)
				if err != nil {
					return 0, StringReferenceInput{}, false, err
				}
				s.chunkIndex = item.chunk
			}
			if item.off >= 0 && item.size >= 0 && int64(item.off)+int64(item.size) <= int64(len(s.chunk)) {
				raw = s.chunk[item.off : item.off+item.size]
			}
		}
	}
	return index, StringReferenceInput{raw: raw, nameOff: item.nameOff, pathOff: item.pathOff}, true, nil
}

// References parses only snapshot-owned data and does not access the Archive.
func (input StringReferenceInput) References(ctx context.Context) ([]StringReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
	r := add(input.nameOff)
	r.FileFields = appendUniqueString(r.FileFields, "name")
	r = add(input.pathOff)
	r.FileFields = appendUniqueString(r.FileFields, "path")
	for pos := 0; pos+5 <= len(input.raw); pos += 5 {
		if pos%40960 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		switch input.raw[pos] {
		case 3, 5, 6, 7, 8, 10:
			r = add(int32(binary.LittleEndian.Uint32(input.raw[pos+1:])))
			r.TokenTypes = appendUniqueInt32(r.TokenTypes, int32(input.raw[pos]))
		}
	}
	result := make([]StringReference, 0, len(refs))
	for _, ref := range refs {
		result = append(result, *ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Offset < result[j].Offset })
	return result, nil
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
