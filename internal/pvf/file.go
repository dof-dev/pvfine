package pvf

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"pvfine/internal/rendering"
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

// SetDataType changes the PVF interpretation type of an existing entry.
// Import uses this when replacing a file whose extension implies a different
// type than the entry currently has.
func (a *Archive) SetDataType(i int32, dataType int32) error {
	if i < 0 || i >= int32(len(a.items)) {
		return ErrBadIndex
	}
	if dataType != TypeScript && dataType != TypeUnicode {
		return ErrBadDataType
	}
	a.items[i].typ = dataType
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

// CanonicalText returns the stable logical text used by version snapshots.
// It intentionally ignores user-facing rendering overrides so a line-layout
// change cannot make an otherwise identical file appear modified.
func (a *Archive) CanonicalText(i int32) (string, error) {
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
		return a.decodeScriptForPathWithRenderer(raw, a.Path(i), a.canonicalScriptRenderer), nil
	default:
		return "", nil
	}
}

// SetScriptRenderer replaces the user-facing renderer for subsequent Text
// calls. The engine is immutable and can safely be shared by archive clones.
func (a *Archive) SetScriptRenderer(engine *rendering.Engine) {
	if a == nil {
		return
	}
	a.scriptRenderer = engine
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
		raw := utf16le(text)
		a.overlay[i] = raw
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
	// offset places the first value tokens on separate lines before grouping
	// begins.
	offset int
	// tokensPerLine wraps content tokens after this many tokens. Zero means
	// that the default line layout is used.
	tokensPerLine int
	// tokensPerLineIndex is resolved during the raw token pre-scan and is nil
	// once the frame is ready for rendering.
	tokensPerLineIndex *int
}

// decodeScript decompiles a TypeScript payload back to readable form.
func (a *Archive) decodeScript(raw []byte) string {
	return a.decodeScriptForPath(raw, "")
}

// decodeScriptForPath decompiles a TypeScript payload using file-specific
// formatting rules in addition to the generic section layout.
func (a *Archive) decodeScriptForPath(raw []byte, path string) string {
	return a.decodeScriptForPathWithRenderer(raw, path, a.scriptRenderer)
}

func (a *Archive) decodeScriptForPathWithRenderer(raw []byte, path string, renderer *rendering.Engine) string {
	var sb strings.Builder
	sectionFormats, sectionClosers := a.scriptSectionFormats(raw, path, renderer)
	fileRule := scriptFormatRuleFromSpec(rendering.FormatSpec{})
	if renderer != nil {
		fileRule = scriptFormatRuleFromSpec(renderer.FileFormat(path))
	}
	n := len(raw) / 5

	type sectionFrame struct {
		name         string
		firstToken   bool
		format       scriptFormatRule
		valuesSeen   int
		tokensOnLine int
	}
	sectionStack := []sectionFrame{}
	topLevelSectionSeen := false
	atLineStart := true
	tokenSeen := false
	fileTokensOnLine := 0
	fileValuesSeen := 0
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
			frame.valuesSeen++
			if frame.format.tokensPerLine > 0 {
				if frame.valuesSeen > frame.format.offset {
					frame.tokensOnLine++
				}
				return
			}
		}
		if fileRule.tokensPerLine > 0 {
			fileValuesSeen++
			if fileValuesSeen > fileRule.offset {
				fileTokensOnLine++
			}
		}
	}
	prepareSectionValue := func(forceNewLine bool) {
		if len(sectionStack) > 0 {
			frame := &sectionStack[len(sectionStack)-1]
			if frame.format.tokensPerLine > 0 {
				if frame.valuesSeen > 0 && frame.valuesSeen <= frame.format.offset {
					if !atLineStart {
						sb.WriteByte('\n')
					}
					atLineStart = true
					frame.tokensOnLine = 0
				} else if frame.tokensOnLine >= frame.format.tokensPerLine {
					if !atLineStart {
						sb.WriteByte('\n')
					}
					atLineStart = true
					frame.tokensOnLine = 0
				}
			} else if fileRule.tokensPerLine > 0 {
				if fileValuesSeen > 0 && fileValuesSeen <= fileRule.offset {
					if !atLineStart {
						sb.WriteByte('\n')
					}
					atLineStart = true
					fileTokensOnLine = 0
				} else if fileTokensOnLine >= fileRule.tokensPerLine {
					if !atLineStart {
						sb.WriteByte('\n')
					}
					atLineStart = true
					fileTokensOnLine = 0
				}
			}
		} else if fileRule.tokensPerLine > 0 {
			if fileValuesSeen > 0 && fileValuesSeen <= fileRule.offset {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				atLineStart = true
				fileTokensOnLine = 0
			} else if fileTokensOnLine >= fileRule.tokensPerLine {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				atLineStart = true
				fileTokensOnLine = 0
			}
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
				sectionRule := sectionFormats[i]
				sectionStack = append(sectionStack, sectionFrame{
					name:         name,
					firstToken:   true,
					format:       sectionRule,
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

func scriptFormatRuleFromSpec(spec rendering.FormatSpec) scriptFormatRule {
	return scriptFormatRule{
		offset:             spec.Offset,
		tokensPerLine:      spec.TokensPerLine,
		tokensPerLineIndex: spec.TokensPerLineIndex,
	}
}

type scriptRenderSection struct {
	name         string
	openingIndex int
	format       scriptFormatRule
	values       []string
}

// scriptSectionFormats pre-scans paired sections so a dynamic
// tokensPerLineIndex can be resolved before the first value is rendered.
func (a *Archive) scriptSectionFormats(raw []byte, path string, renderer *rendering.Engine) (map[int]scriptFormatRule, map[string]bool) {
	n := len(raw) / 5
	sectionClosers := make(map[string]bool)
	for i := 0; i < n; i++ {
		base := i * 5
		if raw[base] != 3 {
			continue
		}
		if name, closing, ok := parseSectionTag(a.ResolveString(int32(binary.LittleEndian.Uint32(raw[base+1:])))); ok && closing {
			sectionClosers[name] = true
		}
	}

	formats := make(map[int]scriptFormatRule)
	stack := make([]scriptRenderSection, 0)
	resolveFrame := func(frame scriptRenderSection) {
		formats[frame.openingIndex] = resolveDynamicScriptFormat(frame.format, frame.values)
	}
	for i := 0; i < n; i++ {
		base := i * 5
		typ := raw[base]
		if typ == 3 {
			name, closing, isTag := parseSectionTag(a.ResolveString(int32(binary.LittleEndian.Uint32(raw[base+1:]))))
			if !isTag {
				continue
			}
			if closing {
				match := -1
				for position := len(stack) - 1; position >= 0; position-- {
					if stack[position].name == name {
						match = position
						break
					}
				}
				if match >= 0 {
					for position := len(stack) - 1; position >= match; position-- {
						resolveFrame(stack[position])
					}
					stack = stack[:match]
				}
				continue
			}
			if !sectionClosers[name] {
				continue
			}
			format := scriptFormatRule{}
			if renderer != nil {
				format = scriptFormatRuleFromSpec(renderer.SectionFormat(path, name))
			}
			stack = append(stack, scriptRenderSection{
				name:         name,
				openingIndex: i,
				format:       format,
			})
			continue
		}
		if len(stack) == 0 || stack[len(stack)-1].format.tokensPerLineIndex == nil {
			continue
		}
		value, ok := a.scriptTokenValue(raw, i)
		if !ok {
			continue
		}
		frame := &stack[len(stack)-1]
		if len(frame.values) <= *frame.format.tokensPerLineIndex {
			frame.values = append(frame.values, value)
		}
	}
	for position := len(stack) - 1; position >= 0; position-- {
		resolveFrame(stack[position])
	}
	return formats, sectionClosers
}

func resolveDynamicScriptFormat(format scriptFormatRule, values []string) scriptFormatRule {
	if format.tokensPerLineIndex != nil {
		index := *format.tokensPerLineIndex
		if index >= 0 && index < len(values) {
			if value, err := strconv.Atoi(strings.TrimSpace(values[index])); err == nil && value > 0 {
				format.tokensPerLine = value
			}
		}
		format.tokensPerLineIndex = nil
	}
	return format
}

func (a *Archive) scriptTokenValue(raw []byte, index int) (string, bool) {
	base := index * 5
	if base < 0 || base+5 > len(raw) {
		return "", false
	}
	typ := raw[base]
	v := int32(binary.LittleEndian.Uint32(raw[base+1:]))
	switch typ {
	case 0:
		return strconv.FormatInt(int64(v), 10), true
	case 2:
		return strconv.FormatFloat(float64(math.Float32frombits(uint32(v))), 'g', -1, 32), true
	case 5, 6, 7:
		return a.ResolveString(v), true
	default:
		return "", false
	}
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
