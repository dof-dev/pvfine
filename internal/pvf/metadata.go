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
				sections = append(sections, metadataSection{name: name, paired: pairedNames[name]})
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
