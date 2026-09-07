package pvf

import (
	"encoding/binary"
	"os"
	"strings"
	"sync"
)

// Header is the decrypted 48-byte archive header (packed, little-endian).
type Header struct {
	Signature     uint32
	Guid          [20]byte
	FileCount     int32
	Padding       int32
	BodySize      int32
	GroupCount    int32
	HashTableSize int32
	NameTableSize int32
}

type fileItem struct {
	nameOff, pathOff int32 // magic offsets into the string pools
	chunk            int32 // body chunk index
	off, size        int32 // location of the file payload inside the decompressed chunk
	typ              int32 // TypeScript / TypeUnicode
}

type groupItem struct{ compSize, origSize int32 }

// Archive is a parsed PVF container. It is not safe for concurrent
// modification, but Chunk decompression is cached and read-only use of
// accessors from multiple goroutines is fine.
type Archive struct {
	cacheMu sync.Mutex

	data  []byte // original file bytes (nil for archives built from scratch)
	hdr   Header
	guard bool

	sourcePath string // file the archive was opened from

	tableOff, hashOff, nameOff, grpiOff, bodyOff int
	hashSize, nameSize, grpiSize                 int

	items  []fileItem
	groups []groupItem // decrypted GRPI

	strA, strW       []byte // decompressed string pools (UTF-8 / UTF-16LE)
	strAIdx, strWIdx map[string]int32
	poolsDirty       bool // pools gained appended strings since parse

	resolveCache map[int32]string
	chunkCache   map[int32][]byte
	overlay      map[int32][]byte // index -> replacement payload
	pathIndex    map[string]int32
}

// Open reads and parses the archive at path.
func Open(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a, err := Parse(data)
	if err != nil {
		return nil, err
	}
	a.sourcePath = path
	return a, nil
}

// Parse parses an archive from memory. The byte slice is retained; treat it
// as read-only afterwards.
func Parse(data []byte) (*Archive, error) {
	if len(data) < headerSize {
		return nil, ErrTruncated
	}
	a := &Archive{
		data:         data,
		resolveCache: map[int32]string{},
		chunkCache:   map[int32][]byte{},
		overlay:      map[int32][]byte{},
		pathIndex:    map[string]int32{},
	}

	var raw [headerSize]byte
	copy(raw[:], data[:headerSize])
	// The guard XOR only touches bytes [24:28] (the FileCount field), so the
	// signature alone cannot tell the variants apart — the decoded section
	// layout must validate as well.
	for _, guard := range [...]bool{true, false} {
		b := raw
		if guard {
			applyGuard(b[:])
		}
		crypt(keyHead, magicMain, b[:])
		if binary.LittleEndian.Uint32(b[:]) != MagicSignature {
			continue
		}
		hdr := decodeHeader(b)
		if hdr.FileCount < 0 || hdr.Padding < 0 || hdr.BodySize < 0 ||
			hdr.GroupCount < 0 || hdr.HashTableSize < 0 || hdr.NameTableSize < 0 {
			continue
		}
		declared := headerSize + int(hdr.FileCount)*0x18 +
			int(hdr.HashTableSize) + int(hdr.NameTableSize) +
			int(hdr.GroupCount)*8 + int(hdr.BodySize)
		if declared > len(data) {
			continue
		}
		a.guard = guard
		a.hdr = hdr
		break
	}
	if a.hdr.Signature != MagicSignature {
		return nil, ErrBadSignature
	}

	// Section layout.
	pos := headerSize
	a.tableOff = pos
	pos += int(a.hdr.FileCount) * 0x18
	a.hashOff = pos
	a.hashSize = int(a.hdr.HashTableSize)
	pos += a.hashSize
	a.nameOff = pos
	a.nameSize = int(a.hdr.NameTableSize)
	pos += a.nameSize
	a.grpiOff = pos
	a.grpiSize = int(a.hdr.GroupCount) * 8
	pos += a.grpiSize
	a.bodyOff = pos

	declaredEnd := pos + int(a.hdr.BodySize)
	if declaredEnd > len(data) {
		return nil, ErrOverflow
	}

	// File table (plaintext).
	a.items = make([]fileItem, a.hdr.FileCount)
	for i := range a.items {
		it := &a.items[i]
		base := a.tableOff + i*0x18
		it.nameOff = int32(binary.LittleEndian.Uint32(data[base:]))
		it.pathOff = int32(binary.LittleEndian.Uint32(data[base+4:]))
		it.chunk = int32(binary.LittleEndian.Uint32(data[base+8:]))
		it.off = int32(binary.LittleEndian.Uint32(data[base+12:]))
		it.size = int32(binary.LittleEndian.Uint32(data[base+16:]))
		it.typ = int32(binary.LittleEndian.Uint32(data[base+20:]))
	}

	// GRPI (cumulative compressed chunk sizes).
	if a.grpiSize > 0 {
		grpi := make([]byte, a.grpiSize)
		copy(grpi, data[a.grpiOff:a.grpiOff+a.grpiSize])
		crypt(keyGrpi, magicMain, grpi)
		a.groups = make([]groupItem, a.hdr.GroupCount)
		for i := range a.groups {
			a.groups[i].compSize = int32(binary.LittleEndian.Uint32(grpi[i*8:]))
			a.groups[i].origSize = int32(binary.LittleEndian.Uint32(grpi[i*8+4:]))
		}
	}

	// String pools.
	a.parseNameTable(data[a.nameOff : a.nameOff+a.nameSize])

	// Path index (case-insensitive, mirrors the reference GM tool).
	for i := range a.items {
		p := normalizePath(a.Path(int32(i)))
		if _, exists := a.pathIndex[p]; !exists {
			a.pathIndex[p] = int32(i)
		}
	}
	return a, nil
}

// New returns an empty archive ready for AddFile + SaveTo.
func New() *Archive {
	return &Archive{
		hdr:          Header{Signature: MagicSignature},
		strA:         []byte{0},
		strW:         []byte{0, 0},
		poolsDirty:   true,
		resolveCache: map[int32]string{},
		chunkCache:   map[int32][]byte{},
		overlay:      map[int32][]byte{},
		pathIndex:    map[string]int32{},
	}
}

func decodeHeader(b [headerSize]byte) Header {
	return Header{
		Signature:     binary.LittleEndian.Uint32(b[0:]),
		FileCount:     int32(binary.LittleEndian.Uint32(b[24:])),
		Padding:       int32(binary.LittleEndian.Uint32(b[28:])),
		BodySize:      int32(binary.LittleEndian.Uint32(b[32:])),
		GroupCount:    int32(binary.LittleEndian.Uint32(b[36:])),
		HashTableSize: int32(binary.LittleEndian.Uint32(b[40:])),
		NameTableSize: int32(binary.LittleEndian.Uint32(b[44:])),
	}
}

// Header returns the (decrypted) archive header.
func (a *Archive) Header() Header { return a.hdr }

// UsesGuard reports whether the archive header uses the 0x55 guard variant.
func (a *Archive) UsesGuard() bool { return a.guard }

// FileCount returns the number of file entries.
func (a *Archive) FileCount() int32 { return int32(len(a.items)) }

// File describes one entry.
type File struct {
	Index      int32
	Name, Path string // resolved from the string pools; Path is the directory part
	ChunkIndex int32
	DataOffset int32
	DataSize   int32
	DataType   int32
}

// File returns entry i.
func (a *Archive) File(i int32) File {
	it := &a.items[i]
	return File{
		Index:      i,
		Name:       a.ResolveString(it.nameOff),
		Path:       a.ResolveString(it.pathOff),
		ChunkIndex: it.chunk,
		DataOffset: it.off,
		DataSize:   it.size,
		DataType:   it.typ,
	}
}

// FullPath returns the canonical "dir/name" path of entry i.
func (a *Archive) FullPath(i int32) string {
	it := &a.items[i]
	return joinPath(a.ResolveString(it.pathOff), a.ResolveString(it.nameOff))
}

// Path is an alias of FullPath.
func (a *Archive) Path(i int32) string { return a.FullPath(i) }

// Find looks up an entry by "dir/name" path (case-insensitive).
func (a *Archive) Find(path string) (int32, bool) {
	i, ok := a.pathIndex[normalizePath(path)]
	return i, ok
}

// Chunk returns decompressed chunk ci, caching the result.
func (a *Archive) Chunk(ci int32) ([]byte, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if ch, ok := a.chunkCache[ci]; ok {
		return ch, nil
	}
	if ci < 0 || ci >= int32(len(a.groups)) {
		return nil, nil
	}
	prev := int64(0)
	if ci > 0 {
		prev = int64(a.groups[ci-1].compSize)
	}
	cur := int64(a.groups[ci].compSize)
	size := cur - prev
	if size <= 0 {
		return nil, nil
	}
	enc := make([]byte, size)
	copy(enc, a.data[a.bodyOff+int(prev):a.bodyOff+int(cur)])
	crypt(keyBody, magicMain, enc)
	raw, err := zlibDecompress(enc)
	if err != nil {
		return nil, err
	}
	a.chunkCache[ci] = raw
	return raw, nil
}

// chunkSpan returns the raw (still encrypted) body byte range of chunk ci.
func (a *Archive) chunkSpan(ci int32) ([]byte, bool) {
	if ci < 0 || ci >= int32(len(a.groups)) {
		return nil, false
	}
	prev := int64(0)
	if ci > 0 {
		prev = int64(a.groups[ci-1].compSize)
	}
	start := a.bodyOff + int(prev)
	end := a.bodyOff + int(a.groups[ci].compSize)
	if start > end || end > len(a.data) {
		return nil, false
	}
	return a.data[start:end], true
}

// normalizePath mirrors the reference implementation's path normalization.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSpace(p)
	for strings.HasPrefix(p, "./") || strings.HasPrefix(p, "/") {
		if strings.HasPrefix(p, "./") {
			p = p[2:]
		} else {
			p = p[1:]
		}
	}
	return strings.TrimRight(p, "/")
}

// joinPath joins raw pool strings for display. No normalization: entries
// may legitimately contain odd whitespace (the reference implementation
// preserves it and only normalizes lookup keys).
func joinPath(dir, name string) string {
	switch {
	case dir == "":
		return name
	case name == "":
		return dir
	default:
		return dir + "/" + name
	}
}

// ArchiveInfoView is a UI-facing snapshot of the archive state.
type ArchiveInfoView struct {
	Path          string `json:"path"`
	FileCount     int32  `json:"fileCount"`
	GroupCount    int32  `json:"groupCount"`
	BodySize      int32  `json:"bodySize"`
	ModifiedCount int    `json:"modifiedCount"`
	UsesGuard     bool   `json:"usesGuard"`
}

// Info returns the current state snapshot.
func (a *Archive) Info() ArchiveInfoView {
	return ArchiveInfoView{
		Path:          a.sourcePath,
		FileCount:     a.hdr.FileCount,
		GroupCount:    a.hdr.GroupCount,
		BodySize:      a.hdr.BodySize,
		ModifiedCount: len(a.overlay),
		UsesGuard:     a.guard,
	}
}
