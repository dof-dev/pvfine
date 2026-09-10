package annotations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsUnsupportedVersionAndUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "version",
			json: `{"version":2,"relations":{},"rules":[]}`,
			want: "version 必须为 1",
		},
		{
			name: "unknown field",
			json: `{"version":1,"relations":{},"rules":[],"extra":true}`,
			want: "unknown field",
		},
		{
			name: "multiple documents",
			json: `{"version":1,"relations":{},"rules":[]} {}`,
			want: "只能包含一个 JSON 文档",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.json))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsInvalidRuleFields(t *testing.T) {
	index := -1
	document := Document{
		Version:   1,
		Relations: map[string]RelationSpec{},
		Rules: []Rule{{
			ID:         "invalid",
			Match:      MatchSpec{Extensions: []string{"equ"}},
			Target:     TargetSpec{Kind: "token", Section: "rarity", Index: &index},
			Annotation: AnnotationSpec{Title: "", Type: "reference", Relation: "missing"},
		}},
	}
	err := Validate(document)
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"以点开头", "不能为负数", "title 不能为空", "不存在的 relation"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestValidateTokenGroupingFields(t *testing.T) {
	dynamicIndex := 0
	valid := Rule{
		ID: "records",
		Target: TargetSpec{
			Kind: "token", Section: "records", Index: intPtr(1),
			Offset: 1, RecordTokens: 3, TokensPerLineIndex: &dynamicIndex,
		},
		Annotation: AnnotationSpec{Title: "记录", Type: "text"},
	}
	if err := Validate(Document{Version: 1, Rules: []Rule{valid}}); err != nil {
		t.Fatalf("valid grouping target rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Rule)
		want string
	}{
		{
			name: "negative offset",
			edit: func(rule *Rule) { rule.Target.Offset = -1 },
			want: "offset 不能为负数",
		},
		{
			name: "negative dynamic index",
			edit: func(rule *Rule) {
				index := -1
				rule.Target.TokensPerLineIndex = &index
			},
			want: "tokensPerLineIndex 不能为负数",
		},
		{
			name: "missing fallback",
			edit: func(rule *Rule) { rule.Target.RecordTokens = 0 },
			want: "需要配置正数 recordTokens",
		},
		{
			name: "range target",
			edit: func(rule *Rule) {
				rule.Target.Index = nil
				rule.Target.Range = &TokenRange{Start: 0, EndExclusive: 1}
			},
			want: "需要配合单个 index 使用",
		},
		{
			name: "non-token target",
			edit: func(rule *Rule) {
				rule.Target.Kind = "section"
				rule.Target.Section = "records"
			},
			want: "只允许用于 token 标注",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := valid
			test.edit(&rule)
			err := Validate(Document{Version: 1, Rules: []Rule{rule}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRuleGroupRoundTrip(t *testing.T) {
	for _, group := range []string{"", "装备"} {
		document := Document{Version: 1, Rules: []Rule{{
			ID: "name", Group: group, Target: TargetSpec{Kind: "section", Section: "name"},
			Annotation: AnnotationSpec{Title: "名称", Type: "text"},
		}}}
		data, err := Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Rules[0].Group != group {
			t.Fatalf("group = %q", parsed.Rules[0].Group)
		}
	}
}

func TestValidateInlineImageRequiresImageAnnotation(t *testing.T) {
	document := Document{Version: 1, Rules: []Rule{{
		ID:         "text",
		Target:     TargetSpec{Kind: "section", Section: "name"},
		Annotation: AnnotationSpec{Title: "名称", Type: "text", InlineImage: true},
	}}}
	err := Validate(document)
	if err == nil || !strings.Contains(err.Error(), "inlineImage 只允许用于 image 标注") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseMigratesLegacyImageTokenOrder(t *testing.T) {
	data := []byte(`{"version":1,"rules":[{"id":"icon","target":{"kind":"token","section":"icon","index":0,"recordTokens":2,"imageIndexToken":1},"annotation":{"title":"图标","type":"image"}}]}`)
	document, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	target := document.Rules[0].Target
	if target.Index == nil || *target.Index != 1 || target.ImagePathToken == nil || *target.ImagePathToken != 0 || target.ImageIndexToken != nil {
		t.Fatalf("migrated target = %#v", target)
	}
	formatted, err := MarshalRules(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(formatted), "imagePathToken") || strings.Contains(string(formatted), "imageIndexToken") {
		t.Fatalf("formatted target = %s", formatted)
	}
}

func TestLoadDefaultIncludesContextualSkillRelation(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	relation, ok := engine.Relation("技能")
	if !ok || relation.Kind != "contextual" || relation.ContextPaths["[fighter]"] == "" {
		t.Fatalf("skill relation = %#v, found=%v", relation, ok)
	}
	union, ok := engine.Relation("物品")
	if !ok || union.Kind != "union" || len(union.Relations) != 2 {
		t.Fatalf("item union relation = %#v, found=%v", union, ok)
	}
}

func TestMarshalRulesSeparatesRelations(t *testing.T) {
	document := Document{
		Version: 1,
		Relations: map[string]RelationSpec{
			"equipment": {ListPath: "equipment/equipment.lst", IDToken: 0, PathToken: 1, RecordTokens: 2, NameSection: "name"},
		},
		Rules: []Rule{{
			ID: "name", Target: TargetSpec{Kind: "section", Section: "name"},
			Annotation: AnnotationSpec{Title: "名称", Type: "text"},
		}},
	}
	data, err := MarshalRules(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "relations") {
		t.Fatalf("rules JSON contains relations: %s", data)
	}
	parsed, err := ParseRules(data)
	if err != nil || parsed.Relations != nil {
		t.Fatalf("parsed rules = %#v, err = %v", parsed, err)
	}
}

func TestParseLists(t *testing.T) {
	lists, err := ParseLists([]byte(`{"version":1,"relations":{"equipment":{"listPath":"equipment/equipment.lst","idToken":0,"pathToken":1,"recordTokens":2,"nameSection":"name"},"物品":{"kind":"union","relations":["装备","道具"]},"装备":{"listPath":"equipment/equipment.lst","idToken":0,"pathToken":1,"recordTokens":2,"nameSection":"name"},"道具":{"listPath":"stackable/stackable.lst","idToken":0,"pathToken":1,"recordTokens":2,"nameSection":"name"}}}`))
	if err != nil || lists.Relations["equipment"].ListPath != "equipment/equipment.lst" || lists.Relations["物品"].Kind != "union" {
		t.Fatalf("lists = %#v, err = %v", lists, err)
	}
}

func TestLoadFileMergesSiblingLists(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "annotations.json")
	if err := os.WriteFile(rulesPath, []byte(`{"version":1,"rules":[{"id":"ref","target":{"kind":"token","section":"related","index":0},"annotation":{"title":"关联","type":"reference","relation":"equipment"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lists.json"), []byte(`{"version":1,"relations":{"equipment":{"listPath":"equipment/equipment.lst","idToken":0,"pathToken":1,"recordTokens":2,"nameSection":"name"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := LoadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.Relation("equipment"); !ok {
		t.Fatal("sibling lists.json was not merged")
	}
}
