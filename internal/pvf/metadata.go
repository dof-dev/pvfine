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

// ScriptName extracts the first direct string value from the [name] section
// without formatting or materializing the full decompiled script.
func (a *Archive) ScriptName(i int32) (string, bool, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return "", false, ErrBadIndex
	}
	if a.items[i].typ != TypeScript {
		return "", false, fmt.Errorf("pvf: file %d is not a script", i)
	}
	raw, err := a.RawBytes(i)
	if err != nil {
		return "", false, err
	}
	if len(raw)%5 != 0 {
		return "", false, fmt.Errorf("pvf: malformed script payload for file %d", i)
	}

	inName := false
	nestedDepth := 0
	for pos := 0; pos < len(raw); pos += 5 {
		typ := raw[pos]
		value := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		if typ == 3 {
			tag := a.ResolveString(value)
			section, closing, ok := parseSectionTag(tag)
			if ok {
				if !inName {
					if !closing && section == "name" {
						inName = true
					}
					continue
				}

				if closing {
					if nestedDepth > 0 {
						nestedDepth--
						continue
					}
					return "", false, nil
				}
				if nestedDepth == 0 {
					// [name] is a flat section in normal PVFs. A new section
					// therefore ends an unterminated [name] section.
					return "", false, nil
				}
				nestedDepth++
				continue
			}
		}

		if !inName {
			continue
		}
		if typ == 3 {
			// A bare string token is also valid as a name value.
			if valueText := a.ResolveString(value); valueText != "" {
				return valueText, true, nil
			}
			continue
		}
		if typ == 5 || typ == 6 || typ == 7 {
			return a.ResolveString(value), true, nil
		}
	}
	return "", false, nil
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
