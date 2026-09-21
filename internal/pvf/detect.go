package pvf

import "encoding/binary"

// detectedArchive contains only the container opening result. Parsing file
// tables, content and edit state is deliberately left to the common parser.
type detectedArchive struct {
	data     []byte
	hdr      Header
	guard    bool
	keys     keySet
	format   formatProfile
	pageKeys []byte
}

// Probes are ordered from cheap known formats to container unlocking. Adding
// a known format does not change the common parser or recovery algorithm.
func detectArchive(data []byte, sidecarDir string) (detectedArchive, error) {
	if len(data) < headerSize {
		return detectedArchive{}, ErrTruncated
	}
	for _, probe := range []func([]byte) (detectedArchive, bool){probeStandard, probeAlternate} {
		if result, ok := probe(data); ok {
			return result, nil
		}
	}
	if dec, pageKeys, keys, hdr, ok := unlockPaged110(data, sidecarDir); ok {
		return detectedArchive{data: dec, hdr: hdr, keys: keys, format: paged110Profile, pageKeys: pageKeys}, nil
	}
	// Recovery remains available even if unrelated sidecar files are present.
	if hdr, guard, keys, ok := recoverHeader(data); ok {
		return detectedArchive{data: data, hdr: hdr, guard: guard, keys: keys, format: recoveredProfile}, nil
	}
	if hasSealedKeyFile(sidecarDir) {
		return detectedArchive{}, ErrPaged110Keys
	}
	return detectedArchive{}, ErrBadSignature
}

func probeStandard(data []byte) (detectedArchive, bool) {
	for _, magic := range [...]uint32{magicMain, magicAlt} {
		keys := standardKeys()
		keys.header.magic = magic
		if result, ok := probeHeader(data, keys, false); ok {
			result.format = standardProfile
			return result, true
		}
	}
	return detectedArchive{}, false
}

func probeAlternate(data []byte) (detectedArchive, bool) {
	result, ok := probeHeader(data, variantKeys(), true)
	result.format = alternateProfile
	return result, ok
}

func probeHeader(data []byte, keys keySet, exactSize bool) (detectedArchive, bool) {
	if len(data) < headerSize {
		return detectedArchive{}, false
	}
	for _, guard := range [...]bool{true, false} {
		var raw [headerSize]byte
		copy(raw[:], data[:headerSize])
		if guard {
			applyGuard(raw[:])
		}
		cryptSeed(keys.header.seed, keys.header.magic, raw[:])
		if binary.LittleEndian.Uint32(raw[:]) != MagicSignature {
			continue
		}
		hdr := decodeHeader(raw)
		if hdr.FileCount < 0 || hdr.Padding < 0 || hdr.BodySize < 0 ||
			hdr.GroupCount < 0 || hdr.HashTableSize < 0 || hdr.NameTableSize < 0 {
			continue
		}
		declared := int64(headerSize) + int64(hdr.FileCount)*24 + int64(hdr.HashTableSize) +
			int64(hdr.NameTableSize) + int64(hdr.GroupCount)*8 + int64(hdr.BodySize)
		if declared > int64(len(data)) || (exactSize && declared != int64(len(data))) {
			continue
		}
		return detectedArchive{data: data, hdr: hdr, guard: guard, keys: keys}, true
	}
	return detectedArchive{}, false
}
