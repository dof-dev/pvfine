package annotations

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"pvfine/internal/pvf"
)

// PreviewFieldValue is one occurrence of a shared field in an editor view.
// Values contains the selected token(s). For a repeated record target it
// contains the complete record so consumers can use context and neighboring
// values (for example, skill context + skill id + level).
type PreviewFieldValue struct {
	Field   FieldDefinition
	Values  []string
	Context string
	Start   int
	End     int
}

// resolveFieldRule lets legacy rules opt into the shared field definition
// without changing the existing rule JSON shape. A rule's id, description and
// group remain local; non-empty target/match/annotation values override the
// field when supplied.
func resolveFieldRule(rule Rule, fields []FieldDefinition) (Rule, error) {
	fieldID := strings.TrimSpace(rule.Field)
	if fieldID == "" {
		return rule, nil
	}
	for _, field := range fields {
		if field.ID != fieldID {
			continue
		}
		resolved := Rule{
			ID:          rule.ID,
			Description: rule.Description,
			Field:       rule.Field,
			Match:       field.Match,
			Target:      field.Target,
			Annotation:  field.Annotation,
			Group:       rule.Group,
		}
		if len(rule.Match.Extensions) > 0 || rule.Match.Glob != "" {
			resolved.Match = rule.Match
		}
		if rule.Target.Kind != "" {
			resolved.Target = rule.Target
		}
		if rule.Annotation.Title != "" || rule.Annotation.Type != "" ||
			rule.Annotation.Content != "" || len(rule.Annotation.Values) > 0 ||
			rule.Annotation.Relation != "" || rule.Annotation.InlineImage {
			resolved.Annotation = rule.Annotation
		}
		return resolved, nil
	}
	return Rule{}, fmt.Errorf("引用了不存在的共享字段: %s", fieldID)
}

// PreviewFields returns the provider-specific shared fields that match a
// path, in stable display order. The returned definitions are copies of the
// immutable engine document and are safe for callers to retain.
func (e *Engine) PreviewFields(filePath, provider string) []FieldDefinition {
	if e == nil {
		return nil
	}
	items := make([]FieldDefinition, 0)
	for _, field := range e.document.Fields {
		if field.Preview == nil || !previewSupportsProvider(field.Preview, provider) {
			continue
		}
		if !fieldMatches(field.Match, filePath, false) {
			continue
		}
		items = append(items, field)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Preview, items[j].Preview
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func previewSupportsProvider(preview *PreviewSpec, provider string) bool {
	provider = strings.TrimSpace(provider)
	if preview == nil || provider == "" {
		return false
	}
	for _, candidate := range preview.providerNames() {
		if strings.EqualFold(candidate, provider) {
			return true
		}
	}
	return false
}

// ExtractPreviewFields projects a ScriptView through provider field targets.
// It deliberately shares the same target semantics as annotations, including
// repeated records, offsets and dynamic record widths.
func (e *Engine) ExtractPreviewFields(filePath string, view pvf.ScriptView, provider string) []PreviewFieldValue {
	fields := e.PreviewFields(filePath, provider)
	values := make([]PreviewFieldValue, 0)
	for _, field := range fields {
		for _, occurrence := range extractField(field, view) {
			values = append(values, occurrence)
		}
	}
	return values
}

func extractField(field FieldDefinition, view pvf.ScriptView) []PreviewFieldValue {
	target := field.Target
	if target.Kind == "section" {
		result := make([]PreviewFieldValue, 0)
		for _, sectionID := range sectionIDs(view, target.Section) {
			tokens := sectionTokens(view, target.Section, sectionID)
			if len(tokens) == 0 {
				continue
			}
			result = append(result, PreviewFieldValue{
				Field: field, Values: valuesOf(tokens),
				Start: tokens[0].Start, End: tokens[len(tokens)-1].End,
			})
		}
		return result
	}
	if target.Kind != "token" {
		return nil
	}

	result := make([]PreviewFieldValue, 0)
	for _, sectionID := range sectionIDs(view, target.Section) {
		tokens := sectionTokens(view, target.Section, sectionID)
		if target.Index != nil && target.RecordTokens > 0 {
			recordTokens := target.RecordTokens
			if target.TokensPerLineIndex != nil {
				recordTokens = repeatedRecordTokens(target, tokens)
			}
			if recordTokens <= 0 {
				continue
			}
			for start := target.Offset; start+recordTokens <= len(tokens); start += recordTokens {
				offset := *target.Index
				if offset < 0 || offset >= recordTokens {
					continue
				}
				record := tokens[start : start+recordTokens]
				context := ""
				if target.ContextIndex != nil && *target.ContextIndex >= 0 && *target.ContextIndex < len(record) {
					context = record[*target.ContextIndex].Value
				}
				result = append(result, PreviewFieldValue{
					Field: field, Values: valuesOf(record), Context: context,
					Start: record[offset].Start, End: record[offset].End,
				})
			}
			continue
		}

		if target.Index != nil {
			index := *target.Index
			if index >= 0 && index < len(tokens) {
				token := tokens[index]
				result = append(result, PreviewFieldValue{Field: field, Values: []string{token.Value}, Start: token.Start, End: token.End})
			}
			continue
		}
		if target.Range == nil {
			continue
		}
		start := maxInt(0, target.Range.Start)
		end := minInt(len(tokens), target.Range.EndExclusive)
		if start >= end {
			continue
		}
		result = append(result, PreviewFieldValue{
			Field: field, Values: valuesOf(tokens[start:end]),
			Start: tokens[start].Start, End: tokens[end-1].End,
		})
	}
	return result
}

func fieldMatches(match MatchSpec, filePath string, isDir bool) bool {
	filePath = normalizeFieldPath(filePath)
	if len(match.Extensions) > 0 {
		if isDir {
			return false
		}
		matched := false
		for _, extension := range match.Extensions {
			if strings.EqualFold(path.Ext(filePath), strings.TrimSpace(extension)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if match.Glob == "" {
		return true
	}
	return globFieldMatch(match.Glob, filePath)
}

func globFieldMatch(pattern, value string) bool {
	patternParts := strings.Split(normalizeFieldPath(pattern), "/")
	valueParts := strings.Split(normalizeFieldPath(value), "/")
	type state struct{ pattern, value int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(pi, vi int) bool {
		key := state{pi, vi}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		if pi == len(patternParts) {
			memo[key] = vi == len(valueParts)
			return memo[key]
		}
		if patternParts[pi] == "**" {
			memo[key] = match(pi+1, vi) || (vi < len(valueParts) && match(pi, vi+1))
			return memo[key]
		}
		if vi >= len(valueParts) {
			return false
		}
		matched, err := path.Match(patternParts[pi], valueParts[vi])
		if err == nil && matched {
			memo[key] = match(pi+1, vi+1)
		}
		return memo[key]
	}
	return match(0, 0)
}

func normalizeFieldPath(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/"))
}

func sectionIDs(view pvf.ScriptView, name string) []int {
	result := make([]int, 0)
	seen := make(map[int]struct{})
	for _, element := range view.Elements {
		if element.Kind != pvf.ScriptElementSection || !strings.EqualFold(element.Section, name) {
			continue
		}
		if _, ok := seen[element.SectionID]; ok {
			continue
		}
		seen[element.SectionID] = struct{}{}
		result = append(result, element.SectionID)
	}
	return result
}

func sectionTokens(view pvf.ScriptView, name string, sectionID int) []pvf.ScriptElement {
	result := make([]pvf.ScriptElement, 0)
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementToken && element.SectionID == sectionID && strings.EqualFold(element.Section, name) {
			result = append(result, element)
		}
	}
	return result
}

func valuesOf(elements []pvf.ScriptElement) []string {
	result := make([]string, len(elements))
	for i, element := range elements {
		result[i] = element.Value
	}
	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
