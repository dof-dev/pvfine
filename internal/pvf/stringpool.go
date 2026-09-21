package pvf

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// parseNameTable decodes the sTrA (UTF-8) and sTrW (UTF-16LE) pools:
//
//	8-byte prefix, then per section:
//	  u32 encSize ^ xorConst
//	  u32 rawLen  ^ encSize
//	  encSize bytes: crypt2(key) -> zlib
func (a *Archive) parseNameTable(nb []byte) {
	maskA, maskW := a.keys.maskA, a.keys.maskW
	if maskA == 0 {
		maskA = xorStrA
	}
	if maskW == 0 {
		maskW = xorStrW
	}
	idx := 8
	for _, sec := range [...]struct {
		key  sectionKey
		xorC uint32
	}{{a.keys.strA, maskA}, {a.keys.strW, maskW}} {
		if idx+8 > len(nb) {
			return
		}
		cnt1 := binary.LittleEndian.Uint32(nb[idx:])
		cnt2 := binary.LittleEndian.Uint32(nb[idx+4:])
		idx += 8
		encSize := int64(cnt1 ^ sec.xorC)
		if encSize <= 0 || idx+int(encSize) > len(nb) {
			continue
		}
		enc := make([]byte, encSize)
		copy(enc, nb[idx:idx+int(encSize)])
		idx += int(encSize)
		rawLen := int(int32(cnt2 ^ uint32(encSize)))
		cryptSeed(sec.key.seed, sec.key.magic, enc)
		raw, err := zlibDecompress(enc)
		if err != nil {
			// Non-standard seed: the pool is a zlib stream, so its header gives
			// two known plaintext bytes and rawLen pins the inflated length.
			enc2 := make([]byte, encSize)
			copy(enc2, nb[idx-int(encSize):idx])
			recovered, ok := recoverZlibSeed(enc2, rawLen)
			if !ok {
				continue
			}
			cryptSeed(recovered.seed, recovered.magic, enc2)
			raw, err = zlibDecompress(enc2)
			if err != nil {
				continue
			}
			if sec.xorC == maskA {
				a.keys.strA = recovered
			} else {
				a.keys.strW = recovered
			}
		}
		if sec.xorC == maskA {
			a.strA = raw
		} else {
			a.strW = raw
		}
	}
}

// ResolveString maps a magic pool offset to its string.
// Even values index the UTF-8 pool (byte offset = off>>1); odd values index
// the UTF-16 pool (byte offset = (off>>1)*2).
func (a *Archive) ResolveString(magicOff int32) string {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if magicOff < 0 {
		return ""
	}
	if s, ok := a.resolveCache[magicOff]; ok {
		return s
	}
	var s string
	if magicOff&1 != 0 {
		s = readUTF16(a.strW, int(magicOff>>1)*2)
	} else {
		s = readUTF8(a.strA, int(magicOff>>1))
	}
	a.resolveCache[magicOff] = s
	return s
}

func readUTF8(buf []byte, start int) string {
	if start < 0 || start >= len(buf) {
		return ""
	}
	end := bytes.IndexByte(buf[start:], 0)
	if end < 0 {
		end = len(buf) - start
	}
	return string(buf[start : start+end])
}

func readUTF16(buf []byte, start int) string {
	if start < 0 || start >= len(buf) {
		return ""
	}
	end := start
	for end+1 < len(buf) {
		if buf[end] == 0 && buf[end+1] == 0 {
			break
		}
		end += 2
	}
	if (end-start)%2 != 0 {
		end--
	}
	u16 := make([]uint16, (end-start)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(buf[start+i*2:])
	}
	return string(utf16.Decode(u16))
}

// ensureStringIndexes builds value -> magic-offset maps for both pools
// (first occurrence wins, mirroring the reference implementation).
func (a *Archive) ensureStringIndexes() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.ensureStringIndexesLocked()
}

func (a *Archive) ensureStringIndexesLocked() {
	if a.strAIdx != nil {
		return
	}
	a.strAIdx = map[string]int32{}
	a.strWIdx = map[string]int32{}

	for pos := 0; pos < len(a.strA); {
		end := bytes.IndexByte(a.strA[pos:], 0)
		vEnd := len(a.strA)
		if end >= 0 {
			vEnd = pos + end
		}
		if vEnd > pos {
			v := string(a.strA[pos:vEnd])
			if _, ok := a.strAIdx[v]; !ok {
				a.strAIdx[v] = int32(pos << 1)
			}
		}
		if end < 0 {
			break
		}
		pos = vEnd + 1
	}

	for pos := 0; pos+1 < len(a.strW); {
		end := pos
		for end+1 < len(a.strW) && !(a.strW[end] == 0 && a.strW[end+1] == 0) {
			end += 2
		}
		if end > pos {
			u16 := make([]uint16, (end-pos)/2)
			for i := range u16 {
				u16[i] = binary.LittleEndian.Uint16(a.strW[pos+i*2:])
			}
			v := string(utf16.Decode(u16))
			if _, ok := a.strWIdx[v]; !ok {
				a.strWIdx[v] = int32((pos>>1)<<1) | 1
			}
		}
		pos = end + 2
	}
}

// StringOffset returns the magic offset of s, appending it to a pool when
// missing. It is the inverse of ResolveString.
//
// The pool is chosen the way every known client build stores its own text:
// non-ASCII goes to the UTF-16 pool, and ASCII goes to the UTF-8 pool for the
// 90US builds. Paged110 keeps newly written strings in the UTF-16 pool even
// when a damaged or third-party archive already has a non-empty UTF-8 pool.
//
// Writing CJK text into the UTF-8 pool is what made freshly entered Chinese show
// up as mojibake in game: the client resolves non-ASCII strings from the UTF-16
// pool, so a UTF-8 entry is not the text it expects.
func (a *Archive) StringOffset(s string) int32 {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.ensureStringIndexesLocked()
	if off, ok := a.strAIdx[s]; ok {
		return off
	}
	if off, ok := a.strWIdx[s]; ok {
		return off
	}
	if a.paged110 || !isASCIIString(s) || !a.hasUTF8PoolLocked() {
		return a.appendUTF16StringLocked(s)
	}
	old := len(a.strA)
	a.strA = append(a.strA, s...)
	a.strA = append(a.strA, 0)
	off := int32(old << 1)
	a.strAIdx[s] = off
	a.poolsDirty = true
	return off
}

// UnicodeStringOffset returns a UTF-16-pool offset, appending a second copy
// when the same text already exists only in the UTF-8 pool.
func (a *Archive) UnicodeStringOffset(s string) int32 {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.ensureStringIndexesLocked()
	if off, ok := a.strWIdx[s]; ok {
		return off
	}
	return a.appendUTF16StringLocked(s)
}

// hasUTF8PoolLocked reports whether the archive stores anything in the UTF-8
// pool. An empty pool means this build keeps all of its strings in the UTF-16
// pool (the 110US containers do).
func (a *Archive) hasUTF8PoolLocked() bool {
	return len(a.strA) > 0
}

func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return false
		}
	}
	return true
}

// appendUTF16StringLocked appends s to the UTF-16 pool and returns its magic
// offset. The caller must hold cacheMu.
func (a *Archive) appendUTF16StringLocked(s string) int32 {
	old := len(a.strW)
	if old&1 != 0 {
		a.strW = append(a.strW, 0)
		old++
	}
	for _, unit := range utf16.Encode([]rune(s)) {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], unit)
		a.strW = append(a.strW, b[:]...)
	}
	a.strW = append(a.strW, 0, 0)
	off := int32((old>>1)<<1) | 1
	a.strWIdx[s] = off
	a.poolsDirty = true
	return off
}

// buildNameTable serializes the (possibly extended) string pools back into
// the encrypted name-table section format.
//
// The archive's own pool keys and size masks are used, not the legacy
// constants: the Paged110 containers name and obfuscate these sections
// differently, and writing them with the 90US key made the section unreadable
// (the size field decodes to garbage, so the parser skips the pool entirely).
func (a *Archive) buildNameTable() ([]byte, error) {
	var prefix [8]byte
	if a.nameSize >= 8 && a.data != nil {
		copy(prefix[:], a.data[a.nameOff:a.nameOff+8])
	}
	var out []byte
	out = append(out, prefix[:]...)

	maskA, maskW := a.keys.maskA, a.keys.maskW
	if maskA == 0 {
		maskA = xorStrA
	}
	if maskW == 0 {
		maskW = xorStrW
	}
	var err error
	if out, err = appendNameSection(out, a.keys.strA, maskA, a.strA); err != nil {
		return nil, err
	}
	return appendNameSection(out, a.keys.strW, maskW, a.strW)
}

func appendNameSection(out []byte, section sectionKey, xorC uint32, raw []byte) ([]byte, error) {
	compressed, err := zlibCompress(raw)
	if err != nil {
		return nil, err
	}
	enc := make([]byte, len(compressed))
	copy(enc, compressed)
	cryptSeed(section.seed, section.magic, enc)

	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:], uint32(len(enc))^xorC)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(raw))^uint32(len(enc)))
	out = append(out, hdr[:]...)
	return append(out, enc...), nil
}
