package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ann "pvfine/internal/annotations"
	"pvfine/internal/ruleconfig"
)

func annotationFixture(t *testing.T) *MCPService {
	t.Helper()
	s := newMCPFixture(t)
	zero := 0
	doc := ann.Document{Version: 1, Fields: []ann.FieldDefinition{{ID: "shared", Match: ann.MatchSpec{Extensions: []string{".equ"}}, Target: ann.TargetSpec{Kind: "token", Section: "price", Index: &zero}, Annotation: ann.AnnotationSpec{Type: "enum", Title: "价格", Content: "共享价格", Values: map[string]string{"001": "保留零", "1": "普通", "2": "另一个"}}}}, Rules: []ann.Rule{
		{ID: "price", Field: "shared"},
		{ID: "section", Target: ann.TargetSpec{Kind: "section", Section: "price"}, Annotation: ann.AnnotationSpec{Title: "价格段", Type: "text", Content: strings.Repeat("中文😀", 200)}},
		{ID: "folder", Match: ann.MatchSpec{Glob: "equipment"}, Target: ann.TargetSpec{Kind: "path"}, Annotation: ann.AnnotationSpec{Type: "text", Title: "装备目录"}},
		{ID: "version", PVFVersions: []string{"110US"}, Target: ann.TargetSpec{Kind: "section", Section: "name"}, Annotation: ann.AnnotationSpec{Type: "text", Title: "版本说明"}},
		{ID: "override", Field: "shared", Annotation: ann.AnnotationSpec{Title: "整块替换", Type: "text"}},
	}}
	engine, err := ann.Compile(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.c.annotationEngine = engine
	s.c.annotationSnapshot = nil
	s.c.annotationErr = nil
	s.c.editorAnnotation = editorAnnotationCache{}
	return s
}
func searchDocs(t *testing.T, s *MCPService, a annotationArgs) map[string]any {
	t.Helper()
	r, err := s.annotationSearch(a)
	if err != nil {
		t.Fatal(err)
	}
	return r.(map[string]any)
}

func TestMCPAnnotation115CNVersionFilter(t *testing.T) {
	s := annotationFixture(t)
	doc := s.c.annotationEngine.Document()
	doc.Rules = append(doc.Rules, ann.Rule{
		ID: "cn115-only", PVFVersions: []string{"115CN"},
		Target:     ann.TargetSpec{Kind: "section", Section: "price"},
		Annotation: ann.AnnotationSpec{Type: "text", Title: "115CN"},
	})
	engine, err := ann.Compile(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.c.annotationEngine = engine
	page := searchDocs(t, s, annotationArgs{PVFVersion: "115CN", Query: "cn115-only"})
	if page["totalMatches"].(int) != 1 {
		t.Fatal("115CN version-specific annotation not found")
	}
	page = searchDocs(t, s, annotationArgs{PVFVersion: "110US", Query: "cn115-only"})
	if page["totalMatches"].(int) != 0 {
		t.Fatal("115CN annotation leaked into another version")
	}
	if _, err := s.annotationSearch(annotationArgs{PVFVersion: "unknown"}); err == nil || !strings.Contains(err.Error(), "115CN") {
		t.Fatal("invalid version diagnostic omitted 115CN")
	}
}

func TestMCPAnnotationSearchGetAndCursors(t *testing.T) {
	s := annotationFixture(t)
	a := annotationArgs{PVFVersion: "all", Limit: 2}
	page := searchDocs(t, s, a)
	if page["totalMatches"].(int) != 6 {
		t.Fatal("empty section did not include all kinds")
	}
	a.Cursor = page["nextCursor"].(string)
	page2 := searchDocs(t, s, a)
	if len(page2["items"].([]map[string]any)) != 2 {
		t.Fatal("pagination")
	}
	a.Query = "价格"
	if _, err := s.annotationSearch(a); err == nil || !strings.Contains(err.Error(), "INVALID_CURSOR") {
		t.Fatal("changed filter accepted")
	}
	hit := searchDocs(t, s, annotationArgs{PVFVersion: "all", FilePath: "equipment/not-created.equ", Section: "[price]", Query: "保留零", Kinds: []string{"rule"}})
	items := hit["items"].([]map[string]any)
	if len(items) != 1 || items[0]["ruleId"] != "price" {
		t.Fatal("inherited enum search")
	}
	get := annotationArgs{DocumentID: items[0]["documentId"].(string), RulesRevision: hit["rulesRevision"].(string), ValuesLimit: 1}
	key := "001"
	get.Value = &key
	value, err := s.annotationGet(get)
	if err != nil {
		t.Fatal(err)
	}
	detail := value.(map[string]any)
	if detail["value"].(map[string]any)["label"] != "保留零" || len(detail["values"].([]map[string]string)) != 1 {
		t.Fatal("exact string key / enum paging")
	}
	raw, _ := json.Marshal(detail["originalDefinition"])
	if strings.Contains(string(raw), "保留零") {
		t.Fatal("raw definition leaked unpaged values")
	}
	get.ValuesCursor = detail["nextValuesCursor"].(string)
	if _, err = s.annotationGet(get); err != nil {
		t.Fatal(err)
	}
	get.RulesRevision = "old"
	if _, err = s.annotationGet(get); err == nil || !strings.Contains(err.Error(), "STALE_RULES") {
		t.Fatal("stale rules accepted")
	}
	overrides := searchDocs(t, s, annotationArgs{PVFVersion: "all", Query: "override"})["items"].([]map[string]any)
	if len(overrides) != 1 || overrides[0]["valueCount"].(int) != 0 {
		t.Fatal("annotation deep merged")
	}
	dirs := searchDocs(t, s, annotationArgs{PVFVersion: "all", FilePath: "equipment", PathKind: "directory"})["items"].([]map[string]any)
	if len(dirs) != 1 || dirs[0]["ruleId"] != "folder" {
		t.Fatal("directory semantics")
	}
	version := searchDocs(t, s, annotationArgs{PVFVersion: "90CN", Query: "version"})
	if version["totalMatches"].(int) != 0 {
		t.Fatal("version filter")
	}
	version = searchDocs(t, s, annotationArgs{PVFVersion: "90CN", Query: "version", Status: "all"})
	if version["totalMatches"].(int) != 1 || version["items"].([]map[string]any)[0]["effective"] != false {
		t.Fatal("inactive version visibility")
	}
}
func TestMCPAnnotationSnapshotAndDisabledVariants(t *testing.T) {
	s := annotationFixture(t)
	doc := s.c.annotationEngine.Document()
	raw, _ := json.Marshal(doc.Rules[1])
	s.c.annotationSnapshot = &ruleconfig.Snapshot{Revision: "active", Entries: []ruleconfig.Entry{
		{ID: "section", Kind: "rule", JSON: string(raw), Source: "builtin", Overridden: true},
		{ID: "section", Kind: "rule", JSON: string(raw), Source: "user", Effective: true},
		{ID: "disabled", Kind: "rule", JSON: strings.Replace(string(raw), `"section"`, `"disabled"`, 1), Source: "user", Disabled: true},
	}}
	page := searchDocs(t, s, annotationArgs{PVFVersion: "all", Status: "all"})
	items := page["items"].([]map[string]any)
	if len(items) != 3 || items[0]["documentId"] == items[1]["documentId"] {
		t.Fatal("source variants collapsed")
	}
	if page["rulesRevision"] != "active" {
		t.Fatal("not active snapshot")
	}
	page = searchDocs(t, s, annotationArgs{PVFVersion: "all"})
	if page["totalMatches"].(int) != 1 {
		t.Fatal("disabled or overridden effective")
	}
}
func TestMCPAnnotationExplainUsesRuntimeCoordinatesAndRevision(t *testing.T) {
	s := annotationFixture(t)
	index, _ := s.c.archive.Find("equipment/first.equ")
	if err := s.c.archive.SetText(index, "[name]\r\n`😀中文`\r\n[price]\r\n1\r\n[price]\r\n2"); err != nil {
		t.Fatal(err)
	}
	current := mcpCurrent(s)
	a := annotationArgs{WorkspaceID: current.WorkspaceID, ExpectedRevision: current.ExpectedRevision, FilePath: "equipment/first.equ", Section: "price", Limit: 1}
	r, err := s.annotationExplain(a)
	if err != nil {
		t.Fatal(err)
	}
	page := r.(map[string]any)
	if page["contentSource"] != "workspace" || page["coordinateUnit"] != "UTF-16" {
		t.Fatal("coordinate contract")
	}
	items := page["annotations"].([]map[string]any)
	if len(items) != 1 || items[0]["occurrence"] != 0 {
		t.Fatal("first occurrence")
	}
	a.Cursor = page["nextCursor"].(string)
	if _, err = s.annotationExplain(a); err != nil {
		t.Fatal(err)
	}
	a.Cursor = ""
	one := 1
	a.Occurrence = &one
	a.Limit = 100
	r, err = s.annotationExplain(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range r.(map[string]any)["annotations"].([]map[string]any) {
		if item["occurrence"] != 1 {
			t.Fatal("occurrence ignored")
		}
		pos := item["range"].(map[string]any)["startPosition"].(map[string]int)
		if pos["line"] < 5 {
			t.Fatal("CRLF coordinates")
		}
	}
	a.Occurrence = nil
	a.Limit = 1
	a.Cursor = page["nextCursor"].(string)
	if err = s.c.archive.SetText(index, "[price]\n2"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.annotationExplain(a); err == nil || !strings.Contains(err.Error(), "STALE_TEXT") {
		t.Fatal("text cursor accepted", err)
	}
	a.Cursor = ""
	a.ExpectedRevision++
	if _, err = s.annotationExplain(a); err == nil || !strings.Contains(err.Error(), "STALE_WORKSPACE") {
		t.Fatal("workspace revision accepted")
	}
}
func TestMCPAnnotationNoArchiveAndSchema(t *testing.T) {
	s := annotationFixture(t)
	s.c.closeArchive()
	if _, err := s.annotationSearch(annotationArgs{}); err == nil {
		t.Fatal("current without archive")
	}
	if _, err := s.annotationSearch(annotationArgs{PVFVersion: "invalid"}); err == nil {
		t.Fatal("invalid version")
	}
	if _, err := s.annotationSearch(annotationArgs{PVFVersion: "all"}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range s.annotationTools() {
		if !tool.ReadOnly || tool.Schema == nil {
			t.Fatal(fmt.Sprintf("invalid tool %s", tool.Name))
		}
	}
}

func TestMCPAnnotationFailedReloadKeepsActiveDocumentation(t *testing.T) {
	c, rules := ruleServiceFixture(t)
	s := &MCPService{c: c}
	before := searchDocs(t, s, annotationArgs{PVFVersion: "all"})
	file := filepath.Join(c.ruleStore.SourceDir, "annotations.json")
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rules.reload("annotations"); err == nil {
		t.Fatal("invalid reload accepted")
	}
	after := searchDocs(t, s, annotationArgs{PVFVersion: "all"})
	if before["rulesRevision"] != after["rulesRevision"] || before["totalMatches"] != after["totalMatches"] {
		t.Fatal("disk corruption changed active docs")
	}
	good := `{"version":1,"rules":[{"id":"new","match":{},"target":{"kind":"path"},"annotation":{"type":"text","title":"new"}}]}`
	if err := os.WriteFile(file, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	// Merely changing the file must not activate it.
	unchanged := searchDocs(t, s, annotationArgs{PVFVersion: "all"})
	if unchanged["rulesRevision"] != before["rulesRevision"] {
		t.Fatal("unloaded disk rules visible")
	}
	if err := rules.reload("annotations"); err != nil {
		t.Fatal(err)
	}
	after = searchDocs(t, s, annotationArgs{PVFVersion: "all"})
	if after["rulesRevision"] == before["rulesRevision"] {
		t.Fatal("successful reload did not advance rules revision")
	}
}

func TestMCPAnnotationLargeEnumPagination(t *testing.T) {
	s := annotationFixture(t)
	doc := s.c.annotationEngine.Document()
	values := map[string]string{}
	for i := 0; i < 250; i++ {
		values[fmt.Sprintf("%03d", i)] = strings.Repeat("含义", 20)
	}
	doc.Fields[0].Annotation.Values = values
	engine, err := ann.Compile(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.c.annotationEngine = engine
	page := searchDocs(t, s, annotationArgs{PVFVersion: "all", Kinds: []string{"field"}})
	item := page["items"].([]map[string]any)[0]
	a := annotationArgs{DocumentID: item["documentId"].(string), RulesRevision: page["rulesRevision"].(string), ValuesLimit: 100}
	seen := map[string]bool{}
	for {
		raw, err := s.annotationGet(a)
		if err != nil {
			t.Fatal(err)
		}
		detail := raw.(map[string]any)
		for _, v := range detail["values"].([]map[string]string) {
			if seen[v["value"]] {
				t.Fatal("duplicate enum page")
			}
			seen[v["value"]] = true
		}
		if detail["nextValuesCursor"] == nil {
			break
		}
		a.ValuesCursor = detail["nextValuesCursor"].(string)
	}
	if len(seen) != 250 {
		t.Fatal("enum pagination lost keys")
	}
}

func TestMCPAnnotationRepeatedContextReferences(t *testing.T) {
	s := annotationFixture(t)
	idx, contextIndex := 1, 0
	doc := ann.Document{Version: 1, Relations: map[string]ann.RelationSpec{"skill": {Kind: "contextual", ContextToken: 0, IDToken: 0, PathToken: 1, RecordTokens: 2, NameSection: "name", ContextPaths: map[string]string{"[fighter]": "skill/fighterskill.lst"}}}, Rules: []ann.Rule{{ID: "skills", Target: ann.TargetSpec{Kind: "token", Section: "skills", Index: &idx, RecordTokens: 3, ContextIndex: &contextIndex}, Annotation: ann.AnnotationSpec{Type: "reference", Title: "技能", Relation: "skill"}}}}
	engine, err := ann.Compile(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.c.annotationEngine = engine
	s.c.annotationRelations = nil
	s.c.editorAnnotation = editorAnnotationCache{}
	index, _ := s.c.archive.Find("equipment/first.equ")
	if err = s.c.archive.SetText(index, "[skills]\n`[fighter]` 7 1\n`[fighter]` 8 2"); err != nil {
		t.Fatal(err)
	}
	current := mcpCurrent(s)
	raw, err := s.annotationExplain(annotationArgs{WorkspaceID: current.WorkspaceID, ExpectedRevision: current.ExpectedRevision, FilePath: "equipment/first.equ", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	items := raw.(map[string]any)["annotations"].([]map[string]any)
	if len(items) != 2 {
		t.Fatal("repeated record anchors")
	}
	for i, item := range items {
		details := item["details"].([]map[string]any)
		if len(details) != 1 {
			t.Fatal("record detail missing")
		}
		ref := details[0]["reference"].(map[string]any)
		if ref["context"] != "[fighter]" || ref["id"] != fmt.Sprint(7+i) || ref["resolutionStatus"] != "missing" {
			t.Fatal("context or missing reference status")
		}
	}
}
