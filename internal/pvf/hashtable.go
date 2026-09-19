package pvf

import (
	"encoding/binary"
	"sort"
)

// hashEntry mirrors the hash-table lookup aid stored alongside the file
// table; the game binary-searches sortedOffsets after decoding the strings.
type hashEntry struct{ nameOff, pathOff int32 }

// parseHashTable decodes an already-decrypted hash section. Extra trailing
// bytes are ignored; short sections yield whatever is present.
func parseHashTable(b []byte) (entries []hashEntry, sorted []int32) {
	if len(b) < 4 {
		return nil, nil
	}
	pos := 0
	count := int32(binary.LittleEndian.Uint32(b[pos:]))
	pos += 4
	if count > 0 && len(b) >= 4+int(count)*8 {
		entries = make([]hashEntry, count)
		for i := range entries {
			entries[i].nameOff = int32(binary.LittleEndian.Uint32(b[pos:]))
			entries[i].pathOff = int32(binary.LittleEndian.Uint32(b[pos+4:]))
			pos += 8
		}
	}
	if pos+4 <= len(b) {
		lookupCount := int32(binary.LittleEndian.Uint32(b[pos:]))
		pos += 4
		if lookupCount > 0 && pos+int(lookupCount)*4 <= len(b) {
			sorted = make([]int32, lookupCount)
			for i := range sorted {
				sorted[i] = int32(binary.LittleEndian.Uint32(b[pos:]))
				pos += 4
			}
		}
	}
	return entries, sorted
}

// buildHashTable serializes and encrypts the hash section for the current
// item list.
func (a *Archive) buildHashTable() []byte {
	count := len(a.items)
	buf := make([]byte, 4, 4+count*8+8)
	binary.LittleEndian.PutUint32(buf, uint32(count))
	unique := map[int32]bool{}
	for i := range a.items {
		it := &a.items[i]
		var e [8]byte
		binary.LittleEndian.PutUint32(e[0:], uint32(it.nameOff))
		binary.LittleEndian.PutUint32(e[4:], uint32(it.pathOff))
		buf = append(buf, e[:]...)
		unique[it.nameOff] = true
		if it.pathOff >= 0 {
			unique[it.pathOff] = true
		}
	}

	sorted := make([]int32, 0, len(unique))
	for off := range unique {
		sorted = append(sorted, off)
	}
	sort.Slice(sorted, func(x, y int) bool {
		return a.ResolveString(sorted[x]) < a.ResolveString(sorted[y])
	})
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(sorted)))
	buf = append(buf, n[:]...)
	for _, off := range sorted {
		var v [4]byte
		binary.LittleEndian.PutUint32(v[:], uint32(off))
		buf = append(buf, v[:]...)
	}

	cryptSeed(a.keys.hash.seed, a.keys.hash.magic, buf)
	return buf
}

// hashCountWindow is how far the HASH entry count may differ from the file
// count. The 110US container stores exactly one entry per file; the 90US
// variant family keeps a few extra entries (measured: 19), so a small window
// around the file count is searched.
const hashCountWindow = 64

// hashSampleSize is how many entries and how many lookup offsets a candidate
// seed is checked against. A wrong seed yields random offsets, so requiring
// this many of them to resolve to pool strings is decisive, while keeping the
// search cheap.
const hashSampleSize = 64

// recoverHashSeed solves the HASH section seed from the section itself.
//
// The section starts with the entry count as a little-endian u32, which is the
// archive's file count (up to a small window). That known plaintext dword pins
// the low 16 bits of the first keystream dword, leaving at most four candidate
// seeds per (magic, count); the section's structure then validates them: the
// size must fit `4 + count*8 + 4 + n*4` exactly, every sampled entry offset must
// resolve to a pool string, and the lookup list must be ascending by string.
func (a *Archive) recoverHashSeed(cipher []byte, fileCount int) (sectionKey, bool) {
	if len(cipher) < 64 || fileCount <= 0 {
		return sectionKey{}, false
	}
	c0 := binary.LittleEndian.Uint32(cipher)
	for _, magic := range [...]uint32{magicMain, magicAlt} {
		for delta := -hashCountWindow; delta <= hashCountWindow; delta++ {
			count := fileCount + delta
			if !hashSectionFits(len(cipher), count) {
				continue
			}
			for _, seed := range lcgStatesForXk(uint32(count)^c0, magic) {
				if !a.hashSectionLooksSane(cipher, seed, magic, count) {
					continue
				}
				return sectionKey{seed, magic}, true
			}
		}
	}
	return sectionKey{}, false
}

// hashSectionFits reports whether a section of size bytes can hold count
// entries plus a non-empty lookup list.
func hashSectionFits(size, count int) bool {
	if count <= 0 || size <= 8+count*8 {
		return false
	}
	remainder := size - 8 - count*8
	return remainder > 0 && remainder%4 == 0
}

// hashSectionLooksSane decrypts the section with (seed, magic) and validates its
// structure and string offsets.
func (a *Archive) hashSectionLooksSane(cipher []byte, seed, magic uint32, count int) bool {
	plain := make([]byte, len(cipher))
	copy(plain, cipher)
	cryptSeed(seed, magic, plain)
	if int32(binary.LittleEndian.Uint32(plain)) != int32(count) {
		return false
	}
	pos := 4 + count*8
	if pos+4 > len(plain) {
		return false
	}
	n := int(int32(binary.LittleEndian.Uint32(plain[pos:])))
	if n <= 0 || pos+4+n*4 != len(plain) {
		return false
	}
	for _, i := range hashSampleIndexes(count) {
		nameOff := int32(binary.LittleEndian.Uint32(plain[4+i*8:]))
		pathOff := int32(binary.LittleEndian.Uint32(plain[4+i*8+4:]))
		if a.ResolveString(nameOff) == "" || a.ResolveString(pathOff) == "" {
			return false
		}
	}
	previous := ""
	for _, i := range hashSampleIndexes(n) {
		resolved := a.ResolveString(int32(binary.LittleEndian.Uint32(plain[pos+4+i*4:])))
		if resolved == "" || (previous != "" && !(previous < resolved)) {
			return false
		}
		previous = resolved
	}
	return true
}

// hashSampleIndexes returns up to hashSampleSize indexes spread over total.
func hashSampleIndexes(total int) []int {
	if total <= 0 {
		return nil
	}
	if total <= hashSampleSize {
		out := make([]int, total)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, 0, hashSampleSize)
	step := total / hashSampleSize
	for i := 0; i < total && len(out) < hashSampleSize; i += step {
		out = append(out, i)
	}
	return out
}
