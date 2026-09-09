package pvf

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Modified reports whether any pending edits exist.
func (a *Archive) Modified() bool { return len(a.overlay) > 0 || a.structuralDirty }

// RawBytes returns the file payload. The slice aliases a cached chunk;
// treat it as read-only.
func (a *Archive) RawBytes(i int32) ([]byte, error) {
	if i < 0 || i >= int32(len(a.items)) {
		return nil, ErrBadIndex
	}
	if b, ok := a.overlay[i]; ok {
		return b, nil
	}
	it := &a.items[i]
	ch, err := a.Chunk(it.chunk)
	if err != nil {
		return nil, err
	}
	if ch == nil || it.off < 0 || it.size <= 0 || int64(it.off)+int64(it.size) > int64(len(ch)) {
		return nil, nil
	}
	return ch[it.off : it.off+it.size], nil
}

// SetRawBytes queues a replacement payload for entry i.
func (a *Archive) SetRawBytes(i int32, b []byte) error {
	if i < 0 || i >= int32(len(a.items)) {
		return ErrBadIndex
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	a.overlay[i] = cp
	return nil
}

// Text decodes entry i: token scripts are decompiled, UTF-16 sections are
// returned as text (with Korean-server mojibake repaired when detected).
func (a *Archive) Text(i int32) (string, error) {
	raw, err := a.RawBytes(i)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", nil
	}
	switch a.items[i].typ {
	case TypeUnicode:
		return fixKoreanMojibake(string(utf16.Decode(u16le(raw)))), nil
	case TypeScript:
		return a.decodeScriptForPath(raw, a.Path(i)), nil
	default:
		return "", nil
	}
}

// SetText re-encodes text for entry i according to its data type and queues
// the result. For TypeUnicode this writes plain UTF-16LE; use SetRawBytes to
// preserve exotic byte-level forms.
func (a *Archive) SetText(i int32, text string) error {
	if i < 0 || i >= int32(len(a.items)) {
		return ErrBadIndex
	}
	switch a.items[i].typ {
	case TypeUnicode:
		a.overlay[i] = utf16le(text)
		return nil
	case TypeScript:
		raw, err := a.encodeScript(text)
		if err != nil {
			return err
		}
		a.overlay[i] = raw
		return nil
	default:
		return ErrBadIndex
	}
}

// AddFile appends a new entry with a raw payload; the returned index is
// valid immediately and the data is stored to its own chunk at save time.
func (a *Archive) AddFile(relPath string, data []byte, dataType int32) int32 {
	normalized := normalizePath(relPath)
	if i, ok := a.pathIndex[normalized]; ok {
		_ = a.SetRawBytes(i, data)
		return i
	}
	dir, name := splitDirName(normalized)
	item := fileItem{
		nameOff: a.StringOffset(name),
		pathOff: a.StringOffset(dir),
		chunk:   -1,
		typ:     dataType,
	}
	if data != nil {
		item.size = int32(len(data))
	}
	a.items = append(a.items, item)
	i := int32(len(a.items) - 1)
	a.pathIndex[normalized] = i
	a.structuralDirty = true
	if data != nil {
		cp := make([]byte, len(data))
		copy(cp, data)
		a.overlay[i] = cp
	}
	return i
}

// RemoveFiles removes the entries at the supplied indexes. The operation is
// applied atomically: invalid indexes are rejected before any entry is
// removed. Returned paths use the archive's original entry order.
func (a *Archive) RemoveFiles(indexes []int32) ([]string, error) {
	if len(indexes) == 0 {
		return []string{}, nil
	}

	removed := make(map[int32]struct{}, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= int32(len(a.items)) {
			return nil, ErrBadIndex
		}
		removed[index] = struct{}{}
	}
	if len(removed) == 0 {
		return []string{}, nil
	}

	paths := make([]string, 0, len(removed))
	nextItems := make([]fileItem, 0, len(a.items)-len(removed))
	nextOverlay := make(map[int32][]byte, len(a.overlay))
	nextPathIndex := make(map[string]int32, len(a.pathIndex))
	nextRemovedSpans := cloneRemovedSpans(a.removedSpans)
	for oldIndex, item := range a.items {
		index := int32(oldIndex)
		if _, ok := removed[index]; ok {
			paths = append(paths, a.Path(index))
			if item.chunk >= 0 && item.chunk < int32(len(a.groups)) && item.size > 0 {
				nextRemovedSpans[item.chunk] = append(
					nextRemovedSpans[item.chunk],
					removedFileSpan{off: item.off, size: item.size},
				)
			}
			continue
		}

		newIndex := int32(len(nextItems))
		nextItems = append(nextItems, item)
		if payload, ok := a.overlay[index]; ok {
			nextOverlay[newIndex] = payload
		}
		path := normalizePath(a.Path(index))
		if _, exists := nextPathIndex[path]; !exists {
			nextPathIndex[path] = newIndex
		}
	}

	a.items = nextItems
	a.overlay = nextOverlay
	a.pathIndex = nextPathIndex
	a.resolveCache = make(map[int32]string)
	a.removedSpans = nextRemovedSpans
	a.structuralDirty = true
	return paths, nil
}

func cloneRemovedSpans(values map[int32][]removedFileSpan) map[int32][]removedFileSpan {
	result := make(map[int32][]removedFileSpan, len(values))
	for chunk, spans := range values {
		result[chunk] = append([]removedFileSpan(nil), spans...)
	}
	return result
}

// AddFileText appends a new entry from decompiled text, encoding it according
// to dataType (TypeScript or TypeUnicode).
func (a *Archive) AddFileText(relPath, text string, dataType int32) (int32, error) {
	i := a.AddFile(relPath, nil, dataType)
	if err := a.SetText(i, text); err != nil {
		return i, err
	}
	return i, nil
}

// utf16le encodes s as UTF-16LE bytes (no BOM), matching Encoding.Unicode.
func utf16le(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	out := make([]byte, len(u16)*2)
	for i, u := range u16 {
		binary.LittleEndian.PutUint16(out[i*2:], u)
	}
	return out
}

func u16le(b []byte) []uint16 {
	n := len(b) / 2
	out := make([]uint16, n)
	for i := 0; i < n; i++ {
		out[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return out
}

func splitDirName(p string) (dir, name string) {
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[:idx], p[idx+1:]
	}
	return "", p
}

type scriptFormatRule struct {
	// tokensPerLine wraps content tokens after this many tokens. Zero means
	// that the default line layout is used.
	tokensPerLine int
}

var scriptSectionRules = map[string]scriptFormatRule{
	"skill data up": {tokensPerLine: 7},
	"skill levelup": {tokensPerLine: 3},
}

// decodeScript decompiles a TypeScript payload back to readable form.
func (a *Archive) decodeScript(raw []byte) string {
	return a.decodeScriptForPath(raw, "")
}

var scriptFileRules = map[string]scriptFormatRule{
	".lst": {tokensPerLine: 2},
}

func scriptFileRule(path string) scriptFormatRule {
	path = strings.ToLower(path)
	if dot := strings.LastIndexByte(path, '.'); dot >= 0 {
		return scriptFileRules[path[dot:]]
	}
	return scriptFormatRule{}
}

// decodeScriptForPath decompiles a TypeScript payload using file-specific
// formatting rules in addition to the generic section layout.
func (a *Archive) decodeScriptForPath(raw []byte, path string) string {
	var sb strings.Builder
	sectionClosers := map[string]bool{}
	fileRule := scriptFileRule(path)
	n := len(raw) / 5
	for i := 0; i < n; i++ {
		base := i * 5
		if raw[base] != 3 {
			continue
		}
		if name, closing, ok := parseSectionTag(a.ResolveString(int32(binary.LittleEndian.Uint32(raw[base+1:])))); ok && closing {
			sectionClosers[name] = true
		}
	}

	type sectionFrame struct {
		name         string
		firstToken   bool
		format       scriptFormatRule
		tokensOnLine int
	}
	sectionStack := []sectionFrame{}
	topLevelSectionSeen := false
	atLineStart := true
	tokenSeen := false
	fileTokensOnLine := 0
	writeIndent := func(depth int) {
		for i := 0; i < depth; i++ {
			sb.WriteByte('\t')
		}
	}
	writeLineTag := func(tag string, depth int) {
		if !atLineStart {
			sb.WriteByte('\n')
		}
		writeIndent(depth)
		sb.WriteString(tag)
		sb.WriteByte('\n')
		atLineStart = true
	}
	markSectionTag := func() {
		if len(sectionStack) > 0 {
			frame := &sectionStack[len(sectionStack)-1]
			frame.firstToken = false
			frame.tokensOnLine = 0
		}
	}
	markSectionValue := func() {
		tokenSeen = true
		if len(sectionStack) > 0 {
			frame := &sectionStack[len(sectionStack)-1]
			frame.firstToken = false
			if frame.format.tokensPerLine > 0 {
				frame.tokensOnLine++
				return
			}
		}
		if fileRule.tokensPerLine > 0 {
			fileTokensOnLine++
		}
	}
	prepareSectionValue := func(forceNewLine bool) {
		if len(sectionStack) > 0 {
			frame := &sectionStack[len(sectionStack)-1]
			if frame.format.tokensPerLine > 0 && frame.tokensOnLine >= frame.format.tokensPerLine {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				atLineStart = true
				frame.tokensOnLine = 0
			} else if fileRule.tokensPerLine > 0 && fileTokensOnLine >= fileRule.tokensPerLine {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				atLineStart = true
				fileTokensOnLine = 0
			}
		} else if fileRule.tokensPerLine > 0 && fileTokensOnLine >= fileRule.tokensPerLine {
			if !atLineStart {
				sb.WriteByte('\n')
			}
			atLineStart = true
			fileTokensOnLine = 0
		}
		if forceNewLine && !atLineStart {
			sb.WriteByte('\n')
			atLineStart = true
		}
	}
	contentIndent := func() int {
		if !tokenSeen {
			return 0
		}
		if len(sectionStack) == 0 {
			if fileRule.tokensPerLine > 0 {
				return 0
			}
			return 1
		}
		frame := sectionStack[len(sectionStack)-1]
		if frame.format.tokensPerLine > 0 || frame.firstToken {
			return len(sectionStack)
		}
		return len(sectionStack) + 1
	}
	writeValuePrefix := func() {
		prepareSectionValue(false)
		if atLineStart {
			writeIndent(contentIndent())
		} else {
			sb.WriteByte('\t')
		}
		atLineStart = false
		markSectionValue()
	}

	for i := 0; i < n; i++ {
		base := i * 5
		typ := raw[base]
		v := int32(binary.LittleEndian.Uint32(raw[base+1:]))
		switch typ {
		case 0:
			writeValuePrefix()
			sb.WriteString(strconv.FormatInt(int64(v), 10))
		case 2:
			writeValuePrefix()
			f := math.Float32frombits(uint32(v))
			sb.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
		case 3:
			tag := a.ResolveString(v)
			name, closing, isTag := parseSectionTag(tag)
			isSectionOpening := isTag && !closing && sectionClosers[name]
			if closing {
				for i := len(sectionStack) - 1; i >= 0; i-- {
					if sectionStack[i].name == name {
						sectionStack = sectionStack[:i]
						break
					}
				}
			}
			isTopLevelSection := isTag && !closing && len(sectionStack) == 0
			if isTopLevelSection && topLevelSectionSeen {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				sb.WriteByte('\n')
				atLineStart = true
			}
			writeLineTag(tag, len(sectionStack))
			tokenSeen = true
			if isTag && !closing {
				markSectionTag()
			}
			if isTopLevelSection {
				topLevelSectionSeen = true
			}
			if isSectionOpening {
				sectionStack = append(sectionStack, sectionFrame{
					name:         name,
					firstToken:   true,
					format:       scriptSectionRules[name],
					tokensOnLine: 0,
				})
			}
		case 5:
			prepareSectionValue(true)
			writeIndent(contentIndent())
			sb.WriteString("{5=`")
			sb.WriteString(escapeBacktick(a.ResolveString(v)))
			sb.WriteString("`}")
			atLineStart = false
			markSectionValue()
		case 6:
			writeValuePrefix()
			sb.WriteString("`")
			sb.WriteString(escapeBacktick(a.ResolveString(v)))
			sb.WriteString("`")
		case 7:
			prepareSectionValue(true)
			writeIndent(contentIndent())
			sb.WriteString("{7=`")
			sb.WriteString(escapeBacktick(a.ResolveString(v)))
			sb.WriteString("`}")
			atLineStart = false
			markSectionValue()
		}
	}
	return sb.String()
}

func parseSectionTag(tag string) (name string, closing, ok bool) {
	if len(tag) < 3 || tag[0] != '[' || tag[len(tag)-1] != ']' {
		return "", false, false
	}
	inner := tag[1 : len(tag)-1]
	if inner == "" {
		return "", false, false
	}
	if strings.HasPrefix(inner, "/") {
		if len(inner) == 1 {
			return "", false, false
		}
		return inner[1:], true, true
	}
	return inner, false, true
}

func escapeBacktick(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}

// IsModified reports whether entry i has a pending in-memory edit.
func (a *Archive) IsModified(i int32) bool {
	_, ok := a.overlay[i]
	return ok
}
