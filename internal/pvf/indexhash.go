package pvf

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The newest clients ship an "index hash" next to every `.lst`:
//
//	list/equipment.lst            id -> path
//	list/equipment_indexhash.etc  id -> <decimal uint32>
//
// Like the `.lst`, the index file is a plain script token stream, but its second
// column is a *string* token whose text is a decimal number (a value above
// 2^31 has no signed-int token form). The numbers are an opaque per-id key:
// measured on the retail 110US container they depend on the id alone (the same
// id has the same value in stackable/monster/dnf/appendage even though those
// lists give it completely different paths), they are not derived from the path
// or the name by any of the hash functions tried so far, and every id present in
// a list also has an entry here (the file is a strict superset of the list, with
// a few thousand stale extras).

// IndexHashPair is one `id -> value` entry of a `*_indexhash.etc` file.
type IndexHashPair struct {
	ID    uint32 `json:"id"`
	Value uint32 `json:"value"`
}

// IndexHashValue returns the value stored beside an id in a 110US
// *_indexhash.etc file. It is the reversible 32-bit hash used by the client;
// all arithmetic intentionally wraps at uint32.
func IndexHashValue(id uint32) uint32 {
	const multiplier uint32 = 0x45d9f3b
	for i := 0; i < 2; i++ {
		id = (id ^ (id >> 16)) * multiplier
	}
	return id ^ (id >> 16)
}

// IndexHashCompanionPath returns the `_indexhash.etc` path paired with a `.lst`
// path: `list/equipment.lst` -> `list/equipment_indexhash.etc`.
func IndexHashCompanionPath(listPath string) (string, bool) {
	trimmed := strings.Trim(strings.ReplaceAll(strings.TrimSpace(listPath), "\\", "/"), "/")
	if !strings.HasSuffix(strings.ToLower(trimmed), ".lst") {
		return "", false
	}
	base := strings.TrimSuffix(trimmed, trimmed[len(trimmed)-4:])
	return base + "_indexhash.etc", true
}

// indexHashRecord is one decoded pair together with its original encoding, so
// untouched entries can be re-emitted verbatim.
type indexHashRecord struct {
	id       uint32
	value    uint32
	hasValue bool
	idToken  batchToken
	valToken batchToken
}

// IndexHashPairs decodes a `*_indexhash.etc` payload. Entries whose second token
// is not a decimal number are reported with hasValue false.
func (a *Archive) IndexHashPairs(path string) ([]IndexHashPair, error) {
	index, ok := a.Find(path)
	if !ok {
		return nil, fmt.Errorf("pvf: %s not found", path)
	}
	records, err := a.indexHashRecords(index)
	if err != nil {
		return nil, err
	}
	out := make([]IndexHashPair, 0, len(records))
	for _, record := range records {
		out = append(out, IndexHashPair{ID: record.id, Value: record.value})
	}
	return out, nil
}

func (a *Archive) indexHashRecords(index int32) ([]indexHashRecord, error) {
	if index < 0 || index >= int32(len(a.items)) {
		return nil, ErrBadIndex
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		return nil, err
	}
	if len(raw)%10 != 0 {
		return nil, fmt.Errorf("pvf: malformed index-hash payload (%d bytes)", len(raw))
	}
	records := make([]indexHashRecord, 0, len(raw)/10)
	for pos := 0; pos+10 <= len(raw); pos += 10 {
		idType, valType := raw[pos], raw[pos+5]
		if idType != 0 || (valType != 6 && valType != 5 && valType != 7 && valType != 8 && valType != 10) {
			return nil, fmt.Errorf("pvf: unsupported index-hash token pair %d/%d", idType, valType)
		}
		record := indexHashRecord{
			id:       binary.LittleEndian.Uint32(raw[pos+1:]),
			idToken:  batchToken{typ: idType, value: int32(binary.LittleEndian.Uint32(raw[pos+1:]))},
			valToken: batchToken{typ: valType, value: int32(binary.LittleEndian.Uint32(raw[pos+6:]))},
		}
		if value, err := strconv.ParseUint(strings.TrimSpace(a.ResolveString(record.valToken.value)), 10, 32); err == nil {
			record.value = uint32(value)
			record.hasValue = true
		}
		records = append(records, record)
	}
	return records, nil
}

// SetIndexHashEntry inserts or updates one `id -> value` entry, writing the
// value the way the shipped files do: as decimal text referenced by a string
// token. An existing entry keeps its position while its value token is
// canonicalized to the UTF-16 pool used by Paged110.
func (a *Archive) SetIndexHashEntry(indexHashPath string, id, value uint32) error {
	index, ok := a.Find(indexHashPath)
	if !ok {
		return fmt.Errorf("pvf: %s not found", indexHashPath)
	}
	records, err := a.indexHashRecords(index)
	if err != nil {
		return err
	}
	valToken := a.indexHashValueToken(value)
	for i := range records {
		if records[i].id != id {
			continue
		}
		records[i].value = value
		records[i].hasValue = true
		records[i].valToken = valToken
		return a.writeIndexHashRecords(index, records)
	}
	records = append(records, indexHashRecord{
		id:       id,
		value:    value,
		hasValue: true,
		idToken:  batchToken{typ: 0, value: int32(id)},
		valToken: valToken,
	})
	return a.writeIndexHashRecords(index, records)
}

// SetIndexHashEntryForID inserts or updates an index-hash entry using the
// client-generated value for id.
func (a *Archive) SetIndexHashEntryForID(indexHashPath string, id uint32) error {
	return a.SetIndexHashEntry(indexHashPath, id, IndexHashValue(id))
}

// IndexHashIDsNeedingUpdate returns requested ids that are missing or whose
// value is not in the canonical 110US form. Paged110 keeps these decimal
// strings in the UTF-16 pool even when they contain ASCII only; accepting an
// existing even offset would preserve a file that the client can parse but
// does not consume correctly.
func (a *Archive) IndexHashIDsNeedingUpdate(indexHashPath string, ids []uint32) ([]uint32, error) {
	index, ok := a.Find(indexHashPath)
	if !ok {
		return nil, fmt.Errorf("pvf: %s not found", indexHashPath)
	}
	records, err := a.indexHashRecords(index)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint32]indexHashRecord, len(records))
	for _, record := range records {
		if _, exists := byID[record.id]; !exists {
			byID[record.id] = record
		}
	}
	seen := make(map[uint32]struct{}, len(ids))
	result := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		record, exists := byID[id]
		if !exists || !record.hasValue || record.value != IndexHashValue(id) ||
			record.valToken.typ != 6 || record.valToken.value&1 == 0 {
			result = append(result, id)
		}
	}
	return result, nil
}

// SetIndexHashEntriesForIDs inserts or updates several generated entries in a
// single decode/re-encode pass. This matters for the large retail index files,
// where parsing the same payload once per id would be needlessly expensive.
func (a *Archive) SetIndexHashEntriesForIDs(indexHashPath string, ids []uint32) error {
	return a.setIndexHashEntries(indexHashPath, ids, nil)
}

// SetIndexHashEntriesForListIDs inserts or updates generated entries while
// preserving the order of the corresponding .lst. The shipped files contain
// historical rows that are not in the current list, so a new row belongs after
// the preceding live list row and before trailing historical rows, rather than
// blindly at the end of the index file.
func (a *Archive) SetIndexHashEntriesForListIDs(indexHashPath, listPath string, ids []uint32) error {
	listIndex, ok := a.FindList(listPath)
	if !ok {
		return fmt.Errorf("pvf: %s not found", listPath)
	}
	pairs, err := a.ListPairs(listIndex)
	if err != nil {
		return err
	}
	listOrder := make(map[uint32]int, len(pairs))
	for position, pair := range pairs {
		id, parseErr := strconv.ParseUint(strings.TrimSpace(pair.ID), 10, 32)
		if parseErr != nil {
			continue
		}
		if _, exists := listOrder[uint32(id)]; !exists {
			listOrder[uint32(id)] = position
		}
	}
	return a.setIndexHashEntries(indexHashPath, ids, listOrder)
}

func (a *Archive) setIndexHashEntries(indexHashPath string, ids []uint32, listOrder map[uint32]int) error {
	index, ok := a.Find(indexHashPath)
	if !ok {
		return fmt.Errorf("pvf: %s not found", indexHashPath)
	}
	if len(ids) == 0 {
		return nil
	}
	records, err := a.indexHashRecords(index)
	if err != nil {
		return err
	}
	positions := make(map[uint32]int, len(records))
	for i, record := range records {
		if _, exists := positions[record.id]; !exists {
			positions[record.id] = i
		}
	}
	seen := make(map[uint32]struct{}, len(ids))
	additions := make([]indexHashRecord, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		value := IndexHashValue(id)
		valToken := a.indexHashValueToken(value)
		if position, exists := positions[id]; exists {
			records[position].value = value
			records[position].hasValue = true
			records[position].valToken = valToken
			continue
		}
		additions = append(additions, indexHashRecord{
			id:       id,
			value:    value,
			hasValue: true,
			idToken:  batchToken{typ: 0, value: int32(id)},
			valToken: valToken,
		})
	}
	if len(additions) > 0 {
		sort.SliceStable(additions, func(i, j int) bool {
			left, leftKnown := listOrder[additions[i].id]
			right, rightKnown := listOrder[additions[j].id]
			if leftKnown != rightKnown {
				return leftKnown
			}
			if leftKnown && left != right {
				return left < right
			}
			return false
		})
		for _, addition := range additions {
			insertAt := len(records)
			if targetPosition, known := listOrder[addition.id]; known {
				lastLive := -1
				for position, record := range records {
					recordPosition, live := listOrder[record.id]
					if !live {
						continue
					}
					if recordPosition > targetPosition {
						insertAt = position
						break
					}
					lastLive = position
				}
				if insertAt == len(records) && lastLive >= 0 {
					insertAt = lastLive + 1
				}
			}
			records = append(records, indexHashRecord{})
			copy(records[insertAt+1:], records[insertAt:])
			records[insertAt] = addition
			positions[addition.id] = insertAt
		}
	}
	if err := a.writeIndexHashRecords(index, records); err != nil {
		return err
	}
	return nil
}

func (a *Archive) indexHashValueToken(value uint32) batchToken {
	return batchToken{
		typ:   6,
		value: a.UnicodeStringOffset(strconv.FormatUint(uint64(value), 10)),
	}
}

func (a *Archive) writeIndexHashRecords(index int32, records []indexHashRecord) error {
	raw := make([]byte, 0, len(records)*10)
	for _, record := range records {
		raw = append(raw, encodeBatchTokens([]batchToken{record.idToken, record.valToken})...)
	}
	if err := a.SetRawBytes(index, raw); err != nil {
		return err
	}
	a.InvalidateStringTables()
	return nil
}

// IndexHashGaps reports which ids of a `.lst` have no entry in its companion
// index-hash file. A missing entry is what a newly registered id looks like,
// because only the original tooling ever wrote these files.
func (a *Archive) IndexHashGaps(listPath string) ([]uint32, error) {
	companion, ok := IndexHashCompanionPath(listPath)
	if !ok {
		return nil, fmt.Errorf("pvf: %s is not a .lst path", listPath)
	}
	pairs, err := a.IndexHashPairs(companion)
	if err != nil {
		return nil, err
	}
	have := make(map[uint32]bool, len(pairs))
	for _, pair := range pairs {
		have[pair.ID] = true
	}
	listIndex, ok := a.FindList(listPath)
	if !ok {
		return nil, fmt.Errorf("pvf: %s not found", listPath)
	}
	entries, err := a.ScriptListPairs(listIndex)
	if err != nil {
		return nil, err
	}
	var missing []uint32
	seen := map[uint32]bool{}
	for _, entry := range entries {
		id, err := strconv.ParseUint(strings.TrimSpace(entry.ID), 10, 32)
		if err != nil || seen[uint32(id)] {
			continue
		}
		seen[uint32(id)] = true
		if !have[uint32(id)] {
			missing = append(missing, uint32(id))
		}
	}
	return missing, nil
}

// IndexHashSiblingPaths lists the index-hash files that exist next to a list
// (some families ship a `_v5` variant as well).
func (a *Archive) IndexHashSiblingPaths(listPath string) []string {
	var out []string
	if companion, ok := IndexHashCompanionPath(listPath); ok {
		if _, exists := a.Find(companion); exists {
			out = append(out, companion)
		}
		v5 := strings.TrimSuffix(companion, ".etc") + "_v5.etc"
		if _, exists := a.Find(v5); exists {
			out = append(out, v5)
		}
	}
	return out
}
