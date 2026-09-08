package annotations

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"pvfine/internal/pvf"
)

type Engine struct {
	document Document
	rules    []compiledRule
}

type compiledRule struct {
	rule       Rule
	extensions map[string]struct{}
}

type matchedItem struct {
	ruleID          string
	title           string
	content         string
	typ             string
	targetFileIndex int32
}

func Compile(document Document) (*Engine, error) {
	if document.Relations == nil {
		document.Relations = make(map[string]RelationSpec)
	}
	if document.Rules == nil {
		document.Rules = make([]Rule, 0)
	}
	if err := Validate(document); err != nil {
		return nil, err
	}
	rules := make([]compiledRule, 0, len(document.Rules))
	for _, rule := range document.Rules {
		extensions := make(map[string]struct{}, len(rule.Match.Extensions))
		for _, extension := range rule.Match.Extensions {
			extensions[strings.ToLower(extension)] = struct{}{}
		}
		rules = append(rules, compiledRule{rule: rule, extensions: extensions})
	}
	return &Engine{document: document, rules: rules}, nil
}

func (e *Engine) Document() Document { return e.document }

func (e *Engine) Annotate(filePath string, view pvf.ScriptView, resolver Resolver) []Result {
	results := make([]Result, 0)
	resultByAnchor := make(map[string]int)
	for _, compiled := range e.rules {
		rule := compiled.rule
		if rule.Target.Kind == "path" || !compiled.matches(filePath, false) {
			continue
		}
		anchors := editorAnchors(rule, view)
		for _, anchor := range anchors {
			item := annotationItem(rule, anchor.value, resolver)
			key := fmt.Sprintf("%d:%d", anchor.start, anchor.end)
			if index, ok := resultByAnchor[key]; ok {
				result := &results[index]
				result.Content = appendTooltip(result.Content, item.title, item.content)
				result.RuleIDs = append(result.RuleIDs, item.ruleID)
				continue
			}
			resultByAnchor[key] = len(results)
			results = append(results, Result{
				Start:           anchor.start,
				End:             anchor.end,
				Title:           item.title,
				Content:         appendTooltip("", item.title, item.content),
				Type:            item.typ,
				TargetFileIndex: item.targetFileIndex,
				RuleIDs:         []string{item.ruleID},
			})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Start != results[j].Start {
			return results[i].Start < results[j].Start
		}
		return results[i].End < results[j].End
	})
	return results
}

func (e *Engine) AnnotatePath(filePath string, isDir bool) []Result {
	items := make([]matchedItem, 0)
	for _, compiled := range e.rules {
		if compiled.rule.Target.Kind != "path" || !compiled.matches(filePath, isDir) {
			continue
		}
		items = append(items, annotationItem(compiled.rule, "", nil))
	}
	if len(items) == 0 {
		return nil
	}
	first := items[0]
	result := Result{
		Title:           first.title,
		Type:            first.typ,
		TargetFileIndex: -1,
		RuleIDs:         make([]string, 0, len(items)),
	}
	for _, item := range items {
		result.Content = appendTooltip(result.Content, item.title, item.content)
		result.RuleIDs = append(result.RuleIDs, item.ruleID)
	}
	return []Result{result}
}

func (e *Engine) Relation(name string) (RelationSpec, bool) {
	relation, ok := e.document.Relations[name]
	if ok && relation.RecordTokens == 0 {
		relation.RecordTokens = max(relation.IDToken, relation.PathToken) + 1
	}
	return relation, ok
}

func (e *Engine) RelationNames() []string {
	names := make([]string, 0, len(e.document.Relations))
	for name := range e.document.Relations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type editorAnchor struct {
	start int
	end   int
	value string
}

func editorAnchors(rule Rule, view pvf.ScriptView) []editorAnchor {
	if rule.Target.Kind == "section" {
		anchors := make([]editorAnchor, 0)
		for _, element := range view.Elements {
			if element.Kind == pvf.ScriptElementSection && strings.EqualFold(element.Section, rule.Target.Section) {
				anchors = append(anchors, editorAnchor{start: element.Start, end: element.End})
			}
		}
		return anchors
	}
	if rule.Target.Kind != "token" {
		return nil
	}
	if rule.Target.Index != nil {
		anchors := make([]editorAnchor, 0)
		for _, element := range view.Elements {
			if element.Kind == pvf.ScriptElementToken && strings.EqualFold(element.Section, rule.Target.Section) && element.Index == *rule.Target.Index {
				anchors = append(anchors, editorAnchor{start: element.Start, end: element.End, value: element.Value})
			}
		}
		return anchors
	}

	ranges := make(map[int][]pvf.ScriptElement)
	sectionOrder := make([]int, 0)
	for _, element := range view.Elements {
		if element.Kind != pvf.ScriptElementToken || !strings.EqualFold(element.Section, rule.Target.Section) {
			continue
		}
		if _, ok := ranges[element.SectionID]; !ok {
			sectionOrder = append(sectionOrder, element.SectionID)
		}
		ranges[element.SectionID] = append(ranges[element.SectionID], element)
	}
	anchors := make([]editorAnchor, 0, len(sectionOrder))
	for _, sectionID := range sectionOrder {
		var first, last *pvf.ScriptElement
		for i := range ranges[sectionID] {
			element := &ranges[sectionID][i]
			if element.Index < rule.Target.Range.Start || element.Index >= rule.Target.Range.EndExclusive {
				continue
			}
			if first == nil {
				first = element
			}
			last = element
		}
		if first != nil && last != nil {
			anchors = append(anchors, editorAnchor{start: first.Start, end: last.End})
		}
	}
	return anchors
}

func annotationItem(rule Rule, value string, resolver Resolver) matchedItem {
	item := matchedItem{
		ruleID:          rule.ID,
		title:           rule.Annotation.Title,
		content:         strings.TrimSpace(rule.Annotation.Content),
		typ:             rule.Annotation.Type,
		targetFileIndex: -1,
	}
	switch rule.Annotation.Type {
	case "enum":
		label, ok := rule.Annotation.Values[value]
		detail := "当前值: " + value
		if ok {
			item.title = label
			detail = value + " - " + label
		}
		item.content = joinContent(item.content, detail)
		keys := make([]string, 0, len(rule.Annotation.Values))
		for key := range rule.Annotation.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := []string{"所有枚举值:"}
		for _, key := range keys {
			entries = append(entries, key+" - "+rule.Annotation.Values[key])
		}
		item.content = joinContent(item.content, strings.Join(entries, "\n"))
	case "reference":
		detail := "ID: " + value + "（未找到关联文件）"
		if resolver != nil {
			if reference, ok := resolver(rule.Annotation.Relation, value); ok {
				if reference.Name != "" {
					item.title = reference.Name
				}
				detail = reference.Name
				if detail == "" {
					detail = reference.Path
				}
				if detail == "" {
					detail = "ID: " + value
				} else {
					detail += " (ID: " + value + ")"
				}
				item.targetFileIndex = reference.FileIndex
			}
		}
		item.content = joinContent(item.content, detail)
	}
	return item
}

func appendTooltip(current, title, content string) string {
	item := title
	if strings.TrimSpace(content) != "" {
		item += "\n" + strings.TrimSpace(content)
	}
	if current == "" {
		return item
	}
	return current + "\n\n" + item
}

func joinContent(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + "\n" + second
}

func (r compiledRule) matches(filePath string, isDir bool) bool {
	filePath = normalizePath(filePath)
	if len(r.extensions) > 0 {
		if isDir {
			return false
		}
		if _, ok := r.extensions[strings.ToLower(path.Ext(filePath))]; !ok {
			return false
		}
	}
	return r.rule.Match.Glob == "" || globMatch(r.rule.Match.Glob, filePath)
}

func normalizePath(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/"))
}

func globMatch(pattern, value string) bool {
	patternParts := splitPath(normalizePath(pattern))
	valueParts := splitPath(normalizePath(value))
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
			if match(pi+1, vi) || (vi < len(valueParts) && match(pi, vi+1)) {
				memo[key] = true
			}
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

func splitPath(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}
