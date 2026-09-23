package pvf

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"pvfine/internal/rendering"
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

type removedFileSpan struct {
	off, size int32
}

// MutationKind identifies one effective archive entry mutation. The mutation
// journal is intentionally small: services use it to decide which derived
// indexes are affected after a compound operation has committed.
type MutationKind string

const (
	MutationModified MutationKind = "modified"
	MutationAdded    MutationKind = "added"
	MutationRemoved  MutationKind = "removed"
)

// FileMutation is one effective file-level change. Path is captured before a
// structural edit can renumber the remaining entries.
type FileMutation struct {
	Index int32
	Path  string
	Kind  MutationKind
}

// MutationSummary describes changes recorded after a mutation checkpoint.
type MutationSummary struct {
	Files      []FileMutation
	Structural bool
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
	keys  keySet // per-section LCG seeds (standard or recovered)

	// format separates container transport from client content conventions.
	// pageKeys retains the per-page state needed to write the container back.
	format   formatProfile
	pageKeys []byte

	sourcePath string // file the archive was opened from

	tableOff, hashOff, nameOff, grpiOff, bodyOff int
	hashSize, nameSize, grpiSize                 int

	items  []fileItem
	groups []groupItem // decrypted GRPI

	strA, strW       []byte // decompressed string pools (UTF-8 / UTF-16LE)
	strAIdx, strWIdx map[string]int32
	poolsDirty       bool // pools gained appended strings since parse

	resolveCache      map[int32]string
	resolveCacheOrder []int32
	resolveCacheBytes int64
	chunkCache        map[int32][]byte
	chunkCacheMeta    map[int32]chunkCacheEntry
	chunkCacheBytes   int64
	chunkCacheClock   uint64
	overlay           map[int32][]byte // index -> replacement payload
	pathIndex         map[string]int32
	structuralDirty   bool // file entries were added or removed since the last save
	removedSpans      map[int32][]removedFileSpan
	mutations         []FileMutation // effective changes since the last checkpoint

	// scriptRenderer controls the user-facing decompiled layout. The
	// canonical renderer is kept stable so version content hashes do not
	// depend on presentation-only configuration.
	scriptRenderer          *rendering.Engine
	canonicalScriptRenderer *rendering.Engine

	// tables caches the lazily loaded string tables used to resolve
	// `<index::key>` placeholders in item names.
	tables struct {
		mu    sync.Mutex
		state *stringTableState
	}
}

// MutationCheckpoint returns the current position in the archive mutation
// journal. The journal is local to one Archive and is not part of the packed
// file format.
func (a *Archive) MutationCheckpoint() int {
	if a == nil {
		return 0
	}
	return len(a.mutations)
}

// MutationsSince returns effective file changes recorded after checkpoint.
// Callers may safely retain the returned slices.
func (a *Archive) MutationsSince(checkpoint int) MutationSummary {
	if a == nil {
		return MutationSummary{}
	}
	if checkpoint < 0 {
		checkpoint = 0
	}
	if checkpoint > len(a.mutations) {
		checkpoint = len(a.mutations)
	}
	result := MutationSummary{
		Files: make([]FileMutation, len(a.mutations)-checkpoint),
	}
	copy(result.Files, a.mutations[checkpoint:])
	for _, mutation := range result.Files {
		if mutation.Kind == MutationAdded || mutation.Kind == MutationRemoved {
			result.Structural = true
			break
		}
	}
	return result
}

// ClearMutations forgets journal entries that have already been consumed by
// the owning service. It does not alter archive content.
func (a *Archive) ClearMutations() {
	if a != nil {
		a.mutations = nil
	}
}

// PendingEdits lists the entries that currently differ from the packed
// baseline: replacement payloads held in the overlay and entries added since
// the archive was parsed. Removed entries cannot be reported because their
// paths are gone from the file table. Callers must not mutate the archive
// while iterating; the returned slice is detached.
func (a *Archive) PendingEdits() []FileMutation {
	if a == nil {
		return nil
	}
	result := make([]FileMutation, 0, len(a.overlay))
	for i := range a.items {
		index := int32(i)
		if a.items[i].chunk < 0 {
			result = append(result, FileMutation{Index: index, Path: a.Path(index), Kind: MutationAdded})
			continue
		}
		if _, ok := a.overlay[index]; ok {
			result = append(result, FileMutation{Index: index, Path: a.Path(index), Kind: MutationModified})
		}
	}
	return result
}

func (a *Archive) recordMutation(index int32, path string, kind MutationKind) {
	if a == nil || path == "" {
		return
	}
	a.mutations = append(a.mutations, FileMutation{Index: index, Path: path, Kind: kind})
}

type chunkCacheEntry struct {
	bytes int64
	used  uint64
}

const defaultChunkCacheLimit = int64(64 << 20)
const defaultResolveCacheLimit = int64(16 << 20)

// Open reads and parses the archive at path. Newer Paged110 containers may
// keep their per-page keys in sibling "sk.dat" / "DFO.exe" files; the bundled
// 110US key table is used when the sibling sk.dat is absent.
func Open(path string) (*Archive, error) {
	return OpenWithSidecars(path, "")
}

// OpenWithSidecars reads and parses the archive at path while resolving
// container key sidecars from sidecarDir instead of the file's own directory.
// Backups of Paged110 archives live outside the client folder, so their page
// keys have to be borrowed from the directory of the file they came from.
// An empty sidecarDir behaves like Open.
func OpenWithSidecars(path, sidecarDir string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if sidecarDir == "" {
		sidecarDir = filepath.Dir(path)
	}
	a, err := parse(data, sidecarDir)
	if err != nil {
		return nil, err
	}
	a.sourcePath = path
	return a, nil
}

// Parse parses an archive from memory. The byte slice is retained; treat it
// as read-only afterwards. Paged110 parsing uses the bundled key table; an
// archive-specific sidecar can be supplied by using Open.
func Parse(data []byte) (*Archive, error) {
	return parse(data, "")
}

func parse(data []byte, sidecarDir string) (*Archive, error) {
	detected, err := detectArchive(data, sidecarDir)
	if err != nil {
		return nil, err
	}
	return parseDetected(detected)
}

// parseDetected is the shared logical-container parser, independent of file
// discovery and physical page unlocking.
func parseDetected(detected detectedArchive) (*Archive, error) {
	data := detected.data
	renderer := defaultScriptRenderer()
	a := &Archive{
		data:                    data,
		resolveCache:            map[int32]string{},
		chunkCache:              map[int32][]byte{},
		chunkCacheMeta:          map[int32]chunkCacheEntry{},
		overlay:                 map[int32][]byte{},
		pathIndex:               map[string]int32{},
		removedSpans:            make(map[int32][]removedFileSpan),
		scriptRenderer:          renderer,
		canonicalScriptRenderer: renderer,
	}

	a.hdr, a.guard, a.keys = detected.hdr, detected.guard, detected.keys
	a.format, a.pageKeys = detected.format, detected.pageKeys

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
		grpiRaw := data[a.grpiOff : a.grpiOff+a.grpiSize]
		grpi := make([]byte, a.grpiSize)
		copy(grpi, grpiRaw)
		cryptSeed(a.keys.grpi.seed, a.keys.grpi.magic, grpi)
		if !a.grpiLooksSane(grpi) {
			// Non-standard seed: recover it from the BodySize anchor and the
			// monotonicity invariants of the cumulative size table.
			if key, ok := recoverGRPISeed(grpiRaw, int(a.hdr.GroupCount), a.hdr.BodySize); ok {
				a.keys.grpi = key
				copy(grpi, grpiRaw)
				cryptSeed(key.seed, key.magic, grpi)
			} else {
				return nil, fmt.Errorf("%w: GRPI cannot be decoded", ErrInvalidSection)
			}
		}
		a.groups = make([]groupItem, a.hdr.GroupCount)
		for i := range a.groups {
			a.groups[i].compSize = int32(binary.LittleEndian.Uint32(grpi[i*8:]))
			a.groups[i].origSize = int32(binary.LittleEndian.Uint32(grpi[i*8+4:]))
		}
	}

	// String pools.
	if err := a.parseNameTable(data[a.nameOff : a.nameOff+a.nameSize]); err != nil {
		return nil, err
	}

	// Hash section seed: known for the standard key set and the alternate
	// variant family. When it is not (an unidentified variant, the Paged110
	// containers), solve it from the section itself now that the string pools
	// are available to validate candidate offsets.
	if !a.keys.hashKnown && a.hashSize > 0 {
		if key, ok := a.recoverHashSeed(data[a.hashOff:a.hashOff+a.hashSize], int(a.hdr.FileCount)); ok {
			a.keys.hash = key
			a.keys.hashKnown = true
		}
	}

	// Body seed: for variants the standard key fails, and chunk 0's zlib
	// header plus GRPI's original size let it be recovered from the data.
	if !a.bodyKeyWorks() {
		if key, ok := recoverZlibSeed(a.firstChunkSpan(), a.firstChunkOrigSize()); ok {
			a.keys.body = key
			a.keys.bodyRecovered = true
		} else {
			return nil, fmt.Errorf("%w: body cannot be decoded", ErrInvalidSection)
		}
	}

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
	renderer := defaultScriptRenderer()
	return &Archive{
		hdr:                     Header{Signature: MagicSignature},
		keys:                    standardKeys(),
		format:                  standardProfile,
		strA:                    []byte{0},
		strW:                    []byte{0, 0},
		poolsDirty:              true,
		resolveCache:            map[int32]string{},
		chunkCache:              map[int32][]byte{},
		chunkCacheMeta:          map[int32]chunkCacheEntry{},
		overlay:                 map[int32][]byte{},
		pathIndex:               map[string]int32{},
		removedSpans:            make(map[int32][]removedFileSpan),
		scriptRenderer:          renderer,
		canonicalScriptRenderer: renderer,
	}
}

func defaultScriptRenderer() *rendering.Engine {
	engine, _ := rendering.LoadDefault()
	return engine
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

// IsPaged110 reports whether the archive uses the 110US page-guard layout.
func (a *Archive) IsPaged110() bool { return a.format.container == pagedContainer }

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
	size := it.size
	if payload, ok := a.overlay[i]; ok {
		size = int32(len(payload))
	}
	return File{
		Index:      i,
		Name:       a.ResolveString(it.nameOff),
		Path:       a.ResolveString(it.pathOff),
		ChunkIndex: it.chunk,
		DataOffset: it.off,
		DataSize:   size,
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

// FindList looks up a `.lst` index across client layouts. The 90US clients keep
// a list next to the files it indexes (`equipment/equipment.lst`), while the
// 110US clients collect every list under `list/` (`list/equipment.lst`) and
// store archive-root-relative entry paths inside it. Both layouts are tried in
// that order, so a configured 90US path also resolves on a newer archive.
func (a *Archive) FindList(path string) (int32, bool) {
	for _, candidate := range listLookupCandidates(path) {
		if i, ok := a.Find(candidate); ok {
			return i, true
		}
	}
	return 0, false
}

func listLookupCandidates(path string) []string {
	trimmed := strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
	if trimmed == "" {
		return nil
	}
	candidates := []string{trimmed}
	base := trimmed
	if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
		base = base[slash+1:]
	}
	if base == "" || strings.EqualFold(base, trimmed) {
		return candidates
	}
	candidates = append(candidates, "list/"+base)
	// A configured `list/x.lst` also names the 90US location `<stem>/x.lst`,
	// e.g. `list/equipment.lst` -> `equipment/equipment.lst`.
	if dir := trimmed[:strings.LastIndexByte(trimmed, '/')]; strings.EqualFold(dir, "list") {
		stem := strings.TrimSuffix(base, pathExt(base))
		if stem != "" {
			candidates = append(candidates, stem+"/"+base)
		}
	}
	return candidates
}

func pathExt(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		return name[dot:]
	}
	return ""
}

// Chunk returns decompressed chunk ci, caching the result.
func (a *Archive) Chunk(ci int32) ([]byte, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if ch, ok := a.chunkCache[ci]; ok {
		a.chunkCacheClock++
		entry := a.chunkCacheMeta[ci]
		entry.used = a.chunkCacheClock
		a.chunkCacheMeta[ci] = entry
		return ch, nil
	}
	raw, err := a.decompressChunk(ci)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	if a.chunkCacheMeta == nil {
		a.chunkCacheMeta = make(map[int32]chunkCacheEntry)
	}
	a.chunkCacheClock++
	if int64(len(raw)) <= defaultChunkCacheLimit {
		for a.chunkCacheBytes+int64(len(raw)) > defaultChunkCacheLimit && len(a.chunkCache) > 0 {
			var oldest int32
			var oldestUsed uint64
			first := true
			for index, entry := range a.chunkCacheMeta {
				if first || entry.used < oldestUsed {
					oldest, oldestUsed, first = index, entry.used, false
				}
			}
			if first {
				break
			}
			delete(a.chunkCache, oldest)
			if entry, ok := a.chunkCacheMeta[oldest]; ok {
				a.chunkCacheBytes -= entry.bytes
			}
			delete(a.chunkCacheMeta, oldest)
		}
		a.chunkCache[ci] = raw
		a.chunkCacheMeta[ci] = chunkCacheEntry{bytes: int64(len(raw)), used: a.chunkCacheClock}
		a.chunkCacheBytes += int64(len(raw))
	}
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
	if start < a.bodyOff || start > end || end > len(a.data) {
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
	return strings.ToLower(strings.TrimRight(p, "/"))
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
	Path              string            `json:"path"`
	FileCount         int32             `json:"fileCount"`
	GroupCount        int32             `json:"groupCount"`
	BodySize          int32             `json:"bodySize"`
	ModifiedCount     int               `json:"modifiedCount"`
	UsesGuard         bool              `json:"usesGuard"`
	Paged110          bool              `json:"paged110"`
	Format            string            `json:"format"`
	ContentRules      ContentRules      `json:"contentRules"`
	WriteCapabilities WriteCapabilities `json:"writeCapabilities"`
}

// Info returns the current state snapshot.
func (a *Archive) Info() ArchiveInfoView {
	return ArchiveInfoView{
		Path:              a.sourcePath,
		FileCount:         int32(len(a.items)),
		GroupCount:        a.hdr.GroupCount,
		BodySize:          a.hdr.BodySize,
		ModifiedCount:     a.ModifiedCount(),
		UsesGuard:         a.guard,
		Paged110:          a.IsPaged110(),
		Format:            a.Format(),
		ContentRules:      a.ContentRules(),
		WriteCapabilities: a.WriteCapabilities(),
	}
}
