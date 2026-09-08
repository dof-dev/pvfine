package annotations

import (
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
