package pvf

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ListPair is one id/path pair from a .lst script.
type ListPair struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// listRecord is one decoded id/path token pair together with its original
// tokens. Unchanged records are re-emitted verbatim so token encoding and
// string-pool offsets stay stable across an edit.
type listRecord struct {
	id        string
	path      string
	idToken   batchToken
	pathToken batchToken
}

// ScriptImageReference is the raw PVF representation used by [icon] and
// [field image]. Services map it to their public ImageReference type.
type ScriptImageReference struct {
	Path  string `json:"path"`
	Index int32  `json:"index"`
}

// ScriptMetadata is the small metadata projection shared by name/search and
// image rendering. It intentionally does not decompile the full script.
type ScriptMetadata struct {
	Name       string                `json:"name,omitempty"`
	HasName    bool                  `json:"-"`
	Icon       *ScriptImageReference `json:"icon,omitempty"`
	FieldImage *ScriptImageReference `json:"fieldImage,omitempty"`
}

// ScriptListPairs parses a TypeScript .lst payload as consecutive id/path
// token pairs. The .lst line layout is presentation-only, so token pairs are
// used instead of trying to recover source lines from the binary payload.
func (a *Archive) ScriptListPairs(i int32) ([]ListPair, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return nil, ErrBadIndex
	}
	if a.items[i].typ != TypeScript {
		return nil, fmt.Errorf("pvf: file %d is not a script", i)
	}
	raw, err := a.RawBytes(i)
	if err != nil {
		return nil, err
	}
	if len(raw)%5 != 0 {
		return nil, fmt.Errorf("pvf: malformed script payload for file %d", i)
	}

	values := make([]scriptMetadataValue, 0, len(raw)/5)
	invalid := false
	for pos := 0; pos < len(raw); pos += 5 {
		value, ok := a.scriptMetadataValue(raw[pos], int32(binary.LittleEndian.Uint32(raw[pos+1:])))
		if !ok {
			invalid = true
		}
		values = append(values, value)
	}
	if invalid || len(values)%2 != 0 {
		return nil, fmt.Errorf("pvf: incomplete or unsupported .lst pair data for file %d", i)
	}

	pairs := make([]ListPair, 0, len(values)/2)
	for pos := 0; pos+1 < len(values); pos += 2 {
		id := strings.TrimSpace(values[pos].text)
		path := strings.TrimSpace(values[pos+1].text)
		if id == "" || path == "" {
			continue
		}
		pairs = append(pairs, ListPair{ID: id, Path: path})
	}
	return pairs, nil
}

// RemoveListPairs removes matching id/path pairs from a TypeScript .lst
// payload while preserving the original token encoding of every remaining
// pair. Duplicate matching pairs are removed together.
func (a *Archive) RemoveListPairs(i int32, entries []ListPair) (int, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return 0, ErrBadIndex
	}
	if a.items[i].typ != TypeScript {
		return 0, fmt.Errorf("pvf: file %d is not a script", i)
	}
	if len(entries) == 0 {
		return 0, nil
	}

	targets := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		path := strings.TrimSpace(entry.Path)
		if id == "" || path == "" {
			continue
		}
		targets[id+"\x00"+path] = struct{}{}
	}
	if len(targets) == 0 {
		return 0, nil
	}

	raw, err := a.RawBytes(i)
	if err != nil {
		return 0, err
	}
	if len(raw)%5 != 0 {
		return 0, fmt.Errorf("pvf: malformed script payload for file %d", i)
	}
	values := make([]scriptMetadataValue, 0, len(raw)/5)
	for pos := 0; pos < len(raw); pos += 5 {
		value, ok := a.scriptMetadataValue(raw[pos], int32(binary.LittleEndian.Uint32(raw[pos+1:])))
		if !ok {
			return 0, fmt.Errorf("pvf: incomplete or unsupported .lst pair data for file %d", i)
		}
		values = append(values, value)
	}
	if len(values)%2 != 0 {
		return 0, fmt.Errorf("pvf: incomplete or unsupported .lst pair data for file %d", i)
	}

	result := make([]byte, 0, len(raw))
	removed := 0
	for pos := 0; pos < len(values); pos += 2 {
		id := strings.TrimSpace(values[pos].text)
		path := strings.TrimSpace(values[pos+1].text)
		if _, ok := targets[id+"\x00"+path]; ok && id != "" && path != "" {
			removed++
			continue
		}
		result = append(result, raw[pos*5:(pos+2)*5]...)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := a.SetRawBytes(i, result); err != nil {
		return 0, err
	}
	return removed, nil
}

// decodeListRecords decodes a .lst payload into records. Every token must be
// a valid id/path member and the token count must be even.
func (a *Archive) decodeListRecords(i int32, raw []byte) ([]listRecord, error) {
	if len(raw)%5 != 0 {
		return nil, fmt.Errorf("pvf: malformed script payload for file %d", i)
	}
	tokens := make([]batchToken, 0, len(raw)/5)
	values := make([]scriptMetadataValue, 0, len(raw)/5)
	for pos := 0; pos < len(raw); pos += 5 {
		value, ok := a.scriptMetadataValue(raw[pos], int32(binary.LittleEndian.Uint32(raw[pos+1:])))
		if !ok {
			return nil, fmt.Errorf("pvf: incomplete or unsupported .lst pair data for file %d", i)
		}
		tokens = append(tokens, batchToken{
			typ:   raw[pos],
			value: int32(binary.LittleEndian.Uint32(raw[pos+1:])),
		})
		values = append(values, value)
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("pvf: incomplete or unsupported .lst pair data for file %d", i)
	}

	records := make([]listRecord, 0, len(values)/2)
	for pos := 0; pos+1 < len(values); pos += 2 {
		records = append(records, listRecord{
			id:        strings.TrimSpace(values[pos].text),
			path:      strings.TrimSpace(values[pos+1].text),
			idToken:   tokens[pos],
			pathToken: tokens[pos+1],
		})
	}
	return records, nil
}

// listPathToken encodes a .lst entry path as the backtick-quoted string token
// used by real list files.
func (a *Archive) listPathToken(path string) batchToken {
	return batchToken{typ: 6, value: a.StringOffset(path)}
}

// listIDToken encodes a list id. Numeric ids keep the integer token used by
// real .lst files; anything else falls back to a quoted string.
func (a *Archive) listIDToken(id string) batchToken {
	if number, err := strconv.ParseInt(id, 10, 32); err == nil {
		return batchToken{typ: 0, value: int32(number)}
	}
	return batchToken{typ: 6, value: a.StringOffset(id)}
}

func encodeListRecords(records []listRecord) []byte {
	raw := make([]byte, 0, len(records)*10)
	for _, record := range records {
		raw = append(raw, encodeBatchTokens([]batchToken{record.idToken, record.pathToken})...)
	}
	return raw
}

// ListPairs returns the id/path pairs of a .lst payload in file order. When an
// id repeats, the first occurrence wins.
func (a *Archive) ListPairs(i int32) ([]ListPair, error) {
	records, err := a.listRecordsAt(i)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(records))
	pairs := make([]ListPair, 0, len(records))
	for _, record := range records {
		if record.id == "" || record.path == "" {
			continue
		}
		if _, duplicate := seen[record.id]; duplicate {
			continue
		}
		seen[record.id] = struct{}{}
		pairs = append(pairs, ListPair{ID: record.id, Path: record.path})
	}
	return pairs, nil
}

func (a *Archive) listRecordsAt(i int32) ([]listRecord, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return nil, ErrBadIndex
	}
	if a.items[i].typ != TypeScript {
		return nil, fmt.Errorf("pvf: file %d is not a script", i)
	}
	raw, err := a.RawBytes(i)
	if err != nil {
		return nil, err
	}
	return a.decodeListRecords(i, raw)
}

// SetListPairs inserts or updates id/path pairs in a .lst payload. An existing
// id keeps its position and id-token encoding while its path is replaced; a
// new id is appended. Duplicate records for an id are collapsed into the first
// one, so an id addresses exactly one entry afterwards.
func (a *Archive) SetListPairs(i int32, pairs []ListPair) error {
	if len(pairs) == 0 {
		return nil
	}
	records, err := a.listRecordsAt(i)
	if err != nil {
		return err
	}

	// Match by trimmed id, mirroring how ListPairs reads the file. The first
	// occurrence wins, so duplicates for the same id collapse into it.
	firstByID := make(map[string]int, len(records))
	for position, record := range records {
		if _, exists := firstByID[record.id]; !exists {
			firstByID[record.id] = position
		}
	}

	requested := make(map[string]listRecord, len(pairs))
	appended := make([]listRecord, 0, len(pairs))
	for _, pair := range pairs {
		id := strings.TrimSpace(pair.ID)
		path := strings.TrimSpace(pair.Path)
		if id == "" || path == "" {
			return fmt.Errorf("pvf: 列表条目的 id 和路径不能为空")
		}
		record := listRecord{
			id:        id,
			path:      path,
			idToken:   a.listIDToken(id),
			pathToken: a.listPathToken(path),
		}
		position, exists := firstByID[id]
		if !exists {
			firstByID[id] = len(records) + len(appended)
			appended = append(appended, record)
			continue
		}
		// An updated entry keeps the original id token so its encoding and
		// string-pool offset are untouched.
		record.idToken = records[position].idToken
		requested[id] = record
	}

	updated := make([]listRecord, 0, len(records)+len(appended))
	for position, record := range records {
		// A duplicate of an id being set is dropped; the surviving copy is the
		// first occurrence, with its replacement applied if there is one.
		if _, isSet := requested[record.id]; isSet && firstByID[record.id] != position {
			continue
		}
		if replacement, ok := requested[record.id]; ok && firstByID[record.id] == position {
			updated = append(updated, replacement)
			continue
		}
		updated = append(updated, record)
	}
	updated = append(updated, appended...)
	return a.SetRawBytes(i, encodeListRecords(updated))
}

// SetListPair inserts or updates one id/path pair. See SetListPairs.
func (a *Archive) SetListPair(i int32, id, path string) error {
	return a.SetListPairs(i, []ListPair{{ID: id, Path: path}})
}

// RemoveListIDs removes every record whose id matches, returning the number of
// removed records. Other records are re-emitted verbatim.
func (a *Archive) RemoveListIDs(i int32, ids []string) (int, error) {
	targets := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		targets[id] = struct{}{}
	}
	if len(targets) == 0 {
		return 0, nil
	}
	records, err := a.listRecordsAt(i)
	if err != nil {
		return 0, err
	}
	kept := make([]listRecord, 0, len(records))
	removed := 0
	for _, record := range records {
		if _, ok := targets[record.id]; ok && record.id != "" {
			removed++
			continue
		}
		kept = append(kept, record)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := a.SetRawBytes(i, encodeListRecords(kept)); err != nil {
		return 0, err
	}
	return removed, nil
}

// ListID returns the id registered for path. It reports a malformed payload
// rather than treating it as "not found", so a lookup failure cannot be
// mistaken for an absent entry.
func (a *Archive) ListID(i int32, path string) (string, bool, error) {
	records, err := a.listRecordsAt(i)
	if err != nil {
		return "", false, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	for _, record := range records {
		if record.path == path && record.id != "" {
			return record.id, true, nil
		}
	}
	return "", false, nil
}

// ScriptMetadata extracts [name], [icon] and [field image] in one raw token
// scan without formatting or materializing the full decompiled script.
func (a *Archive) ScriptMetadata(i int32) (ScriptMetadata, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return ScriptMetadata{}, ErrBadIndex
	}
	if a.items[i].typ != TypeScript {
		return ScriptMetadata{}, fmt.Errorf("pvf: file %d is not a script", i)
	}
	raw, err := a.RawBytes(i)
	if err != nil {
		return ScriptMetadata{}, err
	}
	if len(raw)%5 != 0 {
		return ScriptMetadata{}, fmt.Errorf("pvf: malformed script payload for file %d", i)
	}

	type metadataToken struct {
		typ   byte
		value int32
		text  string
	}
	type metadataSection struct {
		name   string
		paired bool
		depth  int
		values []metadataToken
	}

	// PVF scripts contain both paired sections and legacy, unpaired sections.
	// First collect closing names so the stack follows the same semantic rule
	// as decodeScript and ParseScriptView.
	pairedNames := make(map[string]bool)
	for pos := 0; pos < len(raw); pos += 5 {
		if raw[pos] != 3 {
			continue
		}
		if name, closing, ok := parseSectionTag(a.ResolveString(int32(binary.LittleEndian.Uint32(raw[pos+1:])))); ok && closing {
			pairedNames[name] = true
		}
	}

	sections := make([]metadataSection, 0)
	stack := make([]int, 0)
	activeUnpaired := make(map[int]int)
	closeUnpaired := func(depth int) {
		delete(activeUnpaired, depth)
	}
	for pos := 0; pos < len(raw); pos += 5 {
		typ := raw[pos]
		value := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		if typ == 3 {
			name, closing, ok := parseSectionTag(a.ResolveString(value))
			if ok {
				depth := len(stack)
				closeUnpaired(depth)
				if closing {
					for position := len(stack) - 1; position >= 0; position-- {
						if sections[stack[position]].name == name {
							stack = stack[:position]
							break
						}
					}
					continue
				}
				sectionIndex := len(sections)
				sections = append(sections, metadataSection{name: name, paired: pairedNames[name], depth: depth})
				if pairedNames[name] {
					stack = append(stack, sectionIndex)
				} else {
					activeUnpaired[depth] = sectionIndex
				}
				continue
			}
		}

		sectionIndex, ok := activeUnpaired[len(stack)]
		if !ok && len(stack) > 0 {
			sectionIndex = stack[len(stack)-1]
			ok = true
		}
		if !ok {
			continue
		}
		text := ""
		if typ == 3 || typ == 5 || typ == 6 || typ == 7 {
			text = a.ResolveString(value)
		}
		sections[sectionIndex].values = append(sections[sectionIndex].values, metadataToken{typ: typ, value: value, text: text})
	}

	metadata := ScriptMetadata{}
	for _, section := range sections {
		name := strings.ToLower(strings.TrimSpace(section.name))
		switch name {
		case "name":
			// Entity names only come from top-level sections; a nested [name]
			// belongs to an embedded sub-record, not to the file itself.
			if section.depth != 0 {
				continue
			}
			if metadata.HasName {
				continue
			}
			for _, token := range section.values {
				if (token.typ == 5 || token.typ == 6 || token.typ == 7 || token.typ == 3) && strings.TrimSpace(token.text) != "" {
					metadata.Name = token.text
					metadata.HasName = true
					break
				}
			}
		case "icon", "field image":
			if len(section.values) < 2 {
				continue
			}
			for index := 0; index+1 < len(section.values); index++ {
				pathToken := section.values[index]
				imageIndexToken := section.values[index+1]
				if (pathToken.typ != 3 && pathToken.typ != 5 && pathToken.typ != 6 && pathToken.typ != 7) || strings.TrimSpace(pathToken.text) == "" || imageIndexToken.typ != 0 || imageIndexToken.value < 0 {
					continue
				}
				reference := &ScriptImageReference{Path: strings.TrimSpace(pathToken.text), Index: imageIndexToken.value}
				if name == "icon" && metadata.Icon == nil {
					metadata.Icon = reference
				}
				if name == "field image" && metadata.FieldImage == nil {
					metadata.FieldImage = reference
				}
				break
			}
		}
	}
	return metadata, nil
}

// ScriptName extracts the first direct string value from the [name] section
// without formatting or materializing the full decompiled script.
func (a *Archive) ScriptName(i int32) (string, bool, error) {
	metadata, err := a.ScriptMetadata(i)
	if err != nil {
		return "", false, err
	}
	return metadata.Name, metadata.HasName, nil
}

type scriptMetadataValue struct {
	text string
}

func (a *Archive) scriptMetadataValue(typ byte, value int32) (scriptMetadataValue, bool) {
	switch typ {
	case 0:
		return scriptMetadataValue{text: strconv.FormatInt(int64(value), 10)}, true
	case 2:
		return scriptMetadataValue{text: strconv.FormatFloat(float64(math.Float32frombits(uint32(value))), 'g', -1, 32)}, true
	case 3, 5, 6, 7:
		return scriptMetadataValue{text: a.ResolveString(value)}, true
	default:
		return scriptMetadataValue{}, false
	}
}
