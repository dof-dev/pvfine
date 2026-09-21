package pvf

import "bytes"

// normalizeStringPools repairs archives written by older versions of
// the editor. Retail Paged110 archives keep all referenced strings in sTrW;
// an older save could append new ASCII strings to sTrA and leave even offsets
// in file entries and TypeScript payloads. The client accepts the container but
// can crash when it resolves one of those newly added item scripts.
func (a *Archive) normalizeStringPools() error {
	if !a.ContentRules().UTF16Only || len(a.strA) == 0 {
		return nil
	}

	a.cacheMu.Lock()
	a.ensureStringIndexesLocked()
	remap := make(map[int32]int32)
	for pos := 0; pos < len(a.strA); {
		end := bytes.IndexByte(a.strA[pos:], 0)
		valueEnd := len(a.strA)
		if end >= 0 {
			valueEnd = pos + end
		}
		if valueEnd > pos {
			value := string(a.strA[pos:valueEnd])
			oldOffset := int32(pos << 1)
			newOffset, exists := a.strWIdx[value]
			if !exists {
				newOffset = a.appendUTF16StringLocked(value)
			}
			remap[oldOffset] = newOffset
		}
		if end < 0 {
			break
		}
		pos = valueEnd + 1
	}
	a.strA = nil
	a.strAIdx = map[string]int32{}
	a.resolveCache = map[int32]string{}
	a.resolveCacheOrder = nil
	a.resolveCacheBytes = 0
	a.poolsDirty = true
	a.cacheMu.Unlock()

	remapOffset := func(offset int32) (int32, bool) {
		mapped, ok := remap[offset]
		return mapped, ok
	}
	for index := range a.items {
		item := &a.items[index]
		if mapped, ok := remapOffset(item.nameOff); ok {
			item.nameOff = mapped
		}
		if mapped, ok := remapOffset(item.pathOff); ok {
			item.pathOff = mapped
		}
	}

	for index := range a.items {
		if a.items[index].typ != TypeScript {
			continue
		}
		raw, err := a.RawBytes(int32(index))
		if err != nil {
			return err
		}
		if len(raw) == 0 || len(raw)%5 != 0 {
			continue
		}
		updated := append([]byte(nil), raw...)
		changed := false
		for pos := 0; pos+5 <= len(updated); pos += 5 {
			tokenType := updated[pos]
			if tokenType != 3 && tokenType != 5 && tokenType != 6 &&
				tokenType != 7 && tokenType != 8 && tokenType != 10 {
				continue
			}
			oldOffset := int32(uint32(updated[pos+1]) |
				uint32(updated[pos+2])<<8 |
				uint32(updated[pos+3])<<16 |
				uint32(updated[pos+4])<<24)
			mapped, ok := remapOffset(oldOffset)
			if !ok {
				continue
			}
			updated[pos+1] = byte(mapped)
			updated[pos+2] = byte(mapped >> 8)
			updated[pos+3] = byte(mapped >> 16)
			updated[pos+4] = byte(mapped >> 24)
			changed = true
		}
		if changed {
			if err := a.SetRawBytes(int32(index), updated); err != nil {
				return err
			}
		}
	}
	return nil
}
