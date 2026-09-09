package pvf

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// batchToken is the in-memory representation of one five-byte TypeScript
// token. It intentionally mirrors type1Token, but keeps the batch editor
// implementation separate from the text encoder.
type batchToken struct {
	typ   byte
	value int32
}

type batchSection struct {
	name          string
	open          int
	close         int
	endExclusive  int
	paired        bool
	directTokens  []int
	parentSection []string
}

type batchScript struct {
	tokens         []batchToken
	sections       []batchSection
	topLevelTokens []int
}

type BatchTransformResult struct {
	raw        []byte
	warnings   []string
	matchCount int
	changed    bool
}

func (r BatchTransformResult) Raw() []byte        { return append([]byte(nil), r.raw...) }
func (r BatchTransformResult) Warnings() []string { return append([]string(nil), r.warnings...) }
func (r BatchTransformResult) MatchCount() int    { return r.matchCount }
func (r BatchTransformResult) Changed() bool      { return r.changed }

// parseBatchScript parses a TypeScript token stream while retaining the
// section spans needed by structural edits. The pairing rule intentionally
// follows decodeScript: a section name is treated as paired when a matching
// closing tag exists anywhere in the file.
func (a *Archive) parseBatchScript(raw []byte) (batchScript, error) {
	tokens, err := decodeBatchTokens(raw)
	if err != nil {
		return batchScript{}, err
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

	sections := make([]batchSection, 0)
	topLevelTokens := make([]int, 0)
	stack := make([]int, 0)
	// Unpaired sections are not pushed onto stack by the normal PVF decoder.
	// Keep the current one per nesting depth so its span ends at the next
	// section tag at the same semantic depth.
	activeUnpaired := make(map[int]int)

	closeUnpairedAt := func(index, end int) {
		if sectionIndex, ok := activeUnpaired[index]; ok {
			if sections[sectionIndex].endExclusive == 0 || sections[sectionIndex].endExclusive > end {
				sections[sectionIndex].endExclusive = end
			}
			delete(activeUnpaired, index)
		}
	}

	for index, token := range tokens {
		if token.typ == 3 {
			name, closing, ok := parseSectionTag(a.ResolveString(token.value))
			if ok {
				depth := len(stack)
				closeUnpairedAt(depth, index)
				if closing {
					match := -1
					for position := len(stack) - 1; position >= 0; position-- {
						if sections[stack[position]].name == name {
							match = position
							break
						}
					}
					if match < 0 {
						return batchScript{}, fmt.Errorf("未找到 section [%s] 的开始标签", name)
					}
					for position := len(stack) - 1; position >= match; position-- {
						sectionIndex := stack[position]
						if position == match {
							sections[sectionIndex].close = index
							sections[sectionIndex].endExclusive = index + 1
						} else {
							return batchScript{}, fmt.Errorf("section [%s] 在 [%s] 之前未闭合", sections[sectionIndex].name, name)
						}
					}
					stack = stack[:match]
					continue
				}

				section := batchSection{
					name:          name,
					open:          index,
					close:         -1,
					paired:        pairedNames[name],
					parentSection: currentBatchSectionPath(sections, stack),
				}
				sectionIndex := len(sections)
				sections = append(sections, section)
				if section.paired {
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
			topLevelTokens = append(topLevelTokens, index)
			continue
		}
		sections[sectionIndex].directTokens = append(sections[sectionIndex].directTokens, index)
	}

	for depth, sectionIndex := range activeUnpaired {
		_ = depth
		if sections[sectionIndex].endExclusive == 0 {
			sections[sectionIndex].endExclusive = len(tokens)
		}
	}
	for _, sectionIndex := range stack {
		section := sections[sectionIndex]
		if section.paired && section.close < 0 {
			return batchScript{}, fmt.Errorf("section [%s] 缺少结束标签", section.name)
		}
	}

	return batchScript{tokens: tokens, sections: sections, topLevelTokens: topLevelTokens}, nil
}

func currentBatchSectionPath(sections []batchSection, stack []int) []string {
	path := make([]string, 0, len(stack))
	for _, index := range stack {
		path = append(path, sections[index].name)
	}
	return path
}

func decodeBatchTokens(raw []byte) ([]batchToken, error) {
	if len(raw)%5 != 0 {
		return nil, fmt.Errorf("脚本 payload 长度不是 5 的倍数")
	}
	tokens := make([]batchToken, len(raw)/5)
	for index := range tokens {
		base := index * 5
		typ := raw[base]
		switch typ {
		case 0, 2, 3, 5, 6, 7:
		default:
			return nil, fmt.Errorf("脚本包含不支持的 token 类型: %d", typ)
		}
		tokens[index] = batchToken{
			typ:   typ,
			value: int32(binary.LittleEndian.Uint32(raw[base+1:])),
		}
	}
	return tokens, nil
}

func encodeBatchTokens(tokens []batchToken) []byte {
	raw := make([]byte, len(tokens)*5)
	for index, token := range tokens {
		base := index * 5
		raw[base] = token.typ
		binary.LittleEndian.PutUint32(raw[base+1:], uint32(token.value))
	}
	return raw
}

func (script batchScript) matchingSections(name string) []batchSection {
	name = normalizeBatchSectionName(name)
	result := make([]batchSection, 0)
	for _, section := range script.sections {
		if section.name == name {
			result = append(result, section)
		}
	}
	return result
}

func normalizeBatchSectionName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") && len(name) > 2 {
		name = name[1 : len(name)-1]
	}
	return strings.TrimPrefix(name, "/")
}

func (a *Archive) parseBatchValue(value string, allowMany, allowSectionTags bool) ([]batchToken, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("值不能为空")
	}
	raw, err := a.encodeScript(value)
	if err != nil {
		return nil, err
	}
	tokens, err := decodeBatchTokens(raw)
	if err != nil {
		return nil, err
	}
	if !allowMany && len(tokens) != 1 {
		return nil, fmt.Errorf("设置值必须只包含一个 token")
	}
	for _, token := range tokens {
		if token.typ != 3 {
			continue
		}
		if !allowSectionTags {
			if name, _, ok := parseSectionTag(a.ResolveString(token.value)); ok {
				return nil, fmt.Errorf("值不能包含 section 标签 [%s]", name)
			}
		}
	}
	return tokens, nil
}

func (a *Archive) transformBatchScript(raw []byte, operations []StructuredBatchOperation) (BatchTransformResult, error) {
	script, err := a.parseBatchScript(raw)
	if err != nil {
		return BatchTransformResult{}, err
	}
	result := BatchTransformResult{raw: append([]byte(nil), raw...)}
	for _, operation := range operations {
		current, err := a.parseBatchScript(encodeBatchTokens(script.tokens))
		if err != nil {
			return BatchTransformResult{}, err
		}
		changed, matches, warning, err := a.applyBatchOperation(&current, operation)
		if err != nil {
			return BatchTransformResult{}, err
		}
		result.matchCount += matches
		if warning != "" {
			result.warnings = append(result.warnings, warning)
		}
		if changed {
			result.changed = true
		}
		script = current
	}
	result.raw = encodeBatchTokens(script.tokens)
	return result, nil
}

// TransformStructuredBatch applies structural operations to raw TypeScript
// payload bytes without changing the receiver's overlay. New string values
// are resolved through the receiver's current string-pool state.
func (a *Archive) TransformStructuredBatch(raw []byte, operations []StructuredBatchOperation) (BatchTransformResult, error) {
	return a.transformBatchScript(raw, operations)
}

// StructuredBatchOperation is kept in the pvf package so the binary editor
// remains testable without depending on Wails service DTOs.
type StructuredBatchOperation struct {
	Kind            string
	Section         string
	TokenIndex      int
	Value           string
	CreateIfMissing bool
	Operator        string
	Operand         string
	OperandEnd      string
	AnchorSection   string
	HasEndTag       bool
}

func (a *Archive) applyBatchOperation(script *batchScript, operation StructuredBatchOperation) (bool, int, string, error) {
	kind := strings.ToLower(strings.TrimSpace(operation.Kind))
	switch kind {
	case "set":
		return a.applyBatchSet(script, operation)
	case "number":
		return a.applyBatchNumber(script, operation)
	case "delete":
		return applyBatchDelete(script, operation)
	case "insert":
		return a.applyBatchInsert(script, operation)
	default:
		return false, 0, "", fmt.Errorf("未知结构化操作: %s", operation.Kind)
	}
}

func (a *Archive) applyBatchSet(script *batchScript, operation StructuredBatchOperation) (bool, int, string, error) {
	targets := script.matchingSections(operation.Section)
	if len(targets) == 0 {
		if operation.CreateIfMissing {
			values, err := a.parseBatchValue(operation.Value, true, true)
			if err != nil {
				return false, 0, "", err
			}
			return a.appendBatchSetSection(script, operation, values)
		}
		return false, 0, fmt.Sprintf("缺少 section [%s]", normalizeBatchSectionName(operation.Section)), nil
	}
	values, err := a.parseBatchValue(operation.Value, true, true)
	if err != nil {
		return false, 0, "", err
	}
	if operation.TokenIndex < 0 {
		return a.applyBatchSetAll(script, targets, values, normalizeBatchSectionName(operation.Section))
	}
	type replacementTarget struct {
		index int
	}
	replacements := make([]replacementTarget, 0, len(targets))
	for _, section := range targets {
		if operation.TokenIndex < 0 || operation.TokenIndex >= len(section.directTokens) {
			continue
		}
		replacements = append(replacements, replacementTarget{
			index: section.directTokens[operation.TokenIndex],
		})
	}
	if len(replacements) == 0 {
		return false, 0, fmt.Sprintf("section [%s] 没有第 %d 个直接值", normalizeBatchSectionName(operation.Section), operation.TokenIndex), nil
	}
	sort.Slice(replacements, func(left, right int) bool {
		return replacements[left].index > replacements[right].index
	})
	changed := false
	for _, replacement := range replacements {
		if len(values) != 1 || script.tokens[replacement.index] != values[0] {
			changed = true
		}
		updated := make([]batchToken, 0, len(script.tokens)-1+len(values))
		updated = append(updated, script.tokens[:replacement.index]...)
		updated = append(updated, values...)
		updated = append(updated, script.tokens[replacement.index+1:]...)
		script.tokens = updated
	}
	if _, err := a.parseBatchScript(encodeBatchTokens(script.tokens)); err != nil {
		return false, 0, "", fmt.Errorf("设置值中的 section 结构无效: %w", err)
	}
	if len(replacements) < len(targets) {
		return changed, len(replacements), fmt.Sprintf("section [%s] 有 %d 个重复项缺少第 %d 个直接值", normalizeBatchSectionName(operation.Section), len(targets)-len(replacements), operation.TokenIndex), nil
	}
	return changed, len(replacements), "", nil
}

func (a *Archive) applyBatchSetAll(script *batchScript, targets []batchSection, values []batchToken, sectionName string) (bool, int, string, error) {
	valueScript, err := a.parseBatchScript(encodeBatchTokens(values))
	if err != nil {
		return false, 0, "", fmt.Errorf("设置值中的 section 结构无效: %w", err)
	}
	newValueCount := len(valueScript.topLevelTokens)

	removeIndexes := make(map[int]struct{})
	insertions := make(map[int][][]batchToken)
	changed := false
	countMismatches := make(map[int]int)
	typeMismatches := make(map[[2]byte]int)
	newValues := make([]batchToken, 0, len(valueScript.topLevelTokens))
	for _, index := range valueScript.topLevelTokens {
		newValues = append(newValues, values[index])
	}
	for _, section := range targets {
		oldValues := make([]batchToken, 0, len(section.directTokens))
		for _, index := range section.directTokens {
			oldValues = append(oldValues, script.tokens[index])
			removeIndexes[index] = struct{}{}
		}
		if len(oldValues) != len(values) || !batchTokensEqual(oldValues, values) {
			changed = true
		}
		if len(oldValues) != newValueCount {
			countMismatches[len(oldValues)]++
		}
		for index := 0; index < len(oldValues) && index < len(newValues); index++ {
			if oldValues[index].typ != newValues[index].typ {
				typeMismatches[[2]byte{oldValues[index].typ, newValues[index].typ}]++
			}
		}
		insertAt := section.open + 1
		if len(section.directTokens) > 0 {
			insertAt = section.directTokens[0]
		}
		insertions[insertAt] = append(insertions[insertAt], append([]batchToken(nil), values...))
	}

	updated := make([]batchToken, 0, len(script.tokens)-len(removeIndexes)+len(values)*len(targets))
	for index := 0; index <= len(script.tokens); index++ {
		for _, insertion := range insertions[index] {
			updated = append(updated, insertion...)
		}
		if index == len(script.tokens) {
			break
		}
		if _, remove := removeIndexes[index]; remove {
			continue
		}
		updated = append(updated, script.tokens[index])
	}
	if _, err := a.parseBatchScript(encodeBatchTokens(updated)); err != nil {
		return false, 0, "", fmt.Errorf("设置值中的 section 结构无效: %w", err)
	}
	script.tokens = updated

	warningParts := make([]string, 0, len(countMismatches))
	counts := make([]int, 0, len(countMismatches))
	for oldCount := range countMismatches {
		counts = append(counts, oldCount)
	}
	sort.Ints(counts)
	for _, oldCount := range counts {
		warningParts = append(warningParts, fmt.Sprintf(
			"section [%s] 有 %d 个匹配项的 value 数量为 %d，设置值数量为 %d",
			sectionName,
			countMismatches[oldCount],
			oldCount,
			newValueCount,
		))
	}
	typePairs := make([][2]byte, 0, len(typeMismatches))
	for pair := range typeMismatches {
		typePairs = append(typePairs, pair)
	}
	sort.Slice(typePairs, func(left, right int) bool {
		if typePairs[left][0] != typePairs[right][0] {
			return typePairs[left][0] < typePairs[right][0]
		}
		return typePairs[left][1] < typePairs[right][1]
	})
	for _, pair := range typePairs {
		warningParts = append(warningParts, fmt.Sprintf(
			"section [%s] 有 %d 个 value 类型不一致（%s → %s）",
			sectionName,
			typeMismatches[pair],
			batchTokenTypeName(pair[0]),
			batchTokenTypeName(pair[1]),
		))
	}
	return changed, len(targets), strings.Join(warningParts, "；"), nil
}

func batchTokensEqual(left, right []batchToken) bool {
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

func batchTokenTypeName(tokenType byte) string {
	switch tokenType {
	case 0:
		return "整数"
	case 2:
		return "浮点数"
	case 3:
		return "字符串"
	case 5:
		return "块值(5)"
	case 6:
		return "带引号字符串"
	case 7:
		return "块值(7)"
	default:
		return fmt.Sprintf("token(%d)", tokenType)
	}
}

func (a *Archive) appendBatchSetSection(script *batchScript, operation StructuredBatchOperation, values []batchToken) (bool, int, string, error) {
	sectionName := normalizeBatchSectionName(operation.Section)
	if sectionName == "" {
		return false, 0, "", fmt.Errorf("设置 section 名称不能为空")
	}
	inserted := make([]batchToken, 0, len(values)+2)
	inserted = append(inserted, batchToken{typ: 3, value: a.StringOffset("[" + sectionName + "]")})
	inserted = append(inserted, values...)
	if operation.HasEndTag {
		inserted = append(inserted, batchToken{typ: 3, value: a.StringOffset("[/" + sectionName + "]")})
	}
	updated := make([]batchToken, 0, len(script.tokens)+len(inserted))
	updated = append(updated, script.tokens...)
	updated = append(updated, inserted...)
	if _, err := a.parseBatchScript(encodeBatchTokens(updated)); err != nil {
		return false, 0, "", fmt.Errorf("插入 section 结构无效: %w", err)
	}
	script.tokens = updated
	return true, 1, "", nil
}

func (a *Archive) applyBatchNumber(script *batchScript, operation StructuredBatchOperation) (bool, int, string, error) {
	targets := script.matchingSections(operation.Section)
	if len(targets) == 0 {
		return false, 0, fmt.Sprintf("缺少 section [%s]", normalizeBatchSectionName(operation.Section)), nil
	}
	operator := strings.TrimSpace(operation.Operator)
	operand, err := strconv.ParseFloat(strings.TrimSpace(operation.Operand), 64)
	if err != nil {
		return false, 0, "", fmt.Errorf("数值操作数无效: %q", operation.Operand)
	}
	operandEnd := 0.0
	if operator == "clamp" {
		operandEnd, err = strconv.ParseFloat(strings.TrimSpace(operation.OperandEnd), 64)
		if err != nil {
			return false, 0, "", fmt.Errorf("clamp 上限无效: %q", operation.OperandEnd)
		}
		if operand > operandEnd {
			return false, 0, "", fmt.Errorf("clamp 下限不能大于上限")
		}
	}
	if operator == "round" {
		precision, parseErr := strconv.Atoi(strings.TrimSpace(operation.Operand))
		if parseErr != nil || precision < 0 || precision > 9 {
			return false, 0, "", fmt.Errorf("round 小数位必须是 0 到 9 的整数")
		}
		operand = float64(precision)
	}

	changed := false
	matched := 0
	for _, section := range targets {
		if operation.TokenIndex < 0 || operation.TokenIndex >= len(section.directTokens) {
			continue
		}
		tokenIndex := section.directTokens[operation.TokenIndex]
		current, ok := batchNumericValue(script.tokens[tokenIndex])
		if !ok {
			return false, 0, "", fmt.Errorf("section [%s] 的第 %d 个值不是数字", normalizeBatchSectionName(operation.Section), operation.TokenIndex)
		}
		value, err := applyBatchNumericOperator(current, operator, operand, operandEnd)
		if err != nil {
			return false, 0, "", err
		}
		next, err := batchNumericToken(value, script.tokens[tokenIndex].typ)
		if err != nil {
			return false, 0, "", err
		}
		matched++
		if next != script.tokens[tokenIndex] {
			changed = true
			script.tokens[tokenIndex] = next
		}
	}
	if matched == 0 {
		return false, 0, fmt.Sprintf("section [%s] 没有第 %d 个直接值", normalizeBatchSectionName(operation.Section), operation.TokenIndex), nil
	}
	if matched < len(targets) {
		return changed, matched, fmt.Sprintf("section [%s] 有 %d 个重复项缺少第 %d 个直接值", normalizeBatchSectionName(operation.Section), len(targets)-matched, operation.TokenIndex), nil
	}
	return changed, matched, "", nil
}

func batchNumericValue(token batchToken) (float64, bool) {
	switch token.typ {
	case 0:
		return float64(token.value), true
	case 2:
		return float64(math.Float32frombits(uint32(token.value))), true
	default:
		return 0, false
	}
}

func applyBatchNumericOperator(current float64, operator string, operand, operandEnd float64) (float64, error) {
	var result float64
	switch operator {
	case "=":
		result = operand
	case "+":
		result = current + operand
	case "-":
		result = current - operand
	case "×", "*":
		result = current * operand
	case "÷", "/":
		if operand == 0 {
			return 0, fmt.Errorf("除数不能为 0")
		}
		result = current / operand
	case "+%":
		result = current * (1 + operand/100)
	case "-%":
		result = current * (1 - operand/100)
	case "min":
		result = math.Min(current, operand)
	case "max":
		result = math.Max(current, operand)
	case "clamp":
		result = math.Min(math.Max(current, operand), operandEnd)
	case "round":
		factor := math.Pow10(int(operand))
		result = math.Round(current*factor) / factor
	default:
		return 0, fmt.Errorf("未知数值运算符: %s", operator)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("数值运算结果溢出")
	}
	return result, nil
}

func batchNumericToken(value float64, originalType byte) (batchToken, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return batchToken{}, fmt.Errorf("数值运算结果非法")
	}
	if originalType == 0 && math.Trunc(value) == value && value >= math.MinInt32 && value <= math.MaxInt32 {
		return batchToken{typ: 0, value: int32(value)}, nil
	}
	f := float32(value)
	if math.IsInf(float64(f), 0) || math.IsNaN(float64(f)) {
		return batchToken{}, fmt.Errorf("数值运算结果超出 float32 范围")
	}
	return batchToken{typ: 2, value: int32(math.Float32bits(f))}, nil
}

func applyBatchDelete(script *batchScript, operation StructuredBatchOperation) (bool, int, string, error) {
	targets := script.matchingSections(operation.Section)
	if len(targets) == 0 {
		return false, 0, fmt.Sprintf("缺少 section [%s]", normalizeBatchSectionName(operation.Section)), nil
	}
	type span struct{ start, end int }
	spans := make([]span, 0, len(targets))
	for _, section := range targets {
		end := section.endExclusive
		if end <= section.open {
			end = section.open + 1
		}
		spans = append(spans, span{start: section.open, end: end})
	}
	sort.Slice(spans, func(left, right int) bool {
		if spans[left].start != spans[right].start {
			return spans[left].start < spans[right].start
		}
		return spans[left].end > spans[right].end
	})
	merged := make([]span, 0, len(spans))
	for _, item := range spans {
		if len(merged) > 0 && item.start < merged[len(merged)-1].end {
			if item.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	for index := len(merged) - 1; index >= 0; index-- {
		item := merged[index]
		script.tokens = append(script.tokens[:item.start], script.tokens[item.end:]...)
	}
	return true, len(merged), "", nil
}

func (a *Archive) applyBatchInsert(script *batchScript, operation StructuredBatchOperation) (bool, int, string, error) {
	sectionName := normalizeBatchSectionName(operation.Section)
	if sectionName == "" {
		return false, 0, "", fmt.Errorf("插入 section 名称不能为空")
	}
	values, err := a.parseBatchValue(operation.Value, true, true)
	if err != nil {
		return false, 0, "", err
	}
	insertAt := len(script.tokens)
	if strings.TrimSpace(operation.AnchorSection) != "" {
		anchors := script.matchingSections(operation.AnchorSection)
		if len(anchors) == 0 {
			return false, 0, fmt.Sprintf("缺少锚点 section [%s]", normalizeBatchSectionName(operation.AnchorSection)), nil
		}
		insertAt = anchors[0].endExclusive
	}
	inserted := make([]batchToken, 0, len(values)+2)
	inserted = append(inserted, batchToken{typ: 3, value: a.StringOffset("[" + sectionName + "]")})
	inserted = append(inserted, values...)
	if operation.HasEndTag {
		inserted = append(inserted, batchToken{typ: 3, value: a.StringOffset("[/" + sectionName + "]")})
	}
	updated := make([]batchToken, 0, len(script.tokens)+len(inserted))
	updated = append(updated, script.tokens[:insertAt]...)
	updated = append(updated, inserted...)
	updated = append(updated, script.tokens[insertAt:]...)
	if _, err := a.parseBatchScript(encodeBatchTokens(updated)); err != nil {
		return false, 0, "", fmt.Errorf("插入值中的 section 结构无效: %w", err)
	}
	script.tokens = updated
	return true, 1, "", nil
}

// cloneForBatch creates an isolated archive state. It shares immutable source
// bytes, but copies every mutable pool, overlay and index so preview can never
// alter the live archive.
func (a *Archive) cloneForBatch() *Archive {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	clone := &Archive{
		data:            a.data,
		hdr:             a.hdr,
		guard:           a.guard,
		sourcePath:      a.sourcePath,
		tableOff:        a.tableOff,
		hashOff:         a.hashOff,
		nameOff:         a.nameOff,
		grpiOff:         a.grpiOff,
		bodyOff:         a.bodyOff,
		hashSize:        a.hashSize,
		nameSize:        a.nameSize,
		grpiSize:        a.grpiSize,
		items:           append([]fileItem(nil), a.items...),
		groups:          append([]groupItem(nil), a.groups...),
		strA:            append([]byte(nil), a.strA...),
		strW:            append([]byte(nil), a.strW...),
		strAIdx:         cloneStringOffsetMap(a.strAIdx),
		strWIdx:         cloneStringOffsetMap(a.strWIdx),
		poolsDirty:      a.poolsDirty,
		resolveCache:    cloneStringMap(a.resolveCache),
		chunkCache:      make(map[int32][]byte),
		overlay:         cloneBytesMap(a.overlay),
		pathIndex:       cloneInt32Map(a.pathIndex),
		structuralDirty: a.structuralDirty,
		removedSpans:    cloneRemovedSpans(a.removedSpans),
	}
	return clone
}

// CloneForBatch returns an isolated mutable archive state for preview/staging.
func (a *Archive) CloneForBatch() *Archive { return a.cloneForBatch() }

func cloneStringOffsetMap(values map[string]int32) map[string]int32 {
	if values == nil {
		return nil
	}
	result := make(map[string]int32, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneStringMap(values map[int32]string) map[int32]string {
	if values == nil {
		return map[int32]string{}
	}
	result := make(map[int32]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneBytesMap(values map[int32][]byte) map[int32][]byte {
	result := make(map[int32][]byte, len(values))
	for key, value := range values {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

func cloneInt32Map(values map[string]int32) map[string]int32 {
	if values == nil {
		return nil
	}
	result := make(map[string]int32, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

// commitBatch copies staged string pools and selected payloads into the live
// archive. The caller must hold the service write lock and must validate every
// selected index before calling this method.
func (a *Archive) commitBatch(stage *Archive, selected map[int32]struct{}) error {
	if stage == nil {
		return fmt.Errorf("批处理暂存状态为空")
	}
	nextOverlay := cloneBytesMap(a.overlay)
	for index := range selected {
		payload, ok := stage.overlay[index]
		if !ok {
			return fmt.Errorf("批处理文件 %d 缺少暂存 payload", index)
		}
		nextOverlay[index] = append([]byte(nil), payload...)
	}

	a.cacheMu.Lock()
	a.strA = append([]byte(nil), stage.strA...)
	a.strW = append([]byte(nil), stage.strW...)
	a.strAIdx = cloneStringOffsetMap(stage.strAIdx)
	a.strWIdx = cloneStringOffsetMap(stage.strWIdx)
	a.poolsDirty = stage.poolsDirty
	a.resolveCache = cloneStringMap(stage.resolveCache)
	a.overlay = nextOverlay
	a.cacheMu.Unlock()
	return nil
}

// CommitBatch atomically copies staged pools and selected file payloads into
// the receiver. Callers must serialize this operation with archive access.
func (a *Archive) CommitBatch(stage *Archive, selected map[int32]struct{}) error {
	return a.commitBatch(stage, selected)
}
