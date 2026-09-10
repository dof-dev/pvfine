package annotations

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

type Engine struct {
	document Document
	rules    []compiledRule
}

// ContextResolver resolves an ID using an optional value from the same
// repeated record. It extends Resolver for relations whose list path depends
// on another token, such as skill IDs grouped by profession.
type ContextResolver func(relation, id, context string) (Reference, bool)

// ListResolver resolves one concrete list row. Unlike ContextResolver, it
// receives the row's relative path as well, so duplicate IDs in a list still
// navigate to the file represented by that particular row.
type ListResolver func(relation, id, context, listPath, relativePath string) (Reference, bool)

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
	image           *ImageReference
	inlineImage     bool
}

func Compile(document Document) (*Engine, error) {
	normalizeImageTargets(&document)
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
	return e.annotate(filePath, view, func(relation, id, _ string) (Reference, bool) {
		if resolver == nil {
			return Reference{}, false
		}
		return resolver(relation, id)
	}, nil)
}

func (e *Engine) AnnotateWithContextResolver(filePath string, view pvf.ScriptView, resolver ContextResolver) []Result {
	return e.annotate(filePath, view, resolver, nil)
}

// AnnotateWithContextAndListResolver is the context-aware annotation entry
// point used by the application. List annotations use listResolver when it is
// available, while ordinary rules continue to use resolver.
func (e *Engine) AnnotateWithContextAndListResolver(filePath string, view pvf.ScriptView, resolver ContextResolver, listResolver ListResolver) []Result {
	return e.annotate(filePath, view, resolver, listResolver)
}

func (e *Engine) annotate(filePath string, view pvf.ScriptView, resolver ContextResolver, listResolver ListResolver) []Result {
	results := make([]Result, 0)
	resultByAnchor := make(map[string]int)
	appendResult := func(anchor editorAnchor, item matchedItem) {
		key := fmt.Sprintf("%d:%d", anchor.start, anchor.end)
		if index, ok := resultByAnchor[key]; ok {
			result := &results[index]
			result.Content = appendTooltip(result.Content, item.title, item.content)
			result.RuleIDs = append(result.RuleIDs, item.ruleID)
			if result.TargetFileIndex < 0 && item.targetFileIndex >= 0 {
				result.TargetFileIndex = item.targetFileIndex
			}
			if result.Image == nil && item.image != nil {
				result.Image = item.image
			}
			if item.image != nil && item.inlineImage {
				result.InlineImage = true
			}
			return
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
			Image:           item.image,
			InlineImage:     item.image != nil && item.inlineImage,
		})
	}
	for _, compiled := range e.rules {
		rule := compiled.rule
		if rule.Target.Kind == "path" || !compiled.matches(filePath, false) {
			continue
		}
		anchors := e.editorAnchors(rule, view)
		for _, anchor := range anchors {
			value := anchor.value
			if rule.Annotation.Type == "image" {
				value = anchor.imagePathValue
			}
			item := annotationItem(rule, value, anchor.imageIndexValue, anchor.context, resolver)
			appendResult(anchor, item)
		}
	}
	for _, item := range e.listAnnotations(filePath, view, resolver, listResolver) {
		appendResult(item.anchor, item.item)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Start != results[j].Start {
			return results[i].Start < results[j].Start
		}
		return results[i].End < results[j].End
	})
	return results
}

type listAnnotation struct {
	anchor editorAnchor
	item   matchedItem
}

type listBinding struct {
	relationName string
	context      string
	listPath     string
	relation     RelationSpec
}

// listAnnotations adds the implicit name/link annotations for a list file.
// List files are formatted as fixed-size records, so the path token is the
// natural anchor for both the displayed name and Cmd/Ctrl-click navigation.
func (e *Engine) listAnnotations(filePath string, view pvf.ScriptView, resolver ContextResolver, listResolver ListResolver) []listAnnotation {
	if resolver == nil && listResolver == nil {
		return nil
	}

	bindings := make([]listBinding, 0)
	seenBindings := make(map[string]struct{})
	appendBinding := func(binding listBinding) {
		key := binding.relationName + "\x00" + normalizePath(binding.listPath)
		if _, exists := seenBindings[key]; exists {
			return
		}
		seenBindings[key] = struct{}{}
		bindings = append(bindings, binding)
	}
	for relationName, relation := range e.document.Relations {
		kind := relation.Kind
		if kind == "" {
			kind = "list"
		}
		switch kind {
		case "list":
			if normalizePath(relation.ListPath) == normalizePath(filePath) {
				appendBinding(listBinding{
					relationName: relationName,
					listPath:     relation.ListPath,
					relation:     e.relationWithDefaults(relation),
				})
			}
		case "contextual":
			for context, listPath := range relation.ContextPaths {
				if normalizePath(listPath) != normalizePath(filePath) {
					continue
				}
				appendBinding(listBinding{
					relationName: relationName,
					context:      context,
					listPath:     listPath,
					relation:     e.relationWithDefaults(relation),
				})
			}
		}
	}
	if len(bindings) == 0 {
		return nil
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].relationName != bindings[j].relationName {
			return bindings[i].relationName < bindings[j].relationName
		}
		if bindings[i].listPath != bindings[j].listPath {
			return bindings[i].listPath < bindings[j].listPath
		}
		return bindings[i].context < bindings[j].context
	})

	tokens := make([]pvf.ScriptElement, 0, len(view.Elements))
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementToken {
			tokens = append(tokens, element)
		}
	}

	results := make([]listAnnotation, 0)
	for _, binding := range bindings {
		relation := binding.relation
		for offset := 0; offset+relation.RecordTokens <= len(tokens); offset += relation.RecordTokens {
			id := tokens[offset+relation.IDToken].Value
			pathToken := tokens[offset+relation.PathToken]
			if id == "" || pathToken.Value == "" {
				continue
			}
			var reference Reference
			var ok bool
			if listResolver != nil {
				reference, ok = listResolver(binding.relationName, id, binding.context, binding.listPath, pathToken.Value)
			} else if resolver != nil {
				reference, ok = resolver(binding.relationName, id, binding.context)
			}
			if !ok || reference.FileIndex < 0 {
				continue
			}

			title := strings.TrimSpace(reference.Name)
			if title == "" {
				// A valid indexed file can still omit [name]. Keep the
				// annotation useful and, importantly, keep the path clickable.
				title = reference.Path
			}
			if title == "" {
				title = id
			}
			content := "ID: " + id
			if reference.Path != "" {
				content += "\n路径: " + reference.Path
			}
			results = append(results, listAnnotation{
				anchor: editorAnchor{start: pathToken.Start, end: pathToken.End},
				item: matchedItem{
					ruleID:          "list:" + binding.relationName,
					title:           title,
					content:         content,
					typ:             "reference",
					targetFileIndex: reference.FileIndex,
				},
			})
		}
	}
	return results
}

func (e *Engine) relationWithDefaults(relation RelationSpec) RelationSpec {
	if relation.RecordTokens == 0 {
		relation.RecordTokens = max(relation.IDToken, relation.PathToken) + 1
	}
	if relation.Kind == "" {
		relation.Kind = "list"
	}
	return relation
}

func (e *Engine) AnnotatePath(filePath string, isDir bool) []Result {
	items := make([]matchedItem, 0)
	for _, compiled := range e.rules {
		if compiled.rule.Target.Kind != "path" || !compiled.matches(filePath, isDir) {
			continue
		}
		items = append(items, annotationItem(compiled.rule, "", "", "", nil))
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
	if ok && relation.Kind == "" {
		relation.Kind = "list"
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
	start           int
	end             int
	value           string
	imagePathValue  string
	imageIndexValue string
	context         string
}

func (e *Engine) editorAnchors(rule Rule, view pvf.ScriptView) []editorAnchor {
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
	if rule.Target.Index != nil && rule.Target.RecordTokens > 0 {
		contextIndex := rule.Target.ContextIndex
		if contextIndex == nil && rule.Annotation.Type == "reference" {
			if relation, ok := e.Relation(rule.Annotation.Relation); ok && relation.Kind == "contextual" {
				value := relation.ContextToken
				contextIndex = &value
			}
		}
		return repeatedTokenAnchors(rule, view, contextIndex)
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

func repeatedTokenAnchors(rule Rule, view pvf.ScriptView, contextIndex *int) []editorAnchor {
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
	anchors := make([]editorAnchor, 0)
	for _, sectionID := range sectionOrder {
		tokens := ranges[sectionID]
		recordTokens := repeatedRecordTokens(rule.Target, tokens)
		if recordTokens <= 0 {
			continue
		}
		for start := rule.Target.Offset; start <= len(tokens); {
			if recordTokens > len(tokens)-start {
				break
			}
			targetOffset := *rule.Target.Index
			if targetOffset < 0 || targetOffset >= recordTokens || targetOffset >= len(tokens)-start {
				start += recordTokens
				continue
			}
			target := tokens[start+targetOffset]
			anchor := editorAnchor{start: target.Start, end: target.End, value: target.Value}
			if rule.Annotation.Type == "image" && rule.Target.ImagePathToken != nil {
				imagePathOffset := *rule.Target.ImagePathToken
				if imagePathOffset < 0 || imagePathOffset >= recordTokens || imagePathOffset >= len(tokens)-start {
					start += recordTokens
					continue
				}
				anchor.imagePathValue = tokens[start+imagePathOffset].Value
				anchor.imageIndexValue = target.Value
			}
			if contextIndex != nil {
				contextOffset := *contextIndex
				if contextOffset < 0 || contextOffset >= recordTokens || contextOffset >= len(tokens)-start {
					start += recordTokens
					continue
				}
				anchor.context = tokens[start+contextOffset].Value
			}
			anchors = append(anchors, anchor)
			start += recordTokens
		}
	}
	return anchors
}

// repeatedRecordTokens returns the fixed record width unless a section token
// supplies a valid positive override. The dynamic index is relative to the
// section's direct token sequence, just like rendering rules.
func repeatedRecordTokens(target TargetSpec, tokens []pvf.ScriptElement) int {
	recordTokens := target.RecordTokens
	if target.TokensPerLineIndex == nil {
		return recordTokens
	}
	index := *target.TokensPerLineIndex
	if index < 0 || index >= len(tokens) {
		return recordTokens
	}
	value, err := strconv.Atoi(strings.TrimSpace(tokens[index].Value))
	if err != nil || value <= 0 {
		return recordTokens
	}
	return value
}

func annotationItem(rule Rule, value, imageIndexValue, context string, resolver ContextResolver) matchedItem {
	item := matchedItem{
		ruleID:          rule.ID,
		title:           rule.Annotation.Title,
		content:         strings.TrimSpace(rule.Annotation.Content),
		typ:             rule.Annotation.Type,
		targetFileIndex: -1,
		inlineImage:     rule.Annotation.InlineImage,
	}
	switch rule.Annotation.Type {
	case "image":
		imageIndex, err := strconv.ParseInt(strings.TrimSpace(imageIndexValue), 10, 32)
		item.content = joinContent(item.content, fmt.Sprintf("图片: %s[%s]", value, strings.TrimSpace(imageIndexValue)))
		if strings.TrimSpace(value) != "" && err == nil && imageIndex >= 0 {
			item.image = &ImageReference{Path: strings.TrimSpace(value), Index: int32(imageIndex)}
		}
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
			if reference, ok := resolver(rule.Annotation.Relation, value, context); ok {
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
