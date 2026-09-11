package annotations

import (
	"testing"

	"pvfine/internal/pvf"
)

func TestExtractPreviewFieldsUsesSharedTargets(t *testing.T) {
	index := 1
	context := 0
	engine, err := Compile(Document{
		Version: 1,
		Fields: []FieldDefinition{
			{
				ID:         "equ.name",
				Match:      MatchSpec{Extensions: []string{".equ"}},
				Target:     TargetSpec{Kind: "token", Section: "name", Index: intPtr(0)},
				Annotation: AnnotationSpec{Title: "名称", Type: "text"},
				Preview:    &PreviewSpec{Provider: "equ", Role: "name", Group: "header", Format: "text"},
			},
			{
				ID:         "equ.skill",
				Match:      MatchSpec{Extensions: []string{".equ"}},
				Target:     TargetSpec{Kind: "token", Section: "skill levelup", Index: &index, RecordTokens: 3, ContextIndex: &context},
				Annotation: AnnotationSpec{Title: "技能", Type: "text"},
				Preview:    &PreviewSpec{Provider: "equ", Role: "skill-levelup", Group: "skills", Format: "skill-levelup"},
			},
		},
		Rules: []Rule{},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := engine.ExtractPreviewFields("equipment/a.equ", pvf.ParseScriptView("[name]\n`测试`\n[skill levelup]\n`[swordman]` 38 2\n`[knight]` 14 1\n[/skill levelup]"), "equ")
	if len(values) != 3 || values[0].Values[0] != "测试" {
		t.Fatalf("values = %#v", values)
	}
	if values[1].Context != "[swordman]" || len(values[1].Values) != 3 || values[2].Values[1] != "14" {
		t.Fatalf("repeated values = %#v", values)
	}
}

func TestFieldRuleReferencesSharedDefinition(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Fields: []FieldDefinition{{
			ID:         "equ.rarity",
			Match:      MatchSpec{Extensions: []string{".equ"}},
			Target:     TargetSpec{Kind: "token", Section: "rarity", Index: intPtr(0)},
			Annotation: AnnotationSpec{Title: "稀有度", Type: "enum", Values: map[string]string{"4": "史诗"}},
		}},
		Rules: []Rule{{ID: "rarity.rule", Field: "equ.rarity"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Annotate("equipment/a.equ", pvf.ParseScriptView("[rarity]\n4"), nil)
	if len(results) != 1 || results[0].Title != "史诗" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSharedFieldLoadsAsAnnotationAndSupportsMultipleProviders(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Fields: []FieldDefinition{{
			ID:         "global.cool-time",
			Match:      MatchSpec{Extensions: []string{".equ", ".stk"}},
			Target:     TargetSpec{Kind: "token", Section: "cool time", Index: intPtr(0)},
			Annotation: AnnotationSpec{Title: "冷却时间", Type: "text"},
			Preview:    &PreviewSpec{Providers: []string{"equ", "stk"}, Role: "cool-time", Group: "summary", Format: "text"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, extension := range []string{".equ", ".stk"} {
		results := engine.Annotate("item/test"+extension, pvf.ParseScriptView("[cool time]\n100"), nil)
		if len(results) != 1 || results[0].Title != "冷却时间" {
			t.Fatalf("extension %s results = %#v", extension, results)
		}
	}
	if len(engine.PreviewFields("item/test.equ", "equ")) != 1 || len(engine.PreviewFields("item/test.stk", "stk")) != 1 {
		t.Fatal("multiple preview providers were not matched")
	}
}

func TestLoadDefaultIncludesEquipmentPreviewFields(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	fields := engine.PreviewFields("equipment/a.equ", "equ")
	if len(fields) < 20 {
		t.Fatalf("equipment fields = %d", len(fields))
	}
}
