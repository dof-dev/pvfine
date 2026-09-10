package pvf

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf16"
)

// ScriptTokenType is the semantic type of one TypeScript token.
type ScriptTokenType string

const (
	ScriptTokenInteger ScriptTokenType = "integer"
	ScriptTokenFloat   ScriptTokenType = "float"
	ScriptTokenString  ScriptTokenType = "string"
	ScriptTokenQuoted  ScriptTokenType = "quoted"
	ScriptTokenBlock5  ScriptTokenType = "block5"
	ScriptTokenBlock7  ScriptTokenType = "block7"
)

// ScriptStringPool identifies the name pool used by a string-backed token.
type ScriptStringPool string

const (
	ScriptPoolUTF8  ScriptStringPool = "utf8"
	ScriptPoolUTF16 ScriptStringPool = "utf16"
)

// ScriptValue is a loss-aware value in a structured script document. Value is
// an int64 for integer tokens, float64 for float tokens, and string for the
// string-backed token types. block5/block7 may contain either a number or a
// string because the PVF format permits both representations.
type ScriptValue struct {
	Type  ScriptTokenType
	Value any
	Pool  ScriptStringPool

	raw      batchToken
	rawValid bool
	dirty    bool
}

// NewScriptValue constructs a value intended for insertion or replacement.
// The input is deliberately kept as any because the JS adapter must validate
// and convert Goja values before reaching the binary encoder.
func NewScriptValue(kind ScriptTokenType, value any, pool ScriptStringPool) (ScriptValue, error) {
	if kind == "" {
		return ScriptValue{}, fmt.Errorf("脚本值类型不能为空")
	}
	switch kind {
	case ScriptTokenInteger, ScriptTokenFloat:
		if _, ok := scriptNumber(value); !ok {
			return ScriptValue{}, fmt.Errorf("脚本值类型 %s 需要数字", kind)
		}
	case ScriptTokenString, ScriptTokenQuoted:
		if _, ok := value.(string); !ok {
			return ScriptValue{}, fmt.Errorf("脚本值类型 %s 需要字符串", kind)
		}
		if pool == "" {
			pool = ScriptPoolUTF8
		}
		if pool != ScriptPoolUTF8 && pool != ScriptPoolUTF16 {
			return ScriptValue{}, fmt.Errorf("未知字符串池: %s", pool)
		}
	case ScriptTokenBlock5, ScriptTokenBlock7:
		if _, number := scriptNumber(value); !number {
			if _, stringValue := value.(string); !stringValue {
				return ScriptValue{}, fmt.Errorf("脚本值类型 %s 需要数字或字符串", kind)
			}
			if pool == "" {
				pool = ScriptPoolUTF8
			}
			if pool != ScriptPoolUTF8 && pool != ScriptPoolUTF16 {
				return ScriptValue{}, fmt.Errorf("未知字符串池: %s", pool)
			}
		}
	default:
		return ScriptValue{}, fmt.Errorf("未知脚本值类型: %s", kind)
	}
	return ScriptValue{Type: kind, Value: normalizeScriptValueNumber(value), Pool: pool, dirty: true}, nil
}

func normalizeScriptValueNumber(value any) any {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int8:
		return int64(number)
	case int16:
		return int64(number)
	case int32:
		return int64(number)
	case int64:
		return number
	case uint:
		return int64(number)
	case uint8:
		return int64(number)
	case uint16:
		return int64(number)
	case uint32:
		return int64(number)
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number)
		}
	case float32:
		return float64(number)
	case float64:
		return number
	}
	return value
}

func scriptNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), number <= math.MaxInt64
	case float32:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

type scriptItem struct {
	value    *ScriptValue
	section  *ScriptSection
	rawToken *batchToken
}

// ScriptParseWarningCode identifies a recoverable section-structure problem.
type ScriptParseWarningCode string

const (
	ScriptWarningOrphanClose     ScriptParseWarningCode = "orphan-close"
	ScriptWarningMissingClose    ScriptParseWarningCode = "missing-close"
	ScriptWarningMismatchedClose ScriptParseWarningCode = "mismatched-close"
)

// ScriptParseWarning describes a section-structure problem recovered by the
// structured parser. TokenIndex is zero-based; for an EOF warning it points
// one past the last token.
type ScriptParseWarning struct {
	Code        ScriptParseWarningCode
	Message     string
	TokenIndex  int
	SectionPath []string
}

// ScriptDocument is a mutable token tree. It intentionally does not retain
// whitespace or comments because those are not represented in PVF payloads.
type ScriptDocument struct {
	items    []*scriptItem
	warnings []ScriptParseWarning
}

// Warnings returns a copy of the recoverable parse warnings.
func (d *ScriptDocument) Warnings() []ScriptParseWarning {
	if d == nil || len(d.warnings) == 0 {
		return nil
	}
	result := make([]ScriptParseWarning, len(d.warnings))
	for index, warning := range d.warnings {
		result[index] = warning
		result[index].SectionPath = append([]string(nil), warning.SectionPath...)
	}
	return result
}

// ScriptSection is one section node in a ScriptDocument.
type ScriptSection struct {
	name       string
	paired     bool
	hasEndTag  bool
	parent     *ScriptSection
	document   *ScriptDocument
	items      []*scriptItem
	openToken  batchToken
	closeToken batchToken
	hasClose   bool
	deleted    bool
}

// Name returns the section name without brackets.
func (s *ScriptSection) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}

// HasEndTag reports whether the section has a paired closing tag.
func (s *ScriptSection) HasEndTag() bool {
	return s != nil && s.hasEndTag
}

// Path returns the normalized section path from the document root.
func (s *ScriptSection) Path() []string {
	if s == nil || s.deleted {
		return nil
	}
	path := make([]string, 0, 4)
	for current := s; current != nil; current = current.parent {
		path = append(path, current.name)
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

// Occurrence is the zero-based occurrence among sections with the same full
// path, in document order.
func (s *ScriptSection) Occurrence() int {
	if s == nil || s.document == nil || s.deleted {
		return -1
	}
	path := s.Path()
	occur := 0
	for _, candidate := range s.document.Sections(path) {
		if candidate == s {
			return occur
		}
		occur++
	}
	return -1
}

// Values returns direct values only; nested section values are excluded.
func (s *ScriptSection) Values() []ScriptValue {
	if s == nil || s.deleted {
		return nil
	}
	values := make([]ScriptValue, 0)
	for _, item := range s.items {
		if item.value != nil {
			values = append(values, cloneScriptValue(*item.value))
		}
	}
	return values
}

// Children returns direct child sections in source order.
func (s *ScriptSection) Children() []*ScriptSection {
	if s == nil || s.deleted {
		return nil
	}
	children := make([]*ScriptSection, 0)
	for _, item := range s.items {
		if item.section != nil && !item.section.deleted {
			children = append(children, item.section)
		}
	}
	return children
}

// GetValue returns one direct value by zero-based index.
func (s *ScriptSection) GetValue(index int) (ScriptValue, bool) {
	if s == nil || s.deleted || index < 0 {
		return ScriptValue{}, false
	}
	for _, item := range s.items {
		if item.value == nil {
			continue
		}
		if index == 0 {
			return cloneScriptValue(*item.value), true
		}
		index--
	}
	return ScriptValue{}, false
}

// Get returns the scalar payload of one direct value.
func (s *ScriptSection) Get(index int) (any, bool) {
	value, ok := s.GetValue(index)
	if !ok {
		return nil, false
	}
	return value.Value, true
}

// Set replaces one direct value while preserving the location of nested
// sections. Type inference is performed by the JS adapter before this call.
func (s *ScriptSection) Set(value ScriptValue, index int) error {
	if s == nil || s.deleted {
		return fmt.Errorf("section 已失效")
	}
	if index < 0 {
		return fmt.Errorf("值下标不能小于 0")
	}
	for _, item := range s.items {
		if item.value == nil {
			continue
		}
		if index == 0 {
			copyValue := cloneScriptValue(value)
			copyValue.dirty = true
			item.value = &copyValue
			return nil
		}
		index--
	}
	return fmt.Errorf("section [%s] 没有对应的直接值", s.name)
}

// SetValues replaces all direct values and keeps child section nodes in place.
func (s *ScriptSection) SetValues(values []ScriptValue) error {
	if s == nil || s.deleted {
		return fmt.Errorf("section 已失效")
	}
	for index := range values {
		values[index] = cloneScriptValue(values[index])
		values[index].dirty = true
	}
	firstValue := -1
	updated := make([]*scriptItem, 0, len(s.items)+len(values))
	for _, item := range s.items {
		if item.value != nil {
			if firstValue < 0 {
				firstValue = len(updated)
			}
			continue
		}
		updated = append(updated, item)
	}
	valueItems := make([]*scriptItem, 0, len(values))
	for index := range values {
		value := values[index]
		valueItems = append(valueItems, &scriptItem{value: &value})
	}
	if firstValue < 0 {
		updated = append(updated, valueItems...)
	} else {
		updated = append(append([]*scriptItem(nil), updated[:firstValue]...), append(valueItems, updated[firstValue:]...)...)
	}
	s.items = updated
	return nil
}

// Append appends one direct value after all existing child items.
func (s *ScriptSection) Append(value ScriptValue) error {
	if s == nil || s.deleted {
		return fmt.Errorf("section 已失效")
	}
	copyValue := cloneScriptValue(value)
	copyValue.dirty = true
	s.items = append(s.items, &scriptItem{value: &copyValue})
	return nil
}

// AppendSection appends a child section after all existing children.
func (s *ScriptSection) AppendSection(name string, values []ScriptValue, hasEndTag bool) (*ScriptSection, error) {
	if s == nil || s.deleted {
		return nil, fmt.Errorf("section 已失效")
	}
	return s.document.appendSection(s, name, values, hasEndTag)
}

// Delete removes the complete section subtree from its parent.
func (s *ScriptSection) Delete() error {
	if s == nil || s.deleted {
		return fmt.Errorf("section 已失效")
	}
	items := &s.document.items
	if s.parent != nil {
		items = &s.parent.items
	}
	for index, item := range *items {
		if item.section != s {
			continue
		}
		*items = append((*items)[:index], (*items)[index+1:]...)
		s.deleted = true
		return nil
	}
	return fmt.Errorf("section [%s] 不在文档中", s.name)
}

// Sections returns all sections with the exact normalized path in document
// order. An empty path returns all root-level sections.
func (d *ScriptDocument) Sections(path []string) []*ScriptSection {
	if d == nil {
		return nil
	}
	normalized := normalizeScriptPath(path)
	result := make([]*ScriptSection, 0)
	if len(normalized) == 0 {
		for _, item := range d.items {
			if item.section != nil && !item.section.deleted {
				result = append(result, item.section)
			}
		}
		return result
	}
	var walk func(items []*scriptItem)
	walk = func(items []*scriptItem) {
		for _, item := range items {
			if item.section == nil || item.section.deleted {
				continue
			}
			section := item.section
			if scriptPathEqual(section.Path(), normalized) {
				result = append(result, section)
			}
			walk(section.items)
		}
	}
	walk(d.items)
	return result
}

// Section returns one section by exact path and occurrence.
func (d *ScriptDocument) Section(path []string, occurrence int) (*ScriptSection, bool) {
	if occurrence < 0 {
		return nil, false
	}
	sections := d.Sections(path)
	if occurrence >= len(sections) {
		return nil, false
	}
	return sections[occurrence], true
}

// GetValue returns one value from one explicitly selected section.
func (d *ScriptDocument) GetValue(path []string, occurrence, valueIndex int) (ScriptValue, bool) {
	section, ok := d.Section(path, occurrence)
	if !ok {
		return ScriptValue{}, false
	}
	return section.GetValue(valueIndex)
}

// Get returns the scalar payload of one selected section value.
func (d *ScriptDocument) Get(path []string, occurrence, valueIndex int) (any, bool) {
	value, ok := d.GetValue(path, occurrence, valueIndex)
	if !ok {
		return nil, false
	}
	return value.Value, true
}

// Set replaces a value in one selected section. When create is true, missing
// path components are appended as paired sections.
func (d *ScriptDocument) Set(path []string, occurrence, valueIndex int, value ScriptValue, create, endTag bool) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("脚本文档为空")
	}
	normalized := normalizeScriptPath(path)
	if len(normalized) == 0 {
		return false, fmt.Errorf("section 路径不能为空")
	}
	section, ok := d.Section(normalized, occurrence)
	if !ok {
		if !create {
			return false, nil
		}
		var err error
		section, err = d.ensureSectionPath(normalized, endTag)
		if err != nil {
			return false, err
		}
		if valueIndex != 0 {
			return false, fmt.Errorf("新建 section 只能设置第 0 个值")
		}
		if err := section.Append(value); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := section.Set(value, valueIndex); err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes one selected section.
func (d *ScriptDocument) Delete(path []string, occurrence int) (bool, error) {
	section, ok := d.Section(path, occurrence)
	if !ok {
		return false, nil
	}
	if err := section.Delete(); err != nil {
		return false, err
	}
	return true, nil
}

// AppendSection appends a new root section.
func (d *ScriptDocument) AppendSection(name string, values []ScriptValue, hasEndTag bool) (*ScriptSection, error) {
	if d == nil {
		return nil, fmt.Errorf("脚本文档为空")
	}
	return d.appendSection(nil, name, values, hasEndTag)
}

func (d *ScriptDocument) appendSection(parent *ScriptSection, name string, values []ScriptValue, hasEndTag bool) (*ScriptSection, error) {
	name = normalizeScriptSectionName(name)
	if name == "" {
		return nil, fmt.Errorf("section 名称不能为空")
	}
	section := &ScriptSection{
		name:      name,
		paired:    hasEndTag,
		hasEndTag: hasEndTag,
		parent:    parent,
		document:  d,
		items:     make([]*scriptItem, 0, len(values)),
	}
	for _, value := range values {
		copyValue := cloneScriptValue(value)
		copyValue.dirty = true
		section.items = append(section.items, &scriptItem{value: &copyValue})
	}
	item := &scriptItem{section: section}
	if parent == nil {
		d.items = append(d.items, item)
	} else {
		parent.items = append(parent.items, item)
	}
	return section, nil
}

func (d *ScriptDocument) ensureSectionPath(path []string, endTag bool) (*ScriptSection, error) {
	var parent *ScriptSection
	for index, name := range path {
		children := d.rootOrChildren(parent)
		var current *ScriptSection
		for _, candidate := range children {
			if candidate.name == name {
				current = candidate
				break
			}
		}
		if current == nil {
			var err error
			current, err = d.appendSection(parent, name, nil, index == len(path)-1 && endTag)
			if err != nil {
				return nil, err
			}
		}
		parent = current
	}
	return parent, nil
}

func (d *ScriptDocument) rootOrChildren(parent *ScriptSection) []*ScriptSection {
	items := d.items
	if parent != nil {
		items = parent.items
	}
	children := make([]*ScriptSection, 0)
	for _, item := range items {
		if item.section != nil && !item.section.deleted {
			children = append(children, item.section)
		}
	}
	return children
}

// ParseScriptDocument parses a TypeScript token stream into a tree.
//
// Token-level corruption remains an error, but recoverable section-structure
// problems are retained in the tree and reported through Warnings. The
// pairing behavior follows the existing decoder: a section name is paired
// when a matching closing tag exists anywhere in the file, while names with
// no closing tag retain the legacy unpaired-section semantics.
func (a *Archive) ParseScriptDocument(raw []byte) (*ScriptDocument, error) {
	if a == nil {
		return nil, fmt.Errorf("归档为空")
	}
	tokens, err := decodeBatchTokens(raw)
	if err != nil {
		return nil, err
	}
	pairedNames := make(map[string]bool)
	for _, token := range tokens {
		if token.typ != 3 {
			continue
		}
		name, closing, ok := parseSectionTag(a.ResolveString(token.value))
		if ok && closing {
			pairedNames[name] = true
		}
	}
	document := &ScriptDocument{items: make([]*scriptItem, 0, len(tokens))}
	stack := make([]*ScriptSection, 0)
	activeUnpaired := make(map[int]*ScriptSection)
	closeUnpaired := func(depth int) {
		delete(activeUnpaired, depth)
	}
	appendItem := func(container *ScriptSection, item *scriptItem) {
		if container == nil {
			document.items = append(document.items, item)
			return
		}
		container.items = append(container.items, item)
	}
	currentContainer := func() *ScriptSection {
		if unpaired := activeUnpaired[len(stack)]; unpaired != nil {
			return unpaired
		}
		if len(stack) > 0 {
			return stack[len(stack)-1]
		}
		return nil
	}
	addWarning := func(code ScriptParseWarningCode, message string, tokenIndex int, section *ScriptSection) {
		warning := ScriptParseWarning{
			Code:       code,
			Message:    message,
			TokenIndex: tokenIndex,
		}
		if section != nil {
			warning.SectionPath = section.Path()
		}
		document.warnings = append(document.warnings, warning)
	}

	for tokenIndex, token := range tokens {
		if token.typ == 3 {
			name, closing, ok := parseSectionTag(a.ResolveString(token.value))
			if ok {
				depth := len(stack)
				closeUnpaired(depth)
				if closing {
					match := -1
					for index := len(stack) - 1; index >= 0; index-- {
						if stack[index].name == name {
							match = index
							break
						}
					}
					if match < 0 {
						container := currentContainer()
						addWarning(
							ScriptWarningOrphanClose,
							fmt.Sprintf("section [%s] 缺少开始标签，已保留原始结束标签", name),
							tokenIndex,
							container,
						)
						rawToken := token
						appendItem(container, &scriptItem{rawToken: &rawToken})
						continue
					}
					for index := len(stack) - 1; index > match; index-- {
						unclosed := stack[index]
						unclosed.hasEndTag = false
						addWarning(
							ScriptWarningMismatchedClose,
							fmt.Sprintf("section [%s] 在结束标签 [/%s] 前缺少结束标签，已继续恢复", unclosed.name, name),
							tokenIndex,
							unclosed,
						)
					}
					section := stack[match]
					section.closeToken = token
					section.hasClose = true
					section.hasEndTag = true
					stack = stack[:match]
					continue
				}

				parent := (*ScriptSection)(nil)
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				section := &ScriptSection{
					name:      name,
					paired:    pairedNames[name],
					hasEndTag: false,
					parent:    parent,
					document:  document,
					items:     make([]*scriptItem, 0),
					openToken: token,
				}
				item := &scriptItem{section: section}
				appendItem(parent, item)
				if section.paired {
					stack = append(stack, section)
				} else {
					activeUnpaired[depth] = section
				}
				continue
			}
		}

		container := currentContainer()
		value := scriptValueFromToken(a, token)
		item := &scriptItem{value: &value}
		appendItem(container, item)
	}
	for index := len(stack) - 1; index >= 0; index-- {
		section := stack[index]
		section.hasEndTag = false
		addWarning(
			ScriptWarningMissingClose,
			fmt.Sprintf("section [%s] 缺少结束标签，已按文件末尾恢复", section.name),
			len(tokens),
			section,
		)
	}
	return document, nil
}

func scriptValueFromToken(a *Archive, token batchToken) ScriptValue {
	value := ScriptValue{raw: token, rawValid: true}
	switch token.typ {
	case 0:
		value.Type = ScriptTokenInteger
		value.Value = int64(token.value)
	case 2:
		value.Type = ScriptTokenFloat
		value.Value = float64(math.Float32frombits(uint32(token.value)))
	case 3:
		value.Type = ScriptTokenString
		value.Value = a.ResolveString(token.value)
		value.Pool = scriptPoolForOffset(token.value)
	case 5:
		value.Type = ScriptTokenBlock5
		value.Value, value.Pool = a.scriptBlockValue(token.value)
	case 6:
		value.Type = ScriptTokenQuoted
		value.Value = a.ResolveString(token.value)
		value.Pool = scriptPoolForOffset(token.value)
	case 7:
		value.Type = ScriptTokenBlock7
		value.Value, value.Pool = a.scriptBlockValue(token.value)
	default:
		value.Type = ScriptTokenString
		value.Value = a.ResolveString(token.value)
		value.Pool = scriptPoolForOffset(token.value)
	}
	return value
}

func (a *Archive) scriptBlockValue(raw int32) (any, ScriptStringPool) {
	if text, pool, ok := a.scriptStringAtOffset(raw); ok {
		return text, pool
	}
	return int64(raw), ""
}

func (a *Archive) scriptStringAtOffset(raw int32) (string, ScriptStringPool, bool) {
	if a == nil || raw < 0 {
		return "", "", false
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if raw&1 == 0 {
		start := int(raw >> 1)
		if start < 0 || start >= len(a.strA) {
			return "", "", false
		}
		return readUTF8(a.strA, start), ScriptPoolUTF8, true
	}
	start := int(raw>>1) * 2
	if start < 0 || start+1 >= len(a.strW) {
		return "", "", false
	}
	return readUTF16(a.strW, start), ScriptPoolUTF16, true
}

func scriptPoolForOffset(offset int32) ScriptStringPool {
	if offset&1 != 0 {
		return ScriptPoolUTF16
	}
	return ScriptPoolUTF8
}

func cloneScriptValue(value ScriptValue) ScriptValue {
	return value
}

// EncodeScriptDocument serializes a structured document using the receiver's
// string pools. Untouched values/section tags retain their original raw token
// so parse -> encode remains byte-stable when the tree was not modified.
func (a *Archive) EncodeScriptDocument(document *ScriptDocument) ([]byte, error) {
	if a == nil || document == nil {
		return nil, fmt.Errorf("脚本文档为空")
	}
	tokens := make([]batchToken, 0)
	var encodeItems func([]*scriptItem) error
	encodeItems = func(items []*scriptItem) error {
		for _, item := range items {
			if item == nil {
				continue
			}
			if item.rawToken != nil {
				tokens = append(tokens, *item.rawToken)
				continue
			}
			if item.value != nil {
				token, err := a.encodeScriptValue(*item.value)
				if err != nil {
					return err
				}
				tokens = append(tokens, token)
				continue
			}
			section := item.section
			if section == nil || section.deleted {
				continue
			}
			open := section.openToken
			if section.openTokenEqualsZero() {
				open = batchToken{typ: 3, value: a.StringOffset("[" + section.name + "]")}
			}
			tokens = append(tokens, open)
			if err := encodeItems(section.items); err != nil {
				return err
			}
			if section.hasEndTag {
				close := section.closeToken
				if !section.hasClose {
					close = batchToken{typ: 3, value: a.StringOffset("[/" + section.name + "]")}
				}
				tokens = append(tokens, close)
			}
		}
		return nil
	}
	if err := encodeItems(document.items); err != nil {
		return nil, err
	}
	return encodeBatchTokens(tokens), nil
}

func (s *ScriptSection) openTokenEqualsZero() bool {
	return s.openToken.typ == 0 && s.openToken.value == 0
}

func (a *Archive) encodeScriptValue(value ScriptValue) (batchToken, error) {
	if value.rawValid && !value.dirty {
		return value.raw, nil
	}
	switch value.Type {
	case ScriptTokenInteger:
		number, ok := scriptNumber(value.Value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt32 || number > math.MaxInt32 {
			return batchToken{}, fmt.Errorf("整数值超出 int32 范围")
		}
		return batchToken{typ: 0, value: int32(number)}, nil
	case ScriptTokenFloat:
		number, ok := scriptNumber(value.Value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return batchToken{}, fmt.Errorf("浮点值非法")
		}
		floatValue := float32(number)
		if math.IsInf(float64(floatValue), 0) || math.IsNaN(float64(floatValue)) {
			return batchToken{}, fmt.Errorf("浮点值超出 float32 范围")
		}
		return batchToken{typ: 2, value: int32(math.Float32bits(floatValue))}, nil
	case ScriptTokenString, ScriptTokenQuoted:
		text, ok := value.Value.(string)
		if !ok {
			return batchToken{}, fmt.Errorf("字符串值非法")
		}
		return batchToken{typ: scriptTokenByte(value.Type), value: a.scriptStringOffset(text, value.Pool)}, nil
	case ScriptTokenBlock5, ScriptTokenBlock7:
		if number, ok := scriptNumber(value.Value); ok {
			if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt32 || number > math.MaxInt32 {
				return batchToken{}, fmt.Errorf("块值超出 int32 范围")
			}
			return batchToken{typ: scriptTokenByte(value.Type), value: int32(number)}, nil
		}
		text, ok := value.Value.(string)
		if !ok {
			return batchToken{}, fmt.Errorf("块值非法")
		}
		return batchToken{typ: scriptTokenByte(value.Type), value: a.scriptStringOffset(text, value.Pool)}, nil
	default:
		return batchToken{}, fmt.Errorf("未知脚本值类型: %s", value.Type)
	}
}

func scriptTokenByte(kind ScriptTokenType) byte {
	switch kind {
	case ScriptTokenInteger:
		return 0
	case ScriptTokenFloat:
		return 2
	case ScriptTokenString:
		return 3
	case ScriptTokenQuoted:
		return 6
	case ScriptTokenBlock5:
		return 5
	case ScriptTokenBlock7:
		return 7
	default:
		return 3
	}
}

func (a *Archive) scriptStringOffset(value string, pool ScriptStringPool) int32 {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.ensureStringIndexesLocked()
	if pool == ScriptPoolUTF16 {
		if offset, ok := a.strWIdx[value]; ok {
			return offset
		}
		old := len(a.strW)
		if old&1 != 0 {
			a.strW = append(a.strW, 0)
			old++
		}
		for _, unit := range utf16.Encode([]rune(value)) {
			var encoded [2]byte
			binary.LittleEndian.PutUint16(encoded[:], unit)
			a.strW = append(a.strW, encoded[:]...)
		}
		a.strW = append(a.strW, 0, 0)
		offset := int32((old>>1)<<1 | 1)
		a.strWIdx[value] = offset
		a.poolsDirty = true
		return offset
	}
	if offset, ok := a.strAIdx[value]; ok {
		return offset
	}
	old := len(a.strA)
	a.strA = append(a.strA, value...)
	a.strA = append(a.strA, 0)
	offset := int32(old << 1)
	a.strAIdx[value] = offset
	a.poolsDirty = true
	return offset
}

func normalizeScriptPath(path []string) []string {
	result := make([]string, 0, len(path))
	for _, raw := range path {
		name := normalizeScriptSectionName(raw)
		if name != "" {
			result = append(result, name)
		}
	}
	return result
}

func normalizeScriptSectionName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") && len(name) > 2 {
		name = name[1 : len(name)-1]
	}
	return strings.TrimPrefix(name, "/")
}

func scriptPathEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// SortScriptSectionsByPath gives callers a deterministic order when combining
// sections obtained from multiple documents.
func SortScriptSectionsByPath(sections []*ScriptSection) {
	sort.SliceStable(sections, func(left, right int) bool {
		return strings.Join(sections[left].Path(), "\x00") < strings.Join(sections[right].Path(), "\x00")
	})
}
