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

func TestLoadDefaultIncludesContextualSkillRelation(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	relation, ok := engine.Relation("技能")
	if !ok || relation.Kind != "contextual" || relation.ContextPaths["[fighter]"] == "" {
		t.Fatalf("skill relation = %#v, found=%v", relation, ok)
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
	lists, err := ParseLists([]byte(`{"version":1,"relations":{"equipment":{"listPath":"equipment/equipment.lst","idToken":0,"pathToken":1,"recordTokens":2,"nameSection":"name"}}}`))
	if err != nil || lists.Relations["equipment"].ListPath != "equipment/equipment.lst" {
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
