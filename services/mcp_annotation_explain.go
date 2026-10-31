package services

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

func annotationPosition(lineStarts []int, offset int) map[string]int {
	offset = max(0, offset)
	line := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > offset }) - 1
	return map[string]int{"line": line + 1, "column": offset - lineStarts[line] + 1}
}
func (s *MCPService) annotationExplain(a annotationArgs) (any, error) {
	if err := s.checkLocked(mcpArgs{WorkspaceID: a.WorkspaceID, ExpectedRevision: a.ExpectedRevision}); err != nil {
		return nil, err
	}
	if a.Occurrence != nil && a.Section == "" || a.TokenIndex != nil && (a.Section == "" || a.Occurrence == nil) || a.StartLine != nil && a.EndLine != nil && *a.StartLine > *a.EndLine {
		return nil, fmt.Errorf("INVALID_ARGUMENT: occurrence/tokenIndex/行范围")
	}
	cat, err := s.c.annotationCatalogLocked()
	if err != nil {
		return nil, err
	}
	if a.RulesRevision != "" && a.RulesRevision != cat.Revision {
		return nil, fmt.Errorf("STALE_RULES")
	}
	c := s.c
	index, ok := c.archive.Find(a.FilePath)
	if !ok {
		return nil, fmt.Errorf("FILE_NOT_FOUND")
	}
	if c.archive.File(index).DataSize > 8<<20 {
		return nil, fmt.Errorf("FILE_TOO_LARGE")
	}
	text, err := c.archive.Text(index)
	if err != nil {
		return nil, err
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	textRevision := annotationHash(text)
	rendered, err := c.editorAnnotationsLocked(index, text)
	if err != nil {
		return nil, err
	}
	view := pvf.ParseScriptViewWithNestedSections(text, func(parent, child string) bool {
		if c.renderingEngine == nil {
			return false
		}
		for _, nested := range c.renderingEngine.SectionFormat(a.FilePath, parent).NestedSections {
			if strings.EqualFold(nested, child) {
				return true
			}
		}
		return false
	})
	anchors := c.annotationEngine.DocumentationAnchors(a.FilePath, view)
	anchorRanges := map[[2]int][]int{}
	for i, a := range anchors {
		key := [2]int{a.Start, a.End}
		anchorRanges[key] = append(anchorRanges[key], i)
	}
	docIDs := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			for _, d := range cat.Items {
				want := d.Rule.ID
				if d.Entry.Kind == "field" {
					want = "field:" + want
				}
				if want == id && d.Entry.Effective && annotationVersion(d, cat.Document, c.archive.ClientVersion()) {
					out = append(out, d.DocumentID)
				}
			}
		}
		return out
	}
	pathContext := []map[string]any{}
	scope := a.FilePath
	directory := false
	for scope != "." && scope != "" {
		for _, r := range c.annotationEngine.AnnotatePath(scope, directory) {
			pathContext = append(pathContext, map[string]any{"scopePath": scope, "pathKind": map[bool]string{true: "directory", false: "file"}[directory], "title": r.Title, "content": r.Content, "type": r.Type, "ruleIds": r.RuleIDs, "documentIds": docIDs(r.RuleIDs)})
		}
		scope = path.Dir(scope)
		directory = true
	}
	occurrences := map[int]int{}
	counts := map[string]int{}
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementSection {
			key := annotationSection(e.Section)
			occurrences[e.SectionID] = counts[key]
			counts[key]++
		}
	}
	units := utf16.Encode([]rune(text))
	lineStarts := []int{0}
	for i, u := range units {
		if u == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	items := []map[string]any{}
	for _, r := range rendered {
		start, end := int(r.Start), int(r.End)
		var element *pvf.ScriptElement
		values := []string{}
		elementStart := sort.Search(len(view.Elements), func(i int) bool { return view.Elements[i].End > start })
		for i := elementStart; i < len(view.Elements) && view.Elements[i].Start < end; i++ {
			e := &view.Elements[i]
			if e.Start < end && e.End > start {
				if element == nil {
					element = e
				}
				if e.Kind == pvf.ScriptElementToken {
					values = append(values, e.Value)
				}
			}
		}
		if a.Section != "" && (element == nil || annotationSection(element.Section) != annotationSection(a.Section)) {
			continue
		}
		if a.Occurrence != nil && (element == nil || occurrences[element.SectionID] != *a.Occurrence) {
			continue
		}
		if a.TokenIndex != nil {
			selected := false
			for _, e := range view.Elements {
				if e.Kind == pvf.ScriptElementToken && element != nil && e.SectionID == element.SectionID && e.Index == *a.TokenIndex && e.Start < end && e.End > start {
					selected = true
				}
			}
			if !selected {
				continue
			}
		}
		begin, finish := annotationPosition(lineStarts, start), annotationPosition(lineStarts, end)
		last := annotationPosition(lineStarts, max(start, end-1))
		if a.StartLine != nil && last["line"] < *a.StartLine || a.EndLine != nil && begin["line"] > *a.EndLine {
			continue
		}
		content, truncated := annotationShort(r.Content)
		origin := "rule"
		if len(r.RuleIDs) == 0 {
			origin = "runtime"
		}
		item := map[string]any{"range": map[string]any{"start": start, "end": end, "startPosition": begin, "endPosition": finish}, "title": r.Title, "content": content, "contentTruncated": truncated, "type": r.Type, "ruleIds": r.RuleIDs, "documentIds": docIDs(r.RuleIDs), "origin": origin, "values": values, "inlineImage": r.InlineImage}
		if element != nil {
			item["section"] = element.Section
			item["sectionPath"] = element.SectionPath
			item["occurrence"] = occurrences[element.SectionID]
			if element.Kind == pvf.ScriptElementToken {
				item["tokenIndex"] = element.Index
			}
		}
		details := []map[string]any{}
		for _, anchorIndex := range anchorRanges[[2]int{start, end}] {
			anchor := anchors[anchorIndex]
			spec := anchor.Rule.Annotation
			detail := map[string]any{"ruleId": anchor.Rule.ID, "value": anchor.Value}
			if label, found := spec.Values[anchor.Value]; found {
				detail["valueLabel"] = label
			}
			if spec.Relation != "" {
				ctx := anchor.Context
				relation, exists := c.annotationEngine.Relation(spec.Relation)
				if exists && relation.Kind == "contextual" && strings.EqualFold(path.Ext(a.FilePath), ".skl") && (ctx == "" || ctx == anchor.Value) {
					if source := c.annotationContextForSourceLocked(index, spec.Relation, relation); source != "" {
						ctx = source
					}
				}
				status := "missing"
				reference := map[string]any{"relation": spec.Relation, "id": anchor.Value, "context": ctx}
				if !exists {
					status = "unsupported"
				} else if relation.Kind == "contextual" && ctx == "" {
					status = "context_required"
				} else if resolved, found := c.resolveAnnotationReferenceContextLocked(spec.Relation, anchor.Value, ctx); found {
					status = "resolved"
					reference["name"] = resolved.Name
					reference["path"] = resolved.Path
				}
				reference["resolutionStatus"] = status
				detail["reference"] = reference
			}
			details = append(details, detail)
		}
		item["details"] = details
		if r.TargetFileIndex >= 0 {
			item["targetPath"] = c.archive.Path(r.TargetFileIndex)
		}
		if r.Image != nil {
			item["image"] = r.Image
		}
		if r.Placeholder != nil {
			item["placeholder"] = map[string]any{"key": r.Placeholder.Key, "fallback": r.Placeholder.Fallback, "missing": r.Placeholder.Missing}
		}
		items = append(items, item)
	}
	if a.Limit == 0 {
		a.Limit = 20
	}
	a.Limit = max(1, min(a.Limit, 100))
	cursor := a.Cursor
	a.Cursor = ""
	a.RulesRevision = cat.Revision
	a.Section = annotationSection(a.Section)
	binding := annotationHash(a)
	workspace := fmt.Sprintf("%s:%d", a.WorkspaceID, a.ExpectedRevision)
	start, end, err := annotationPage(cursor, cat.Revision, binding, workspace, textRevision, len(items), a.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schemaVersion": 1, "rulesRevision": cat.Revision, "workspaceId": a.WorkspaceID, "revision": a.ExpectedRevision, "textRevision": textRevision, "filePath": a.FilePath, "contentSource": "workspace", "coordinateUnit": "UTF-16", "pathContext": pathContext, "annotations": items[start:end], "nextCursor": annotationNext(end, len(items), cat.Revision, binding, workspace, textRevision), "truncated": end < len(items), "warnings": []string{}}, nil
}
