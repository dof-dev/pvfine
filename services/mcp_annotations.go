package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	ann "pvfine/internal/annotations"
	"pvfine/internal/mcpserver"
	"pvfine/internal/ruleconfig"
)

type annotationArgs struct {
	Query            string   `json:"query"`
	FilePath         string   `json:"filePath"`
	PathKind         string   `json:"pathKind"`
	Extensions       []string `json:"extensions"`
	Section          string   `json:"section"`
	Kinds            []string `json:"kinds"`
	TargetKinds      []string `json:"targetKinds"`
	AnnotationTypes  []string `json:"annotationTypes"`
	Relation         string   `json:"relation"`
	PVFVersion       string   `json:"pvfVersion"`
	WorkspaceID      string   `json:"workspaceId"`
	Status           string   `json:"status"`
	Cursor           string   `json:"cursor"`
	Limit            int      `json:"limit"`
	DocumentID       string   `json:"documentId"`
	RulesRevision    string   `json:"rulesRevision"`
	Value            *string  `json:"value"`
	ValuesCursor     string   `json:"valuesCursor"`
	ValuesLimit      int      `json:"valuesLimit"`
	ExpectedRevision uint64   `json:"expectedRevision"`
	Occurrence       *int     `json:"occurrence"`
	TokenIndex       *int     `json:"tokenIndex"`
	StartLine        *int     `json:"startLine"`
	EndLine          *int     `json:"endLine"`
}

func (s *MCPService) annotationTools() []mcpserver.Tool {
	props := map[string]any{}
	for _, k := range strings.Fields("query filePath pathKind section relation pvfVersion workspaceId status cursor documentId rulesRevision value valuesCursor") {
		props[k] = map[string]any{"type": "string", "maxLength": 4096}
	}
	for _, k := range strings.Fields("extensions kinds targetKinds annotationTypes") {
		props[k] = map[string]any{"type": "array", "maxItems": 100, "items": map[string]any{"type": "string", "maxLength": 256}}
	}
	for _, k := range strings.Fields("expectedRevision occurrence tokenIndex startLine endLine") {
		min := 0
		if k == "startLine" || k == "endLine" {
			min = 1
		}
		props[k] = map[string]any{"type": "integer", "minimum": min}
	}
	for _, k := range []string{"limit", "valuesLimit"} {
		props[k] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	}
	for k, values := range map[string][]string{"pathKind": {"file", "directory"}, "status": {"effective", "all"}} {
		props[k] = map[string]any{"type": "string", "enum": values}
	}
	result := []mcpserver.Tool{}
	for _, d := range []struct{ name, description, fields, required string }{
		{"search", "搜索已激活的 PVF 标注规则与共享字段说明；section 留空检索全部类别。版本默认 current（需 workspaceId），all 可离线浏览。返回文档身份与分页摘要，不扫描归档。", "query filePath pathKind extensions section kinds targetKinds annotationTypes relation pvfVersion workspaceId status cursor limit", ""},
		{"get", "读取标注文档完整布局、整块继承后的定义、分页枚举和关联定义。需搜索返回的 documentId/rulesRevision；value 按字符串精确查找。", "documentId rulesRevision value valuesCursor valuesLimit", "documentId rulesRevision"},
		{"explain", "解释当前工作区已提交脚本的实际标注。位置为 UTF-16；occurrence/tokenIndex 从 0 开始。与 read_file 同源，不包含前端草稿，不冻结界面。", "workspaceId expectedRevision filePath rulesRevision section occurrence tokenIndex startLine endLine cursor limit", "workspaceId expectedRevision filePath"},
	} {
		d := d
		properties := map[string]any{}
		for _, k := range strings.Fields(d.fields) {
			properties[k] = props[k]
		}
		result = append(result, mcpserver.Tool{Name: "pvf_annotation_" + d.name, Description: d.description, ReadOnly: true, Schema: map[string]any{"type": "object", "properties": properties, "required": strings.Fields(d.required), "additionalProperties": false}, Call: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a annotationArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, fmt.Errorf("INVALID_ARGUMENT")
			}
			s.c.mu.Lock()
			defer s.c.mu.Unlock()
			switch d.name {
			case "search":
				return s.annotationSearch(a)
			case "get":
				return s.annotationGet(a)
			default:
				return s.annotationExplain(a)
			}
		}})
	}
	return result
}

type annotationDocument struct {
	Entry           ruleconfig.Entry
	Rule            ann.Rule
	Original        any
	Preview         *ann.PreviewSpec
	DocumentID      string
	ResolutionError string
}

type annotationCatalog struct {
	Revision string
	Document ann.Document
	Items    []annotationDocument
}

func annotationHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (c *core) annotationCatalogLocked() (annotationCatalog, error) {
	if c.annotationErr != nil {
		return annotationCatalog{}, c.annotationErr
	}
	if c.annotationEngine == nil {
		return annotationCatalog{}, fmt.Errorf("RULES_UNAVAILABLE")
	}
	if c.annotationDocumentation != nil && c.annotationDocumentationEngine == c.annotationEngine && c.annotationDocumentationSnapshot == c.annotationSnapshot {
		return *c.annotationDocumentation, nil
	}
	doc := c.annotationEngine.Document()
	entries := []ruleconfig.Entry{}
	revision := annotationHash(doc)
	if c.annotationSnapshot != nil {
		revision = c.annotationSnapshot.Revision
		entries = c.annotationSnapshot.Entries
	}
	if len(entries) == 0 {
		for _, r := range doc.Rules {
			b, _ := json.Marshal(r)
			entries = append(entries, ruleconfig.Entry{ID: r.ID, Kind: "rule", JSON: string(b), Source: "unknown", Effective: true})
		}
		for _, f := range doc.Fields {
			b, _ := json.Marshal(f)
			entries = append(entries, ruleconfig.Entry{ID: f.ID, Kind: "field", JSON: string(b), Source: "unknown", Effective: true})
		}
	}
	out := annotationCatalog{Revision: revision, Document: doc, Items: []annotationDocument{}}
	for i, e := range entries {
		d := annotationDocument{Entry: e, DocumentID: annotationHash([]any{revision, e.Kind, e.ID, e.Source, i})}
		if e.Kind == "field" {
			var f ann.FieldDefinition
			if json.Unmarshal([]byte(e.JSON), &f) != nil {
				continue
			}
			d.Original = f
			d.Preview = f.Preview
			d.Rule = ann.Rule{ID: f.ID, PVFVersions: f.PVFVersions, Match: f.Match, Target: f.Target, Annotation: f.Annotation}
		} else if e.Kind == "rule" {
			var r ann.Rule
			if json.Unmarshal([]byte(e.JSON), &r) != nil {
				continue
			}
			d.Original = r
			d.Rule = r
			if resolved, err := ann.ResolveDocumentationRule(r, doc.Fields); err == nil {
				d.Rule = resolved
			} else {
				d.ResolutionError = err.Error()
			}
			for _, f := range doc.Fields {
				if f.ID == r.Field {
					d.Preview = f.Preview
					break
				}
			}
		} else {
			continue
		}
		out.Items = append(out.Items, d)
	}
	c.annotationDocumentation = &out
	c.annotationDocumentationEngine = c.annotationEngine
	c.annotationDocumentationSnapshot = c.annotationSnapshot
	return out, nil
}

// Read-only cursors bind all filters and the activated snapshots; no filesystem state is read.
type annotationCursor struct {
	Revision  string `json:"r"`
	Binding   string `json:"b"`
	Offset    int    `json:"o"`
	Workspace string `json:"w,omitempty"`
	Text      string `json:"t,omitempty"`
}

func annotationPage(cursor, revision, binding, workspace, text string, n, limit int) (int, int, error) {
	start := 0
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		var c annotationCursor
		if err != nil || json.Unmarshal(b, &c) != nil {
			return 0, 0, fmt.Errorf("INVALID_CURSOR")
		}
		if c.Revision != revision {
			return 0, 0, fmt.Errorf("STALE_RULES")
		}
		if c.Workspace != workspace {
			return 0, 0, fmt.Errorf("STALE_WORKSPACE")
		}
		if c.Text != text {
			return 0, 0, fmt.Errorf("STALE_TEXT")
		}
		if c.Binding != binding || c.Offset < 0 || c.Offset > n {
			return 0, 0, fmt.Errorf("INVALID_CURSOR")
		}
		start = c.Offset
	}
	return start, min(start+limit, n), nil
}
func annotationNext(end, n int, revision, binding, workspace, text string) any {
	if end >= n {
		return nil
	}
	b, _ := json.Marshal(annotationCursor{revision, binding, end, workspace, text})
	return base64.RawURLEncoding.EncodeToString(b)
}
func annotationSection(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), "["), "]")))
}
func annotationIn(xs []string, s string) bool {
	if len(xs) == 0 {
		return true
	}
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
func annotationSource(source string) string {
	switch source {
	case "annotations.user.json", "user":
		return "user"
	case "annotations.json", "builtin":
		return "builtin"
	case "paths.json", "sections.json", "generated":
		return "generated"
	default:
		return "unknown"
	}
}
func annotationTerms(values []string) []string {
	unique := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if !unique[v] {
			unique[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func annotationShort(s string) (string, bool) {
	r := []rune(s)
	if len(r) > 512 {
		return string(r[:512]), true
	}
	return s, false
}
func annotationVersion(d annotationDocument, doc ann.Document, v string) bool {
	if v == "all" {
		return true
	}
	if !annotationIn(d.Rule.PVFVersions, v) {
		return false
	}
	if d.Rule.Field != "" {
		for _, f := range doc.Fields {
			if f.ID == d.Rule.Field {
				return annotationIn(f.PVFVersions, v)
			}
		}
		return false
	}
	return true
}
func (s *MCPService) annotationSearch(a annotationArgs) (any, error) {
	cat, err := s.c.annotationCatalogLocked()
	if err != nil {
		return nil, err
	}
	if a.PVFVersion == "" {
		a.PVFVersion = "current"
	}
	if a.Status == "" {
		a.Status = "effective"
	}
	if a.PathKind == "" {
		a.PathKind = "file"
	}
	if a.Limit == 0 {
		a.Limit = 20
	}
	if a.PVFVersion == "current" {
		if s.c.archive == nil {
			return nil, fmt.Errorf("NO_ARCHIVE: 请指定 pvfVersion 或 all")
		}
		if a.WorkspaceID == "" || a.WorkspaceID != s.c.mcpWorkspaceID {
			return nil, fmt.Errorf("STALE_WORKSPACE")
		}
		a.PVFVersion = s.c.archive.ClientVersion()
	} else if a.PVFVersion != "all" && !ann.ValidPVFVersion(a.PVFVersion) {
		return nil, fmt.Errorf("INVALID_VERSION: 支持 90US,90CN,110US,115US,115CN,100CN,TW")
	}
	cursor := a.Cursor
	a.Cursor = ""
	a.Limit = max(1, min(a.Limit, 100))
	a.Section = annotationSection(a.Section)
	for _, p := range []*[]string{&a.Extensions, &a.Kinds, &a.TargetKinds, &a.AnnotationTypes} {
		*p = annotationTerms(*p)
	}
	a.Query = strings.Join(annotationTerms(strings.Fields(a.Query)), " ")
	a.FilePath = strings.ToLower(strings.Trim(strings.ReplaceAll(a.FilePath, "\\", "/"), "/"))
	binding := annotationHash(a)
	type hit struct {
		item  map[string]any
		score int
		key   string
	}
	hits := []hit{}
	terms := strings.Fields(strings.ToLower(a.Query))
	for _, d := range cat.Items {
		r := d.Rule
		effective := d.Entry.Effective && annotationVersion(d, cat.Document, a.PVFVersion)
		if a.Status != "all" && !effective {
			continue
		}
		if !annotationIn(a.Kinds, d.Entry.Kind) || !annotationIn(a.TargetKinds, r.Target.Kind) || !annotationIn(a.AnnotationTypes, r.Annotation.Type) {
			continue
		}
		if a.Section != "" && a.Section != annotationSection(r.Target.Section) {
			continue
		}
		if a.Relation != "" && r.Annotation.Relation != a.Relation {
			continue
		}
		applicability := "unknown"
		reasons := []string{}
		if a.FilePath != "" {
			if !ann.DocumentationMatches(r.Match, a.FilePath, a.PathKind == "directory") {
				continue
			}
			applicability = "matched"
			reasons = append(reasons, "filePath")
		}
		if a.PathKind == "directory" && r.Target.Kind != "path" {
			continue
		}
		if len(a.Extensions) > 0 && len(r.Match.Extensions) > 0 {
			matched := false
			for _, ext := range a.Extensions {
				if annotationIn(r.Match.Extensions, ext) {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		if a.PVFVersion == "all" || !effective {
			applicability = "unknown"
		}
		raw, _ := json.Marshal([]any{r.ID, r.Description, r.Field, r.Target.Section, r.Annotation.Relation, r.Annotation})
		hay := strings.ToLower(string(raw))
		matched := true
		score := 0
		for _, term := range terms {
			if !strings.Contains(hay, term) {
				matched = false
				break
			}
			score++
			for _, v := range []string{r.ID, r.Target.Section, r.Annotation.Title} {
				if strings.EqualFold(v, term) {
					score += 10
				}
			}
		}
		if !matched {
			continue
		}
		summary, truncated := annotationShort(strings.TrimSpace(r.Description + "\n" + r.Annotation.Content))
		why := []string{}
		if d.Entry.Disabled {
			why = append(why, "disabled")
		}
		if d.Entry.Overridden {
			why = append(why, "overridden")
		}
		if !annotationVersion(d, cat.Document, a.PVFVersion) {
			why = append(why, "version_mismatch")
		}
		if len(terms) > 0 {
			reasons = append(reasons, "query")
		}
		if a.Section != "" {
			reasons = append(reasons, "section")
		}
		values := []map[string]string{}
		keys := []string{}
		for k := range r.Annotation.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(terms) > 0 {
			for _, k := range keys {
				label := r.Annotation.Values[k]
				for _, term := range terms {
					if strings.Contains(strings.ToLower(k+" "+label), term) {
						short, _ := annotationShort(label)
						values = append(values, map[string]string{"value": k, "label": short})
						break
					}
				}
				if len(values) >= 5 {
					break
				}
			}
		}
		item := map[string]any{"documentId": d.DocumentID, "kind": d.Entry.Kind, "ruleId": r.ID, "fieldId": r.Field, "title": r.Annotation.Title, "summary": summary, "summaryTruncated": truncated, "section": r.Target.Section, "targetKind": r.Target.Kind, "annotationType": r.Annotation.Type, "pvfVersions": r.PVFVersions, "effective": effective, "applicability": applicability, "matchReasons": reasons, "inactiveReasons": why, "source": annotationSource(d.Entry.Source), "valueCount": len(r.Annotation.Values), "matchedValues": values, "relation": r.Annotation.Relation}
		hits = append(hits, hit{item, score, d.Entry.Kind + "\x00" + r.ID + "\x00" + d.Entry.Source + d.DocumentID})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].key < hits[j].key
	})
	start, end, err := annotationPage(cursor, cat.Revision, binding, a.WorkspaceID, "", len(hits), a.Limit)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, h := range hits[start:end] {
		items = append(items, h.item)
	}
	return map[string]any{"schemaVersion": 1, "rulesRevision": cat.Revision, "context": map[string]any{"workspaceId": a.WorkspaceID, "pvfVersion": a.PVFVersion, "filePath": a.FilePath}, "items": items, "totalMatches": len(hits), "nextCursor": annotationNext(end, len(hits), cat.Revision, binding, a.WorkspaceID, "")}, nil
}

func (s *MCPService) annotationGet(a annotationArgs) (any, error) {
	cat, err := s.c.annotationCatalogLocked()
	if err != nil {
		return nil, err
	}
	if a.RulesRevision != cat.Revision {
		return nil, fmt.Errorf("STALE_RULES")
	}
	var selected *annotationDocument
	for i := range cat.Items {
		if cat.Items[i].DocumentID == a.DocumentID {
			selected = &cat.Items[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("DOCUMENT_NOT_FOUND")
	}
	d := *selected
	r := d.Rule
	if a.ValuesLimit == 0 {
		a.ValuesLimit = 50
	}
	a.ValuesLimit = max(1, min(a.ValuesLimit, 100))
	cursor := a.ValuesCursor
	a.ValuesCursor = ""
	binding := annotationHash(a)
	keys := []string{}
	for k := range r.Annotation.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	start, end, err := annotationPage(cursor, cat.Revision, binding, "", "", len(keys), a.ValuesLimit)
	if err != nil {
		return nil, err
	}
	values := []map[string]string{}
	for _, k := range keys[start:end] {
		values = append(values, map[string]string{"value": k, "label": r.Annotation.Values[k]})
	}
	// Enum maps are paged separately, never duplicated inside raw definitions.
	raw, _ := json.Marshal(d.Original)
	var original map[string]any
	_ = json.Unmarshal(raw, &original)
	if annotation, ok := original["annotation"].(map[string]any); ok {
		delete(annotation, "values")
	}
	resolved := r
	resolved.Annotation.Values = nil
	relations := map[string]ann.RelationSpec{}
	warnings := []string{}
	if d.ResolutionError != "" {
		warnings = append(warnings, d.ResolutionError)
	}
	visiting := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if name == "" {
			return
		}
		if visiting[name] {
			warnings = append(warnings, "relation_cycle:"+name)
			return
		}
		if _, ok := relations[name]; ok {
			return
		}
		spec, ok := cat.Document.Relations[name]
		if !ok {
			warnings = append(warnings, "missing_relation:"+name)
			return
		}
		visiting[name] = true
		relations[name] = spec
		for _, child := range spec.Relations {
			visit(child)
		}
		visiting[name] = false
	}
	visit(r.Annotation.Relation)
	result := map[string]any{"schemaVersion": 1, "rulesRevision": cat.Revision, "documentId": d.DocumentID, "kind": d.Entry.Kind, "ruleId": r.ID, "source": annotationSource(d.Entry.Source), "effective": d.Entry.Effective, "disabled": d.Entry.Disabled, "overridden": d.Entry.Overridden, "originalDefinition": original, "resolvedDefinition": resolved, "preview": d.Preview, "valueCount": len(keys), "values": values, "definitionsOmitValues": true, "nextValuesCursor": annotationNext(end, len(keys), cat.Revision, binding, "", ""), "relations": relations, "warnings": warnings}
	if a.Value != nil {
		label, found := r.Annotation.Values[*a.Value]
		result["value"] = map[string]any{"value": *a.Value, "found": found, "label": label}
	}
	return result, nil
}
