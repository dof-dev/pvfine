package pvf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

// Newer scripts do not always carry the display text itself: `.equ` / `.stk`
// name fields hold placeholders of the form
//
//	<31::equip_name_1>
//
// where 31 is an index into the archive's string-table map (`list/n_string.lst`)
// and `equip_name_1` is a key inside that table's `.str` file. The `.str` files
// are UTF-16 text with one `key>value` per line.
//
// Resolution is read-only: the placeholder is what the archive stores, so the
// script text itself is never rewritten. Editing the text behind a placeholder
// therefore edits the `.str` payload instead (SetStringTableEntry).

// stringTableFiles lists, in **decreasing** precedence, the `.lst` files that
// map a table index to its `.str` payload.
//
// The base mapping comes first: `list/n_string.lst` points at the `String/*.uv.str`
// files, which hold the text the client itself displays (in the 110US archive
// that is the Chinese/English localization: 622k keys for `equipment.uv.str`).
// The `*.kor.str` / `*.translate.str` files shipped next to them are language
// overlays (Korean and translated text); ranking those first made item names
// come out Korean even though the base table has Chinese for the same key, so
// they are only consulted for keys the base table leaves empty.
var stringTableFiles = []string{
	"list/n_string.lst",
	"n_string.lst",
	"list/n_string_translate.lst",
	"n_string_translate.lst",
	"list/n_string_kor.lst",
	"n_string_kor.lst",
}

type stringTableState struct {
	once         sync.Once
	paths        map[int][]string                  // table index -> candidate .str paths (highest precedence first)
	cache        map[string]map[string]string      // lowercased path -> key -> value
	valueIndexes map[string]*stringTableValueIndex // lowercased path -> lazy key/value byte index
	fail         map[string]bool                   // paths that could not be read
}

type stringTableValueIndex struct {
	once   sync.Once
	raw    []byte
	values []stringTableValueRange
	valid  bool
}

type stringTableValueRange struct {
	hash     uint64
	keyStart uint32
	keyEnd   uint32
	start    uint32
	end      uint32
}

func (a *Archive) initStringTables() {
	st := &stringTableState{
		paths:        map[int][]string{},
		cache:        map[string]map[string]string{},
		valueIndexes: map[string]*stringTableValueIndex{},
		fail:         map[string]bool{},
	}
	for _, lst := range stringTableFiles {
		for _, entry := range a.stringTableMap(lst) {
			paths := st.paths[entry.index]
			dup := false
			for _, p := range paths {
				if strings.EqualFold(p, entry.path) {
					dup = true
					break
				}
			}
			if !dup {
				st.paths[entry.index] = append(paths, entry.path)
			}
		}
	}
	a.tables.mu.Lock()
	a.tables.state = st
	a.tables.mu.Unlock()
}

// ensureStringTables returns the lazily built table index -> path map.
func (a *Archive) ensureStringTables() *stringTableState {
	a.tables.mu.Lock()
	st := a.tables.state
	a.tables.mu.Unlock()
	if st != nil {
		return st
	}
	a.initStringTables()
	a.tables.mu.Lock()
	defer a.tables.mu.Unlock()
	return a.tables.state
}

type stringTableEntry struct {
	index int
	path  string
}

// stringTableMap reads one index -> path list (`*.lst` files are ordinary
// script token streams of alternating integer and string values).
func (a *Archive) stringTableMap(name string) []stringTableEntry {
	i, ok := a.Find(name)
	if !ok {
		return nil
	}
	if f := a.File(i); f.DataType != TypeScript {
		return nil
	}
	raw, err := a.RawBytes(i)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var out []stringTableEntry
	index := -1
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		typ := raw[pos]
		v := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		switch typ {
		case 0:
			index = int(v)
		case 6, 8, 10:
			if index >= 0 {
				out = append(out, stringTableEntry{index: index, path: a.ResolveString(v)})
				index = -1
			}
		}
	}
	return out
}

// stringTablePaths returns the candidate `.str` files for a table index in
// decreasing precedence: the base localization first, overlays afterwards.
func (a *Archive) stringTablePaths(index int) []string {
	st := a.ensureStringTables()
	if st == nil {
		return nil
	}
	a.tables.mu.Lock()
	defer a.tables.mu.Unlock()
	return append([]string(nil), st.paths[index]...)
}

// StringTablePaths returns the candidate archive paths that can answer one
// string-table index, ordered by the same precedence used for placeholder
// resolution.
func (a *Archive) StringTablePaths(index int) []string {
	return a.stringTablePaths(index)
}

// loadStringTable parses one `.str` payload (UTF-16LE, `key>value` lines,
// `//` comments) and caches the result. Localized siblings of the universal
// file are preferred when the listed path is missing.
func (a *Archive) loadStringTable(path string) map[string]string {
	st := a.ensureStringTables()
	if st == nil {
		return nil
	}
	a.tables.mu.Lock()
	key := strings.ToLower(path)
	if tbl, ok := st.cache[key]; ok {
		a.tables.mu.Unlock()
		return tbl
	}
	if st.fail[key] {
		a.tables.mu.Unlock()
		return nil
	}
	a.tables.mu.Unlock()

	tbl := a.readStringTable(path)
	if tbl == nil {
		// Try the localized siblings of a `*.uv.str` universal file.
		base := path
		if i := strings.LastIndexByte(base, '.'); i > 0 {
			base = base[:i]
		}
		for _, suffix := range []string{".translate.str", ".kor.str", ".uv.str", ".str"} {
			cand := base + suffix
			if strings.EqualFold(cand, path) {
				continue
			}
			if tbl = a.readStringTable(cand); tbl != nil {
				break
			}
		}
	}

	a.tables.mu.Lock()
	if a.tables.state == st {
		if tbl == nil {
			st.fail[key] = true
		} else {
			st.cache[key] = tbl
		}
	}
	a.tables.mu.Unlock()
	return tbl
}

// lookupStringTableValue resolves keys through a lazily built compact index.
// It scans a large UTF-16 table once without decoding and retaining every key
// and value as separate Go strings.
func (a *Archive) lookupStringTableValue(path, key string) (string, bool) {
	st := a.ensureStringTables()
	if st == nil {
		return "", false
	}
	cacheKey := strings.ToLower(path)

	a.tables.mu.Lock()
	if table, ok := st.cache[cacheKey]; ok {
		value, found := table[key]
		a.tables.mu.Unlock()
		return value, found
	}
	if st.fail[cacheKey] {
		a.tables.mu.Unlock()
		return "", false
	}
	index := st.valueIndexes[cacheKey]
	if index == nil {
		index = &stringTableValueIndex{}
		st.valueIndexes[cacheKey] = index
	}
	a.tables.mu.Unlock()

	index.once.Do(func() {
		index.raw, index.values, index.valid = a.readStringTableValueIndex(path)
	})
	if !index.valid {
		return "", false
	}
	valueRange, found := lookupStringTableValueRange(index.raw, index.values, key)
	if !found {
		return "", false
	}
	return decodeUTF16(index.raw[int(valueRange.start):int(valueRange.end)]), true
}

// readStringTableValueIndex indexes the listed payload and, only when it is
// missing or malformed, tries the localized siblings used by loadStringTable.
func (a *Archive) readStringTableValueIndex(path string) ([]byte, []stringTableValueRange, bool) {
	candidates := []string{path}
	base := path
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	for _, suffix := range []string{".translate.str", ".kor.str", ".uv.str", ".str"} {
		candidate := base + suffix
		if strings.EqualFold(candidate, path) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	for _, candidate := range candidates {
		index, ok := a.Find(candidate)
		if !ok {
			continue
		}
		raw, err := a.RawBytes(index)
		if err != nil || len(raw) < 2 {
			continue
		}
		values, valid := indexStringTableValues(raw)
		if valid {
			return raw, values, true
		}
	}
	return nil, nil, false
}

func findStringTableValue(raw []byte, key string) (string, bool) {
	values, valid := indexStringTableValues(raw)
	if !valid {
		return "", false
	}
	valueRange, found := lookupStringTableValueRange(raw, values, key)
	if !found {
		return "", false
	}
	return decodeUTF16(raw[int(valueRange.start):int(valueRange.end)]), true
}

func indexStringTableValues(raw []byte) ([]stringTableValueRange, bool) {
	if len(raw) < 2 {
		return nil, false
	}
	values := make([]stringTableValueRange, 0, len(raw)/80)
	valid := false
	for lineStart := 0; lineStart+1 < len(raw); {
		lineEnd, next, more := utf16StringTableLineBounds(raw, lineStart)
		matchStart := lineStart
		if lineStart == 0 && len(raw) >= 2 && raw[0] == 0xff && raw[1] == 0xfe {
			matchStart = 2
		}
		contentEnd := lineEnd
		if contentEnd >= matchStart+2 && raw[contentEnd-2] == '\r' && raw[contentEnd-1] == 0 {
			contentEnd -= 2
		}
		if contentEnd >= matchStart+2 && !utf16StringTableComment(raw, matchStart, contentEnd) {
			separator := -1
			for at := matchStart; at+1 < contentEnd; at += 2 {
				if raw[at] == '>' && raw[at+1] == 0 {
					separator = at
					break
				}
			}
			if separator > matchStart {
				valid = true
				valueStart := separator + 2
				for contentEnd >= valueStart+2 && raw[contentEnd-2] == '\r' && raw[contentEnd-1] == 0 {
					contentEnd -= 2
				}
				key := raw[matchStart:separator]
				values = append(values, stringTableValueRange{
					hash:     hashUTF16Bytes(key),
					keyStart: uint32(matchStart),
					keyEnd:   uint32(separator),
					start:    uint32(valueStart),
					end:      uint32(contentEnd),
				})
			}
		}
		if !more {
			break
		}
		lineStart = next
	}
	slices.SortFunc(values, func(left, right stringTableValueRange) int {
		switch {
		case left.hash < right.hash:
			return -1
		case left.hash > right.hash:
			return 1
		default:
			return 0
		}
	})
	return values, valid
}

func lookupStringTableValueRange(raw []byte, values []stringTableValueRange, key string) (stringTableValueRange, bool) {
	hash := hashUTF16String(key)
	start, _ := slices.BinarySearchFunc(values, hash, func(value stringTableValueRange, target uint64) int {
		switch {
		case value.hash < target:
			return -1
		case value.hash > target:
			return 1
		default:
			return 0
		}
	})
	if start >= len(values) || values[start].hash != hash {
		return stringTableValueRange{}, false
	}
	var found stringTableValueRange
	matched := false
	for i := start; i < len(values) && values[i].hash == hash; i++ {
		candidate := values[i]
		if equalUTF16String(raw[int(candidate.keyStart):int(candidate.keyEnd)], key) && (!matched || candidate.keyStart > found.keyStart) {
			found = candidate
			matched = true
		}
	}
	return found, matched
}

func hashUTF16Bytes(raw []byte) uint64 {
	hash := uint64(14695981039346656037)
	for i := 0; i+1 < len(raw); i += 2 {
		hash ^= uint64(raw[i])
		hash *= 1099511628211
		hash ^= uint64(raw[i+1])
		hash *= 1099511628211
	}
	return hash
}

func hashUTF16String(value string) uint64 {
	hash := uint64(14695981039346656037)
	for _, r := range value {
		if r <= 0xffff {
			hash ^= uint64(byte(r))
			hash *= 1099511628211
			hash ^= uint64(byte(r >> 8))
			hash *= 1099511628211
			continue
		}
		hi, lo := utf16.EncodeRune(r)
		for _, unit := range [...]uint16{uint16(hi), uint16(lo)} {
			hash ^= uint64(byte(unit))
			hash *= 1099511628211
			hash ^= uint64(byte(unit >> 8))
			hash *= 1099511628211
		}
	}
	return hash
}

func equalUTF16String(raw []byte, value string) bool {
	at := 0
	for _, r := range value {
		if r <= 0xffff {
			if at+1 >= len(raw) || binary.LittleEndian.Uint16(raw[at:]) != uint16(r) {
				return false
			}
			at += 2
			continue
		}
		hi, lo := utf16.EncodeRune(r)
		if at+3 >= len(raw) || binary.LittleEndian.Uint16(raw[at:]) != uint16(hi) || binary.LittleEndian.Uint16(raw[at+2:]) != uint16(lo) {
			return false
		}
		at += 4
	}
	return at == len(raw)
}

func utf16StringTableLineBounds(raw []byte, start int) (end, next int, more bool) {
	for at := start; at+1 < len(raw); at += 2 {
		if raw[at] == 0 && raw[at+1] == 0 {
			return at, len(raw), false
		}
		if raw[at] != '\n' || raw[at+1] != 0 {
			continue
		}
		end = at
		if at >= start+2 && raw[at-2] == '\r' && raw[at-1] == 0 {
			end -= 2
		}
		next = at + 2
		return end, next, next < len(raw)
	}
	return len(raw) &^ 1, len(raw), false
}

func utf16StringTableComment(raw []byte, start, end int) bool {
	return end-start >= 4 && raw[start] == '/' && raw[start+1] == 0 && raw[start+2] == '/' && raw[start+3] == 0
}

func (a *Archive) readStringTable(path string) map[string]string {
	i, ok := a.Find(path)
	if !ok {
		return nil
	}
	raw, err := a.RawBytes(i)
	if err != nil || len(raw) < 2 {
		return nil
	}
	text := decodeUTF16(raw)
	tbl := make(map[string]string, 1024)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		sep := strings.IndexByte(line, '>')
		if sep <= 0 {
			continue
		}
		tbl[line[:sep]] = line[sep+1:]
	}
	if len(tbl) == 0 {
		return nil
	}
	return tbl
}

func decodeUTF16(raw []byte) string {
	u16 := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u16 = append(u16, binary.LittleEndian.Uint16(raw[i:]))
	}
	s := string(utf16.Decode(u16))
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return s
}

// StringTableResolution is one resolved `<table::key>` placeholder.
type StringTableResolution struct {
	Text   string // the resolved text
	Source string // the `.str` file that answered
	// Fallback is true when the archive's own localization (the base
	// `list/n_string.lst` table) has no text for the key, so a language overlay
	// answered instead. The text is then the Korean/translated original rather
	// than what this client displays.
	Fallback bool
}

// ResolveStringTable resolves one table index + key, consulting the candidate
// files from the base localization to the language overlays.
//
// A candidate that carries the key with an empty value does not win: the base
// `*.uv.str` files list every known key, including the ~37% of equipment names
// the localization left untranslated as `key=`, and stopping there would blank
// out names that an overlay can still answer.
func (a *Archive) ResolveStringTable(index int, key string) (StringTableResolution, bool) {
	for i, path := range a.stringTablePaths(index) {
		if v, ok := a.lookupStringTableValue(path, key); ok && v != "" {
			return StringTableResolution{Text: v, Source: path, Fallback: i > 0}, true
		}
	}
	return StringTableResolution{}, false
}

// LookupStringTable resolves one table index + key to its text, ignoring where
// the text came from.
func (a *Archive) LookupStringTable(index int, key string) (string, bool) {
	res, ok := a.ResolveStringTable(index, key)
	return res.Text, ok
}

// LookupStringTableText keeps normal localization precedence but distinguishes
// a defined empty value from a missing key.
func (a *Archive) LookupStringTableText(index int, key string) (string, bool) {
	defined := false
	for _, path := range a.stringTablePaths(index) {
		if value, found := a.lookupStringTableValue(path, key); found {
			if value != "" {
				return value, true
			}
			defined = true
		}
	}
	return "", defined
}

// ResolvePlaceholder replaces a single `<table::key>` placeholder. It returns
// the input unchanged when it is not a placeholder or cannot be resolved.
func (a *Archive) ResolvePlaceholder(text string) string {
	index, key, ok := parsePlaceholder(text)
	if !ok {
		return text
	}
	if v, ok := a.LookupStringTable(index, key); ok {
		return v
	}
	return text
}

// ResolvePlaceholders replaces every `<table::key>` placeholder embedded in
// text. Unresolvable placeholders are kept verbatim so no information is lost.
func (a *Archive) ResolvePlaceholders(text string) string {
	resolved, _ := a.resolvePlaceholders(text, "")
	return resolved
}

// ResolvePlaceholdersMarked is ResolvePlaceholders with `fallbackSuffix`
// appended to every value that only a language overlay could answer, letting a
// caller flag text the archive's own localization does not contain. An empty
// suffix behaves exactly like ResolvePlaceholders.
func (a *Archive) ResolvePlaceholdersMarked(text, fallbackSuffix string) string {
	resolved, _ := a.resolvePlaceholders(text, fallbackSuffix)
	return resolved
}

// resolvePlaceholders replaces placeholders and reports whether any of them was
// answered by a language overlay rather than by the archive's own localization.
func (a *Archive) resolvePlaceholders(text, fallbackSuffix string) (string, bool) {
	if !strings.Contains(text, "<") || !strings.Contains(text, "::") {
		return text, false
	}
	fallback := false
	var sb strings.Builder
	sb.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] != '<' {
			sb.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], '>')
		if end < 0 {
			sb.WriteString(text[i:])
			break
		}
		cand := text[i : i+end+1]
		if index, key, ok := parsePlaceholder(cand); ok {
			if res, found := a.ResolveStringTable(index, key); found {
				sb.WriteString(res.Text)
				if res.Fallback {
					fallback = true
					if fallbackSuffix != "" {
						sb.WriteString(fallbackSuffix)
					}
				}
				i += end + 1
				continue
			}
		}
		sb.WriteString(cand)
		i += end + 1
	}
	return sb.String(), fallback
}

// ParsePlaceholder splits `<31::equip_name_1>` into its table index and key. It
// reports false for any text that is not a placeholder.
func ParsePlaceholder(text string) (int, string, bool) {
	return parsePlaceholder(text)
}

// parsePlaceholder splits `<31::equip_name_1>` into its table index and key.
func parsePlaceholder(text string) (int, string, bool) {
	if len(text) < 6 || text[0] != '<' || text[len(text)-1] != '>' {
		return 0, "", false
	}
	inner := text[1 : len(text)-1]
	sep := strings.Index(inner, "::")
	if sep <= 0 {
		return 0, "", false
	}
	index, err := strconv.Atoi(strings.TrimSpace(inner[:sep]))
	if err != nil {
		return 0, "", false
	}
	key := strings.TrimSpace(inner[sep+2:])
	if key == "" {
		return 0, "", false
	}
	return index, key, true
}

// ItemName returns the display name of a script entry: the value of its
// top-level `[name]` section with placeholders resolved. It reports false when
// the file has no such section.
func (a *Archive) ItemName(i int32) (string, bool) {
	f := a.File(i)
	if f.DataType != TypeScript {
		return "", false
	}
	raw, err := a.RawBytes(i)
	if err != nil || len(raw) == 0 {
		return "", false
	}
	name := ""
	found := false
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		typ := raw[pos]
		v := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		if typ == 3 {
			// A top-level [name] opener; a nested one is reset by the label.
			if strings.EqualFold(a.ResolveString(v), "[name]") {
				found = true
				name = ""
				continue
			}
			// Any other label ends the search once a value was seen.
			if found && name != "" {
				break
			}
			continue
		}
		if !found {
			continue
		}
		switch typ {
		case 6, 8, 10:
			name = a.ResolvePlaceholders(a.ResolveString(v))
		case 5, 7:
			name = a.ResolvePlaceholders(a.ResolveString(v))
		}
		if name != "" {
			return name, true
		}
	}
	if name != "" {
		return name, true
	}
	return "", false
}

// ErrStringTableUnreadable reports that the `.str` payload behind a table index
// is missing or is not a `key>value` UTF-16 table, so it is never written to.
var ErrStringTableUnreadable = errors.New("pvf: string table payload is missing or malformed")

// InvalidateStringTables drops the parsed string-table cache so the next lookup
// re-reads the payloads. Call it after editing a table.
func (a *Archive) InvalidateStringTables() {
	a.tables.mu.Lock()
	a.tables.state = nil
	a.tables.mu.Unlock()
}

// SetStringTableEntry rewrites the value of one `<table::key>` entry and returns
// the `.str` path that was edited.
//
// The entry is edited where it currently lives — the first table that has the
// key, which may be a language overlay rather than the base file. A key no table
// knows is appended to the base table so new text can be created.
//
// The payload is spliced at the byte level (UTF-16LE): only the value of the
// matching line changes, while its terminator, a possible BOM and every other
// byte stay exactly as they were. Unparseable payloads are refused instead of
// being overwritten.
func (a *Archive) SetStringTableEntry(tableIndex int, key, value string) (string, error) {
	index, ok := a.StringTableEntryIndex(tableIndex, key)
	if !ok {
		paths := a.stringTablePaths(tableIndex)
		if len(paths) == 0 {
			return "", fmt.Errorf("pvf: no string table is mapped to index %d", tableIndex)
		}
		return "", fmt.Errorf("%w: %s", ErrStringTableUnreadable, paths[0])
	}
	if err := a.SetStringTableEntryAt(index, key, value); err != nil {
		return "", err
	}
	return a.Path(index), nil
}

// StringTableEntryIndex reports which file entry SetStringTableEntry edits for
// this table index and key: the first table that carries the key, or the base
// table when no table knows it yet.
func (a *Archive) StringTableEntryIndex(tableIndex int, key string) (int32, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, false
	}
	paths := a.stringTablePaths(tableIndex)
	if len(paths) == 0 {
		return 0, false
	}
	target := paths[0]
	for _, path := range paths {
		table := a.loadStringTable(path)
		if table == nil {
			continue
		}
		if _, ok := table[key]; ok {
			target = path
			break
		}
	}
	if a.loadStringTable(target) == nil {
		return 0, false
	}
	return a.stringTableFileIndex(target)
}

// SetStringTableEntryAt rewrites one `key>value` line of an already resolved
// `.str` file entry. See SetStringTableEntry.
func (a *Archive) SetStringTableEntryAt(index int32, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("pvf: empty string table key")
	}
	if index < 0 || index >= int32(len(a.items)) {
		return ErrBadIndex
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		return err
	}
	updated, err := spliceStringTableValue(raw, key, value)
	if err != nil {
		return err
	}
	if err := a.SetRawBytes(index, updated); err != nil {
		return err
	}
	a.InvalidateStringTables()
	return nil
}

// stringTableFileIndex resolves the file entry behind a table path, accepting
// the localized siblings loadStringTable falls back to.
func (a *Archive) stringTableFileIndex(path string) (int32, bool) {
	if index, ok := a.Find(path); ok {
		return index, true
	}
	base := path
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	for _, suffix := range []string{".translate.str", ".kor.str", ".uv.str", ".str"} {
		if index, ok := a.Find(base + suffix); ok {
			return index, true
		}
	}
	return 0, false
}

// spliceStringTableValue replaces the value of one `key>value` line, or appends
// the line when the key is not present yet.
func spliceStringTableValue(raw []byte, key, value string) ([]byte, error) {
	if len(raw) < 2 {
		return nil, fmt.Errorf("%w: payload too short", ErrStringTableUnreadable)
	}
	pattern := utf16LEBytes(key + ">")
	search := 0
	for search < len(raw) {
		rel := bytes.Index(raw[search:], pattern)
		if rel < 0 {
			break
		}
		at := search + rel
		if !utf16LineStart(raw, at) {
			search = at + 2
			continue
		}
		valueStart := at + len(pattern)
		valueEnd, ok := utf16LineEnd(raw, valueStart)
		if !ok {
			break
		}
		out := make([]byte, 0, len(raw)+len(value)*2)
		out = append(out, raw[:valueStart]...)
		out = append(out, utf16LEBytes(value)...)
		out = append(out, raw[valueEnd:]...)
		return out, nil
	}

	// Not present yet: append a line before any trailing NUL padding. The reader
	// stops at the first NUL character, so text written after the padding would
	// never be seen.
	content := len(raw)
	for content >= 2 && raw[content-1] == 0 && raw[content-2] == 0 {
		content -= 2
	}
	body, padding := raw[:content], raw[content:]
	terminator := dominantUTF16Terminator(body)
	out := make([]byte, 0, len(raw)+len(pattern)+len(value)*2+len(terminator))
	out = append(out, body...)
	if !utf16LineStart(out, len(out)) {
		out = append(out, terminator...)
	}
	out = append(out, pattern...)
	out = append(out, utf16LEBytes(value)...)
	out = append(out, terminator...)
	out = append(out, padding...)
	return out, nil
}

// utf16LineStart reports whether offset at begins a line of a UTF-16LE payload.
func utf16LineStart(raw []byte, at int) bool {
	if at == 0 || at%2 != 0 {
		return at == 0
	}
	if raw[at-1] != 0 {
		return false
	}
	if raw[at-2] == '\n' {
		return true
	}
	return at >= 4 && raw[at-4] == '\r' && raw[at-3] == 0 && raw[at-2] == '\n'
}

// utf16LineEnd returns the offset where the value starting at from ends,
// excluding its `\r\n` / `\n` terminator. A NUL character ends the value too:
// the payload may hold an unterminated last line followed by NUL padding.
// from must be even.
func utf16LineEnd(raw []byte, from int) (int, bool) {
	for i := from; i+1 < len(raw); i += 2 {
		if raw[i] == 0 && raw[i+1] == 0 {
			return i, true
		}
		if raw[i] != '\n' || raw[i+1] != 0 {
			continue
		}
		if i >= 2 && raw[i-2] == '\r' && raw[i-1] == 0 {
			return i - 2, true
		}
		return i, true
	}
	return 0, false
}

// dominantUTF16Terminator returns the line terminator the payload uses: CRLF
// when any CRLF is present, LF otherwise.
func dominantUTF16Terminator(raw []byte) []byte {
	for i := 2; i+1 < len(raw); i += 2 {
		if raw[i-2] == '\r' && raw[i-1] == 0 && raw[i] == '\n' && raw[i+1] == 0 {
			return []byte{'\r', 0, '\n', 0}
		}
	}
	return []byte{'\n', 0}
}

// utf16LEBytes is the UTF-16LE encoding of s, including surrogate pairs.
func utf16LEBytes(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
