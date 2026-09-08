package annotations

import (
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func intPtr(value int) *int { return &value }

func testEngine(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	engine, err := Compile(Document{Version: 1, Relations: map[string]RelationSpec{}, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestValidateRejectsDuplicateRuleID(t *testing.T) {
	rule := Rule{
		ID: "same", Target: TargetSpec{Kind: "section", Section: "name"},
		Annotation: AnnotationSpec{Title: "名称", Type: "text"},
	}
	err := Validate(Document{Version: 1, Relations: map[string]RelationSpec{}, Rules: []Rule{rule, rule}})
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnnotateDuplicateSectionsAndConflict(t *testing.T) {
	first := Rule{
		ID: "first", Match: MatchSpec{Extensions: []string{".equ"}},
		Target:     TargetSpec{Kind: "token", Section: "rarity", Index: intPtr(0)},
		Annotation: AnnotationSpec{Title: "装备品级", Type: "enum", Values: map[string]string{"3": "神器", "4": "史诗"}},
	}
	second := first
	second.ID = "second"
	second.Annotation = AnnotationSpec{Title: "第二说明", Type: "text", Content: "补充内容"}
	engine := testEngine(t, first, second)
	results := engine.Annotate("equipment/a.equ", pvf.ParseScriptView("[rarity]\n3\n[rarity]\n4"), nil)
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	for i, result := range results {
		if result.Title != []string{"神器", "史诗"}[i] || len(result.RuleIDs) != 2 || !strings.Contains(result.Content, "第二说明") {
			t.Fatalf("result = %#v", result)
		}
	}
}

func TestAnnotateTokenRangeStaysWithinSection(t *testing.T) {
	rangeTarget := &TokenRange{Start: 0, EndExclusive: 2}
	engine := testEngine(t, Rule{
		ID: "range", Target: TargetSpec{Kind: "token", Section: "pair", Range: rangeTarget},
		Annotation: AnnotationSpec{Title: "一组数据", Type: "text"},
	})
	results := engine.Annotate("a.equ", pvf.ParseScriptView("[pair]\n1 2\n[pair]\n3 4"), nil)
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
}

func TestAnnotateReference(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Relations: map[string]RelationSpec{
			"equipment": {ListPath: "equipment/equipment.lst", IDToken: 0, PathToken: 1, NameSection: "name"},
		},
		Rules: []Rule{{
			ID: "ref", Target: TargetSpec{Kind: "token", Section: "ref", Index: intPtr(0)},
			Annotation: AnnotationSpec{Title: "关联装备", Type: "reference", Relation: "equipment"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Annotate("a.equ", pvf.ParseScriptView("[ref]\n1008"), func(_, id string) (Reference, bool) {
		return Reference{ID: id, Name: "烈火之心", FileIndex: 9}, true
	})
	if len(results) != 1 || results[0].Title != "烈火之心" || results[0].TargetFileIndex != 9 || !strings.Contains(results[0].Content, "烈火之心") {
		t.Fatalf("results = %#v", results)
	}
}

func TestAnnotateMissingReferenceDegradesGracefully(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Relations: map[string]RelationSpec{
			"equipment": {ListPath: "equipment/equipment.lst", IDToken: 0, PathToken: 1, NameSection: "name"},
		},
		Rules: []Rule{{
			ID: "ref", Target: TargetSpec{Kind: "token", Section: "ref", Index: intPtr(0)},
			Annotation: AnnotationSpec{Title: "关联装备", Type: "reference", Relation: "equipment"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Annotate("a.equ", pvf.ParseScriptView("[ref]\n404"), nil)
	if len(results) != 1 || results[0].Title != "关联装备" || results[0].TargetFileIndex != -1 || !strings.Contains(results[0].Content, "未找到关联文件") {
		t.Fatalf("results = %#v", results)
	}
}

func TestAnnotatePathSupportsRecursiveGlobAndDirectories(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "path", Match: MatchSpec{Glob: "equipment/**"}, Target: TargetSpec{Kind: "path"},
		Annotation: AnnotationSpec{Title: "装备目录", Type: "text"},
	})
	if len(engine.AnnotatePath("equipment/character", true)) != 1 {
		t.Fatal("directory should match recursive glob")
	}
	if len(engine.AnnotatePath("stackable/item.stk", false)) != 0 {
		t.Fatal("unrelated file should not match")
	}
}

func TestAnnotateUnknownEnumKeepsRuleTitle(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "rarity", Target: TargetSpec{Kind: "token", Section: "rarity", Index: intPtr(0)},
		Annotation: AnnotationSpec{Title: "装备品级", Type: "enum", Values: map[string]string{"4": "史诗"}},
	})
	results := engine.Annotate("a.equ", pvf.ParseScriptView("[rarity]\n99"), nil)
	if len(results) != 1 || results[0].Title != "装备品级" || !strings.Contains(results[0].Content, "当前值: 99") {
		t.Fatalf("results = %#v", results)
	}
}

func TestEnumTooltipListsAllValues(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "rarity", Target: TargetSpec{Kind: "token", Section: "rarity", Index: intPtr(0)},
		Annotation: AnnotationSpec{
			Title: "装备品级", Type: "enum", Content: "品级说明",
			Values: map[string]string{"4": "史诗", "0": "普通", "3": "神器"},
		},
	})
	for _, value := range []string{"4", "99"} {
		t.Run(value, func(t *testing.T) {
			results := engine.Annotate("a.equ", pvf.ParseScriptView("[rarity]\n"+value), nil)
			if len(results) != 1 {
				t.Fatalf("results = %#v", results)
			}
			if !strings.Contains(results[0].Content, "品级说明") ||
				!strings.Contains(results[0].Content, "所有枚举值:\n0 - 普通\n3 - 神器\n4 - 史诗") {
				t.Fatalf("tooltip = %q", results[0].Content)
			}
		})
	}
}
