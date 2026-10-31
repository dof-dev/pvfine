package pvf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/traditionalchinese"
)

// TW uses an encrypted variable-length directory and independently encrypted
// files. Adapt its tokens to the common editor representation; keep native
// bytes and string indices so untouched entries survive saves verbatim.
type twEntry struct {
	number, size, crc, offset uint32
	typ                       int32
	path                      []byte
}
type twReference struct{ table, key uint32 }
type twArchive struct {
	uuid        []byte
	version     uint32
	entries     []twEntry
	strings     []int32
	stringTable []byte
	directory   []byte
	references  map[twReference]int32
	body        int
}

func isTW(data []byte) bool {
	if len(data) < 20 {
		return false
	}
	n := int(binary.LittleEndian.Uint32(data))
	// Known TW UUIDs are 36-byte strings; accept the binary UUID representation too.
	return (n == 36 || n == 16) && len(data) >= n+20
}

func twDecrypt(data []byte, crc uint32) []byte {
	out := bytes.Clone(data)
	for i := 0; i+4 <= len(out); i += 4 {
		v := binary.LittleEndian.Uint32(out[i:]) ^ (crc ^ 0x81A79011)
		binary.LittleEndian.PutUint32(out[i:], bits.RotateLeft32(v, -6))
	}
	return out
}

func twEncrypt(data []byte, seed uint32) ([]byte, uint32) {
	out := make([]byte, (len(data)+3)&^3)
	copy(out, data)
	crc := crc32.Update(seed, crc32.IEEETable, out)
	for i := 0; i < len(out); i += 4 {
		v := bits.RotateLeft32(binary.LittleEndian.Uint32(out[i:]), 6) ^ (crc ^ 0x81A79011)
		binary.LittleEndian.PutUint32(out[i:], v)
	}
	return out, crc
}

func twDecodeText(data []byte) string {
	// The decoder replaces undefined legacy bytes, like the reference reader.
	out, _ := traditionalchinese.Big5.NewDecoder().Bytes(data)
	return string(out)
}

func (tw *twArchive) payload(data []byte, i int32) ([]byte, error) {
	if i < 0 || int(i) >= len(tw.entries) {
		return nil, ErrBadIndex
	}
	e := tw.entries[i]
	start := uint64(tw.body) + uint64(e.offset)
	end := start + ((uint64(e.size) + 3) &^ uint64(3))
	if end > uint64(len(data)) {
		return nil, ErrOverflow
	}
	raw := twDecrypt(data[start:end], e.crc)
	if e.size > 0 && crc32.Update(e.number, crc32.IEEETable, raw) != e.crc {
		return nil, fmt.Errorf("%w: TW file checksum (entry=%d size=%d)", ErrInvalidSection, i, e.size)
	}
	return raw[:e.size], nil
}

func parseTW(data []byte, codecs ...string) (*Archive, error) {
	if !isTW(data) {
		return nil, ErrBadSignature
	}
	n := int(binary.LittleEndian.Uint32(data))
	h := data[4+n : 4+n+16]
	treeLen := uint64(binary.LittleEndian.Uint32(h[4:]))
	count := uint64(binary.LittleEndian.Uint32(h[12:]))
	body := uint64(n+20) + treeLen
	if treeLen%4 != 0 || body > uint64(len(data)) || count > treeLen/20 {
		return nil, ErrOverflow
	}
	crc := binary.LittleEndian.Uint32(h[8:])
	tree := twDecrypt(data[n+20:body], crc)
	if crc32.Update(uint32(count), crc32.IEEETable, tree) != crc {
		return nil, fmt.Errorf("%w: TW directory checksum", ErrInvalidSection)
	}
	a := New()
	a.data = data
	a.format = twProfile
	if len(codecs) > 0 {
		a.textEncoding = codecs[0]
	}
	tw := &twArchive{uuid: bytes.Clone(data[4 : 4+n]), version: binary.LittleEndian.Uint32(h), body: int(body), directory: tree, references: make(map[twReference]int32)}
	a.tw = tw
	a.bodyOff = tw.body
	a.items = make([]fileItem, 0, count)
	tw.entries = make([]twEntry, 0, count)
	// Directory names use the client's ANSI pool. They are separate from
	// stringtable.bin, whose entries must retain their original numeric indices.
	names := map[string]int32{"": 0}
	nameOffset := func(s string) int32 {
		if off, ok := names[s]; ok {
			return off
		}
		off := int32(len(a.strA) << 1)
		a.strA = append(a.strA, s...)
		a.strA = append(a.strA, 0)
		names[s] = off
		return off
	}
	pos := 0
	stringIndex := int32(-1)
	for i := uint64(0); i < count; i++ {
		if len(tree)-pos < 8 {
			return nil, ErrTruncated
		}
		number := binary.LittleEndian.Uint32(tree[pos:])
		length := uint64(binary.LittleEndian.Uint32(tree[pos+4:]))
		pos += 8
		if length == 0 || length > uint64(len(tree)-pos) || uint64(len(tree)-pos)-length < 12 {
			return nil, ErrInvalidSection
		}
		nativePath := tree[pos : pos+int(length)]
		decodedPath, _ := korean.EUCKR.NewDecoder().Bytes(nativePath)
		path := normalizePath(string(decodedPath))
		pos += int(length)
		e := twEntry{path: nativePath, number: number, size: binary.LittleEndian.Uint32(tree[pos:]), crc: binary.LittleEndian.Uint32(tree[pos+4:]), offset: binary.LittleEndian.Uint32(tree[pos+8:])}
		pos += 12
		if uint64(tw.body)+uint64(e.offset)+((uint64(e.size)+3)&^3) > uint64(len(data)) {
			return nil, ErrOverflow
		}
		dir, name := splitDirName(path)
		if _, exists := a.pathIndex[path]; exists || name == "" {
			return nil, fmt.Errorf("%w: duplicate or empty TW path", ErrInvalidSection)
		}
		index := int32(i)
		a.pathIndex[path] = index
		a.items = append(a.items, fileItem{identity: nextFileIdentity.Add(1), nameOff: nameOffset(name), pathOff: nameOffset(dir), chunk: index})
		tw.entries = append(tw.entries, e)
		if path == "stringtable.bin" {
			stringIndex = index
		}
	}
	if len(tree)-pos > 3 {
		return nil, ErrInvalidSection
	}
	if stringIndex < 0 {
		return nil, fmt.Errorf("%w: TW stringtable.bin missing", ErrInvalidSection)
	}
	table, err := tw.payload(data, stringIndex)
	if err != nil {
		return nil, err
	}
	tw.stringTable = table
	if len(table) < 8 {
		return nil, ErrTruncated
	}
	stringCount := uint64(binary.LittleEndian.Uint32(table))
	bias := (stringCount + 1) * 4
	if bias+4 > uint64(len(table)) {
		return nil, ErrInvalidSection
	}
	first := uint64(binary.LittleEndian.Uint32(table[4:]))
	last := uint64(binary.LittleEndian.Uint32(table[4+stringCount*4:]))
	if first < bias || last < first || last+4 > uint64(len(table)) {
		return nil, ErrInvalidSection
	}
	tw.strings = make([]int32, stringCount)
	appendString := func(s string) int32 {
		off := int32(len(a.strW)) | 1
		a.strW = append(a.strW, utf16le(s)...)
		a.strW = append(a.strW, 0, 0)
		return off
	}
	for i := uint64(0); i < stringCount; i++ {
		start := uint64(binary.LittleEndian.Uint32(table[4+i*4:]))
		end := uint64(binary.LittleEndian.Uint32(table[8+i*4:]))
		if start < bias || end < start || end+4 > uint64(len(table)) {
			return nil, ErrInvalidSection
		}
		tw.strings[i] = appendString(a.decodeNativeText(table[4+start : 4+end]))
	}
	// Inspect native streams once, without retaining decrypted payloads. This
	// establishes exact logical sizes and immutable reference offsets for the
	// common chunk scanner, including concurrent searches.
	a.groups = make([]groupItem, count)
	for i := range a.items {
		raw, err := tw.payload(data, int32(i))
		if err != nil {
			return nil, err
		}
		e := &tw.entries[i]
		it := &a.items[i]
		path := a.Path(int32(i))
		if strings.HasSuffix(path, ".str") || (strings.HasSuffix(path, ".nut") && a.nativePayloadIsText(raw)) {
			e.typ = TypeUnicode
			it.size = int32(len(utf16le(a.decodeNativeText(raw))))
		} else if len(raw) >= 2 && raw[0] == 0xB0 && raw[1] == 0xD0 {
			e.typ = TypeScript
			it.size = int32((len(raw) - 2) / 5 * 5)
			// Some TW writers include up to three alignment bytes in the
			// declared file length; the reference reader ignores that tail.
			tail := raw[2+(len(raw)-2)/5*5:]
			if len(tail) > 3 || !bytes.Equal(tail, make([]byte, len(tail))) {
				return nil, fmt.Errorf("%w: TW token stream tail (%d bytes)", ErrInvalidSection, len(tail))
			}
			for p := 2; p+5 <= len(raw); p += 5 {
				typ := raw[p]
				v := binary.LittleEndian.Uint32(raw[p+1:])
				if typ == 5 || typ == 6 || typ == 7 || typ == 8 || typ == 10 {
					if uint64(v) >= stringCount {
						return nil, fmt.Errorf("%w: TW string index", ErrInvalidSection)
					}
				}
				if typ == 9 {
					if p+10 > len(raw) || raw[p+5] != 10 {
						return nil, fmt.Errorf("%w: TW string reference", ErrInvalidSection)
					}
					key := binary.LittleEndian.Uint32(raw[p+6:])
					if uint64(key) >= stringCount {
						return nil, ErrInvalidSection
					}
					ref := twReference{v, key}
					if _, ok := tw.references[ref]; !ok {
						tw.references[ref] = appendString("<" + strconv.FormatUint(uint64(v), 10) + "::" + a.ResolveString(tw.strings[key]) + ">")
					}
					it.size -= 5
					p += 5
				}
			}
		} else {
			it.size = int32(len(raw))
		}
		it.typ = e.typ
		a.groups[i].origSize = it.size
	}
	a.hdr = Header{FileCount: int32(count), BodySize: int32(len(data) - tw.body), GroupCount: int32(count)}
	a.poolsDirty = false
	return a, nil
}

func (a *Archive) twChunk(ci int32) ([]byte, error) {
	raw, err := a.tw.payload(a.data, ci)
	if err != nil {
		return nil, err
	}
	e := a.tw.entries[ci]
	if e.typ == TypeUnicode {
		return utf16le(a.decodeNativeText(raw)), nil
	}
	if e.typ != TypeScript {
		return raw, nil
	}
	out := make([]byte, 0, len(raw)-2)
	for p := 2; p+5 <= len(raw); p += 5 {
		typ := raw[p]
		v := binary.LittleEndian.Uint32(raw[p+1:])
		switch typ {
		case 2:
			typ = 0
		case 3:
			// Preserve this uncommon native value as an opaque numeric token.
			// Do not reinterpret it as a string index or lose its type on writeback.
			typ = 1
		case 4:
			typ = 2
		case 5:
			typ = 3
			v = uint32(a.tw.strings[v])
		case 6:
			typ = 5
			v = uint32(a.tw.strings[v])
		case 7:
			typ = 6
			v = uint32(a.tw.strings[v])
		case 8:
			typ = 7
			v = uint32(a.tw.strings[v])
		case 10:
			typ = 10
			v = uint32(a.tw.strings[v])
		case 9:
			typ = 8
			v = uint32(a.tw.references[twReference{v, binary.LittleEndian.Uint32(raw[p+6:])}])
			p += 5
		default:
			return nil, fmt.Errorf("%w: unsupported TW token %d", ErrInvalidSection, typ)
		}
		out = append(out, typ, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[len(out)-4:], v)
	}
	return out, nil
}

// saveTW copies unchanged ciphertext, encrypts changed files, and appends
// stringtable entries without renumbering any original string index.
func (a *Archive) saveTW(w io.Writer) error {
	return a.saveTWWithProgress(w, nil)
}

func (a *Archive) saveTWWithProgress(w io.Writer, progress SaveProgressFunc) error {
	if !a.Modified() {
		return a.writeContainerWithProgress(w, a.data, progress)
	}
	tableIndex, ok := a.Find("stringtable.bin")
	if !ok {
		return fmt.Errorf("TW 保存需要 stringtable.bin")
	}
	if _, ok := a.overlay[tableIndex]; ok {
		return fmt.Errorf("TW stringtable.bin 由编辑器维护，不能直接替换")
	}
	tw := a.tw
	indices := make(map[int32]uint32, len(tw.strings))
	for i, off := range tw.strings {
		indices[off] = uint32(i)
	}
	var added [][]byte
	stringIndex := func(off int32) (uint32, error) {
		if i, ok := indices[off]; ok {
			return i, nil
		}
		encoded, err := a.encodeNativeText(a.ResolveString(off))
		if err != nil {
			return 0, fmt.Errorf("TW 字符串无法用所选编码写入: %w", err)
		}
		i := uint32(len(tw.strings) + len(added))
		indices[off] = i
		added = append(added, encoded)
		return i, nil
	}
	var tree, body bytes.Buffer
	nextNumber := uint32(0)
	for _, e := range tw.entries {
		if e.number >= nextNumber {
			nextNumber = e.number + 1
		}
	}
	entries := make([]twEntry, len(a.items))
	total := int64(len(a.items) - 1)
	var processed int64
	reportSaveProgress(progress, "packing", 0, total, "files")
	for i, it := range a.items {
		if int32(i) == tableIndex {
			continue
		}
		var e twEntry
		var encrypted []byte
		raw, changed := a.overlay[int32(i)]
		if it.chunk >= 0 {
			e = tw.entries[it.chunk]
		}
		if it.chunk < 0 {
			e.number = nextNumber
			nextNumber++
			changed = true
		}
		if it.chunk >= 0 && it.typ != e.typ {
			return fmt.Errorf("TW 不支持修改现有文件的数据类型")
		}
		if !changed {
			start := tw.body + int(e.offset)
			encrypted = a.data[start : start+((int(e.size)+3)&^3)]
		} else {
			var err error
			if it.typ == TypeUnicode {
				raw, err = a.encodeNativeText(stringFromUTF16(raw))
			} else if it.typ == TypeScript {
				raw, err = a.encodeTWScript(raw, stringIndex)
			}
			if err != nil {
				return err
			}
			e.size = uint32(len(raw))
			encrypted, e.crc = twEncrypt(raw, e.number)
		}
		e.offset = uint32(body.Len())
		body.Write(encrypted)
		entries[i] = e
		processed++
		reportSaveProgress(progress, "packing", processed, total, "files")
	}
	reportSaveProgress(progress, "metadata", 0, 0, "")
	table := tw.stringTable
	if len(added) > 0 {
		count := len(tw.strings)
		oldBias := (count + 1) * 4
		newBias := (count + len(added) + 1) * 4
		end := int(binary.LittleEndian.Uint32(table[4+count*4:]))
		data := bytes.Clone(table[4+oldBias : 4+end])
		table = make([]byte, 4+newBias)
		binary.LittleEndian.PutUint32(table, uint32(count+len(added)))
		for i := 0; i <= count; i++ {
			v := binary.LittleEndian.Uint32(tw.stringTable[4+i*4:])
			binary.LittleEndian.PutUint32(table[4+i*4:], v+uint32(newBias-oldBias))
		}
		for i, s := range added {
			data = append(data, s...)
			binary.LittleEndian.PutUint32(table[4+(count+i+1)*4:], uint32(newBias+len(data)))
		}
		table = append(table, data...)
	}
	e := tw.entries[a.items[tableIndex].chunk]
	e.size = uint32(len(table))
	e.offset = uint32(body.Len())
	encrypted, crc := twEncrypt(table, e.number)
	e.crc = crc
	body.Write(encrypted)
	entries[tableIndex] = e
	for i, e := range entries {
		path := e.path
		if path == nil {
			var err error
			path, err = korean.EUCKR.NewEncoder().Bytes([]byte(a.Path(int32(i))))
			if err != nil {
				return fmt.Errorf("TW 文件路径无法用 CP949 编码: %w", err)
			}
		}
		for _, v := range []uint32{e.number, uint32(len(path))} {
			binary.Write(&tree, binary.LittleEndian, v)
		}
		tree.Write(path)
		for _, v := range []uint32{e.size, e.crc, e.offset} {
			binary.Write(&tree, binary.LittleEndian, v)
		}
	}
	encrypted, crc = twEncrypt(tree.Bytes(), uint32(len(entries)))
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(len(tw.uuid)))
	out.Write(tw.uuid)
	for _, v := range []uint32{tw.version, uint32(len(encrypted)), crc, uint32(len(entries))} {
		binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(encrypted)
	out.Write(body.Bytes())
	packed := out.Bytes()
	// Validate the complete result before exposing bytes to the writer.
	next, err := parseTW(packed, a.textEncoding)
	if err != nil {
		return err
	}
	for i, item := range a.items {
		if next.items[i].typ != item.typ {
			return fmt.Errorf("TW 文件类型与路径或载荷不匹配（entry=%d）", i)
		}
		next.items[i].identity = item.identity
	}
	if err := a.writeContainerWithProgress(w, packed, progress); err != nil {
		return err
	}
	a.data, a.hdr, a.tw = next.data, next.hdr, next.tw
	a.contentDigest = ""
	a.items, a.groups = next.items, next.groups
	a.bodyOff = next.bodyOff
	a.strA, a.strW = next.strA, next.strW
	a.strAIdx, a.strWIdx = nil, nil
	a.pathIndex = next.pathIndex
	a.compactPaths = next.compactPaths
	a.resolveCache = next.resolveCache
	a.resolveCacheOrder = nil
	a.resolveCacheBytes = 0
	a.overlay = next.overlay
	a.chunkCache = next.chunkCache
	a.chunkCacheMeta = next.chunkCacheMeta
	a.chunkCacheBytes = 0
	a.chunkCacheClock = 0
	a.poolsDirty = false
	a.structuralDirty = false
	a.removedSpans = next.removedSpans
	return nil
}

func stringFromUTF16(raw []byte) string { return string(utf16.Decode(u16le(raw))) }

func (a *Archive) encodeTWScript(raw []byte, index func(int32) (uint32, error)) ([]byte, error) {
	if len(raw)%5 != 0 {
		return nil, ErrInvalidSection
	}
	out := []byte{0xB0, 0xD0}
	appendToken := func(typ byte, v uint32) {
		out = append(out, typ, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[len(out)-4:], v)
	}
	for p := 0; p < len(raw); p += 5 {
		typ := raw[p]
		v := binary.LittleEndian.Uint32(raw[p+1:])
		var err error
		switch typ {
		case 0:
			typ = 2
		case 1:
			typ = 3
		case 2:
			typ = 4
		case 3:
			typ = 5
			v, err = index(int32(v))
		case 5:
			typ = 6
			v, err = index(int32(v))
		case 6:
			typ = 7
			v, err = index(int32(v))
		case 7:
			typ = 8
			v, err = index(int32(v))
		case 10:
			typ = 10
			v, err = index(int32(v))
		case 8:
			ref := a.ResolveString(int32(v))
			parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(ref, "<"), ">"), "::", 2)
			if len(parts) != 2 || !strings.HasPrefix(ref, "<") || !strings.HasSuffix(ref, ">") {
				return nil, fmt.Errorf("TW 字符串引用格式非法")
			}
			table, parseErr := strconv.ParseUint(parts[0], 10, 32)
			if parseErr != nil {
				return nil, parseErr
			}
			key, err := index(a.StringOffset(parts[1]))
			if err != nil {
				return nil, err
			}
			appendToken(9, uint32(table))
			appendToken(10, key)
			continue
		default:
			return nil, fmt.Errorf("TW 不支持 token 类型 %d", typ)
		}
		if err != nil {
			return nil, err
		}
		appendToken(typ, v)
	}
	return out, nil
}
