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
	idx := 8
	for _, sec := range [...]struct {
		key  string
		xorC uint32
	}{{keyStrA, xorStrA}, {keyStrW, xorStrW}} {
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
		crypt(sec.key, magicAlt, enc)
		raw, err := zlibDecompress(enc)
		if err != nil {
			continue
		}
		_ = cnt2 // rawLen ^ encSize; zlib decoder validates the real length
		if sec.key == keyStrA {
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

// StringOffset returns the magic offset of s, appending it to the UTF-8 pool
// when missing. It is the inverse of ResolveString.
func (a *Archive) StringOffset(s string) int32 {
	a.ensureStringIndexes()
	if off, ok := a.strAIdx[s]; ok {
		return off
	}
	if off, ok := a.strWIdx[s]; ok {
		return off
	}
	old := len(a.strA)
	a.strA = append(a.strA, s...)
	a.strA = append(a.strA, 0)
	off := int32(old << 1)
	a.strAIdx[s] = off
	a.poolsDirty = true
	return off
}

// UnicodeStringOffset is StringOffset for the UTF-16 pool.
func (a *Archive) UnicodeStringOffset(s string) int32 {
	a.ensureStringIndexes()
	if off, ok := a.strWIdx[s]; ok {
		return off
	}
	if off, ok := a.strAIdx[s]; ok {
		return off
	}
	old := len(a.strW)
	if old&1 != 0 {
		a.strW = append(a.strW, 0)
		old++
	}
	u16 := utf16.Encode([]rune(s))
	for _, u := range u16 {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], u)
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
func (a *Archive) buildNameTable() ([]byte, error) {
	var prefix [8]byte
	if a.nameSize >= 8 && a.data != nil {
		copy(prefix[:], a.data[a.nameOff:a.nameOff+8])
	}
	var out []byte
	out = append(out, prefix[:]...)

	var err error
	if out, err = appendNameSection(out, keyStrA, xorStrA, a.strA); err != nil {
		return nil, err
	}
	return appendNameSection(out, keyStrW, xorStrW, a.strW)
}

func appendNameSection(out []byte, key string, xorC uint32, raw []byte) ([]byte, error) {
	compressed, err := zlibCompress(raw)
	if err != nil {
		return nil, err
	}
	enc := make([]byte, len(compressed))
	copy(enc, compressed)
	crypt(key, magicAlt, enc)

	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:], uint32(len(enc))^xorC)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(raw))^uint32(len(enc)))
	out = append(out, hdr[:]...)
	return append(out, enc...), nil
}
