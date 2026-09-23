package pvf

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"pvfine/internal/rendering"
)

// Modified reports whether any pending edits exist.
func (a *Archive) Modified() bool {
	return len(a.overlay) > 0 || a.structuralDirty || a.poolsDirty
}

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
	previous, err := a.RawBytes(i)
	if err != nil {
		return err
	}
	if bytes.Equal(previous, b) {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	a.overlay[i] = cp
	a.recordMutation(i, a.Path(i), MutationModified)
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
	if a.items[i].typ == dataType {
		return nil
	}
	a.items[i].typ = dataType
	a.recordMutation(i, a.Path(i), MutationModified)
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
// the result. For TypeUnicode the text is written back in the byte-level form
// the archive already uses: a Korean-server payload whose characters are EUC-KR
// bytes painted through the CP437 font table is re-encoded that way, so opening
// such a file, editing it and saving it does not turn readable Korean into
// mojibake. Use SetRawBytes to preserve any other exotic byte-level form.
func (a *Archive) SetText(i int32, text string) error {
	if i < 0 || i >= int32(len(a.items)) {
		return ErrBadIndex
	}
	switch a.items[i].typ {
	case TypeUnicode:
		if a.payloadIsPainted(i) {
			if encoded, err := EncodeKoreanMojibake(text); err == nil {
				return a.SetRawBytes(i, encoded)
			}
		}
		return a.SetRawBytes(i, utf16le(text))
	case TypeScript:
		raw, err := a.encodeScript(text)
		if err != nil {
			return err
		}
		return a.SetRawBytes(i, raw)
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
	a.recordMutation(i, normalized, MutationAdded)
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
			path := a.Path(index)
			paths = append(paths, path)
			a.recordMutation(index, path, MutationRemoved)
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
	a.resolveCacheOrder = nil
	a.resolveCacheBytes = 0
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

// payloadIsPainted reports whether entry i currently holds a Korean-server
// payload: EUC-KR bytes painted through the CP437 font table.
func (a *Archive) payloadIsPainted(i int32) bool {
	raw, err := a.RawBytes(i)
	if err != nil || len(raw) < 2 {
		return false
	}
	return looksLikeCP437Painting(string(utf16.Decode(u16le(raw))))
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
	standaloneValues   []int32
	nestedSections     []string
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
	// Legacy PVF scripts use unpaired sections: the next section at the same
	// depth ends the current one. Keep those frames separate from the paired
	// section stack so nested paired sections retain their existing semantics.
	activeUnpaired := make(map[int]*sectionFrame)
	topLevelSectionSeen := false
	atLineStart := true
	tokenSeen := false
	fileTokensOnLine := 0
	fileValuesSeen := 0
	currentSectionFrame := func() *sectionFrame {
		if frame, ok := activeUnpaired[len(sectionStack)]; ok {
			return frame
		}
		if len(sectionStack) > 0 {
			return &sectionStack[len(sectionStack)-1]
		}
		return nil
	}
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
		if frame := currentSectionFrame(); frame != nil {
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
		if frame := currentSectionFrame(); frame != nil {
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
	effectiveSectionDepth := func() int {
		depth := len(sectionStack)
		for activeDepth := range activeUnpaired {
			if activeDepth <= len(sectionStack) {
				depth++
			}
		}
		return depth
	}
	contentIndent := func() int {
		if !tokenSeen {
			return 0
		}
		frame := currentSectionFrame()
		if frame == nil {
			if fileRule.tokensPerLine > 0 {
				return 0
			}
			return 1
		}
		depth := effectiveSectionDepth()
		if frame.format.tokensPerLine > 0 || frame.firstToken {
			return depth
		}
		return depth + 1
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

	// Standalone integer values break the current row and restart grouping.
	isStandaloneValue := func(value int32) bool {
		values := fileRule.standaloneValues
		if frame := currentSectionFrame(); frame != nil && frame.format.tokensPerLine > 0 {
			values = frame.format.standaloneValues
		}
		for _, standalone := range values {
			if value == standalone {
				return true
			}
		}
		return false
	}
	resetTokensOnLine := func() {
		if frame := currentSectionFrame(); frame != nil && frame.format.tokensPerLine > 0 {
			frame.tokensOnLine = 0
		} else {
			fileTokensOnLine = 0
		}
	}
	for i := 0; i < n; i++ {
		base := i * 5
		typ := raw[base]
		v := int32(binary.LittleEndian.Uint32(raw[base+1:]))
		switch typ {
		case 0:
			standalone := isStandaloneValue(v)
			if standalone {
				if !atLineStart {
					sb.WriteByte('\n')
					atLineStart = true
				}
				resetTokensOnLine()
			}
			writeValuePrefix()
			sb.WriteString(strconv.FormatInt(int64(v), 10))
			if standalone {
				sb.WriteByte('\n')
				atLineStart = true
				resetTokensOnLine()
			}
		case 2:
			writeValuePrefix()
			f := math.Float32frombits(uint32(v))
			sb.WriteString(formatScriptFloat(f))
		case 3:
			tag := a.ResolveString(v)
			name, closing, isTag := parseSectionTag(tag)
			if isTag {
				depth := len(sectionStack)
				if closing {
					for activeDepth := range activeUnpaired {
						if activeDepth >= depth {
							delete(activeUnpaired, activeDepth)
						}
					}
				} else {
					// A new section at this depth ends a legacy unpaired
					// section before the new section starts.
					if parent := activeUnpaired[depth]; parent == nil || !sectionClosers[name] || !isNestedScriptSection(parent.format, name) {
						delete(activeUnpaired, depth)
					}
				}
			}
			isSectionOpening := isTag && !closing && sectionClosers[name]
			if closing {
				for i := len(sectionStack) - 1; i >= 0; i-- {
					if sectionStack[i].name == name {
						sectionStack = sectionStack[:i]
						break
					}
				}
			}
			sectionDepth := effectiveSectionDepth()
			isTopLevelSection := isTag && !closing && sectionDepth == 0
			if isTopLevelSection && topLevelSectionSeen {
				if !atLineStart {
					sb.WriteByte('\n')
				}
				sb.WriteByte('\n')
				atLineStart = true
			}
			writeLineTag(tag, sectionDepth)
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
			} else if isTag && !closing {
				sectionRule := sectionFormats[i]
				if sectionRule.tokensPerLine > 0 {
					activeUnpaired[len(sectionStack)] = &sectionFrame{
						name:         name,
						firstToken:   true,
						format:       sectionRule,
						tokensOnLine: 0,
					}
				}
			}
		case 5, 7, 8, 10:
			// Block-string markers. Types 8 and 10 are the string-pool
			// references the newer clients emit (8 for values such as
			// `<31::equip_name_1>`, 10 for multi-line command text).
			prepareSectionValue(true)
			writeIndent(contentIndent())
			sb.WriteString("{")
			sb.WriteString(strconv.Itoa(int(typ)))
			sb.WriteString("=`")
			sb.WriteString(escapeBacktick(a.ResolveString(v)))
			sb.WriteString("`}")
			atLineStart = false
			markSectionValue()
		case 6:
			writeValuePrefix()
			sb.WriteString("`")
			sb.WriteString(escapeBacktick(a.ResolveString(v)))
			sb.WriteString("`")
		}
	}
	return sb.String()
}

func scriptFormatRuleFromSpec(spec rendering.FormatSpec) scriptFormatRule {
	return scriptFormatRule{
		offset:             spec.Offset,
		tokensPerLine:      spec.TokensPerLine,
		tokensPerLineIndex: spec.TokensPerLineIndex,
		standaloneValues:   spec.StandaloneValues,
		nestedSections:     spec.NestedSections,
	}
}

type scriptRenderSection struct {
	name         string
	openingIndex int
	format       scriptFormatRule
	values       []string
}

// scriptSectionFormats pre-scans paired and legacy unpaired sections so a
// dynamic tokensPerLineIndex can be resolved before the first value is
// rendered.
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
	activeUnpaired := make(map[int]*scriptRenderSection)
	resolveFrame := func(frame scriptRenderSection) {
		formats[frame.openingIndex] = resolveDynamicScriptFormat(frame.format, frame.values)
	}
	closeUnpaired := func(depth int) {
		if frame, ok := activeUnpaired[depth]; ok {
			resolveFrame(*frame)
			delete(activeUnpaired, depth)
		}
	}
	closeUnpairedAtOrAbove := func(depth int) {
		for activeDepth, frame := range activeUnpaired {
			if activeDepth < depth {
				continue
			}
			resolveFrame(*frame)
			delete(activeUnpaired, activeDepth)
		}
	}
	for i := 0; i < n; i++ {
		base := i * 5
		typ := raw[base]
		if typ == 3 {
			name, closing, isTag := parseSectionTag(a.ResolveString(int32(binary.LittleEndian.Uint32(raw[base+1:]))))
			if !isTag {
				continue
			}
			depth := len(stack)
			if closing {
				closeUnpairedAtOrAbove(depth)
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
			if parent := activeUnpaired[depth]; parent == nil || !sectionClosers[name] || !isNestedScriptSection(parent.format, name) {
				closeUnpaired(depth)
			}
			format := scriptFormatRule{}
			if renderer != nil {
				format = scriptFormatRuleFromSpec(renderer.SectionFormat(path, name))
			}
			frame := &scriptRenderSection{
				name:         name,
				openingIndex: i,
				format:       format,
			}
			if sectionClosers[name] {
				stack = append(stack, *frame)
			} else if format.tokensPerLine > 0 {
				activeUnpaired[depth] = frame
			}
			continue
		}
		frame := (*scriptRenderSection)(nil)
		if active, ok := activeUnpaired[len(stack)]; ok {
			frame = active
		} else if len(stack) > 0 {
			frame = &stack[len(stack)-1]
		}
		if frame == nil || frame.format.tokensPerLineIndex == nil {
			continue
		}
		value, ok := a.scriptTokenValue(raw, i)
		if !ok {
			continue
		}
		if len(frame.values) <= *frame.format.tokensPerLineIndex {
			frame.values = append(frame.values, value)
		}
	}
	for depth, frame := range activeUnpaired {
		resolveFrame(*frame)
		delete(activeUnpaired, depth)
	}
	for position := len(stack) - 1; position >= 0; position-- {
		resolveFrame(stack[position])
	}
	return formats, sectionClosers
}

// isNestedScriptSection identifies explicitly declared paired child sections.
func isNestedScriptSection(format scriptFormatRule, name string) bool {
	for _, child := range format.nestedSections {
		if strings.EqualFold(strings.TrimSpace(child), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// formatScriptFloat renders a float token so that re-encoding reproduces the
// float type: an integral value keeps an explicit decimal point ("8000.0"),
// otherwise the text would be read back as an integer token.
func formatScriptFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'g', -1, 32)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
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
		return formatScriptFloat(math.Float32frombits(uint32(v))), true
	case 5, 6, 7, 8, 10:
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
