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

	crypt(keyHash, magicMain, buf)
	return buf
}
