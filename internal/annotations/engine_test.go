package annotations

import (
	"strings"
	"testing"

	"pvfine/internal/pvf"
	"pvfine/internal/rendering"
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

func TestValidatePathAnnotationRoot(t *testing.T) {
	rule := Rule{ID: "path", Target: TargetSpec{Kind: "token", Section: "file", Index: intPtr(0)}, Annotation: AnnotationSpec{Title: "文件", Type: "path", PathRoot: "equipment/character"}}
	if err := Validate(Document{Version: 1, Rules: []Rule{rule}}); err != nil {
		t.Fatal(err)
	}
	rule.Annotation.PathRoot = "../outside"
	if err := Validate(Document{Version: 1, Rules: []Rule{rule}}); err == nil || !strings.Contains(err.Error(), "pathRoot") {
		t.Fatalf("invalid root error = %v", err)
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

func TestValidateImageAnnotationTarget(t *testing.T) {
	valid := Rule{
		ID: "icon", Target: TargetSpec{Kind: "token", Section: "icon", Index: intPtr(1), RecordTokens: 2, ImagePathToken: intPtr(0)},
		Annotation: AnnotationSpec{Title: "图标", Type: "image"},
	}
	if err := Validate(Document{Version: 1, Rules: []Rule{valid}}); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ID = "invalid"
	invalid.Target.ImagePathToken = intPtr(1)
	if err := Validate(Document{Version: 1, Rules: []Rule{invalid}}); err == nil || !strings.Contains(err.Error(), "不能与图片索引 token 相同") {
		t.Fatalf("invalid image target error = %v", err)
	}
}

func TestAnnotateImageAnnotation(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "icon", Target: TargetSpec{Kind: "token", Section: "icon", Index: intPtr(1), RecordTokens: 2, ImagePathToken: intPtr(0)},
		Annotation: AnnotationSpec{Title: "图标", Type: "image", Content: "装备图标", InlineImage: true},
	})
	text := "[icon]\n`Item/test.img` 3"
	results := engine.Annotate("a.equ", pvf.ParseScriptView(text), nil)
	if len(results) != 1 || results[0].Image == nil {
		t.Fatalf("image annotation = %#v", results)
	}
	if !results[0].InlineImage {
		t.Fatalf("inline image flag was not propagated: %#v", results[0])
	}
	indexStart := strings.Index(text, "3")
	if results[0].Start != indexStart || results[0].End != indexStart+1 {
		t.Fatalf("image annotation anchor = [%d, %d), want numeric token", results[0].Start, results[0].End)
	}
	if results[0].Image.Path != "Item/test.img" || results[0].Image.Index != 3 || !strings.Contains(results[0].Content, "图片: Item/test.img[3]") {
		t.Fatalf("image annotation result = %#v", results[0])
	}
}

func TestAnnotateContextualReferenceByRepeatedRecord(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Relations: map[string]RelationSpec{
			"skill": {
				Kind: "contextual", ContextToken: 0, IDToken: 0, PathToken: 1,
				RecordTokens: 2, NameSection: "name",
				ContextPaths: map[string]string{"[fighter]": "skill/fighterskill.lst", "[at fighter]": "skill/atfighterskill.lst"},
			},
		},
		Rules: []Rule{{
			ID: "skill.levelup", Target: TargetSpec{
				Kind: "token", Section: "skill levelup", Index: intPtr(1), RecordTokens: 3,
			},
			Annotation: AnnotationSpec{Title: "技能", Type: "reference", Relation: "skill"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	view := pvf.ParseScriptView("[skill levelup]\n`[fighter]` 20 1\n`[fighter]` 19 1\n`[at fighter]` 20 1\n[/skill levelup]")
	results := engine.AnnotateWithContextResolver("skills/test.skl", view, func(relation, id, context string) (Reference, bool) {
		if relation != "skill" {
			t.Fatalf("relation = %q", relation)
		}
		return Reference{ID: context + ":" + id, Name: context + " skill " + id, FileIndex: 7}, true
	})
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Title != "[fighter] skill 20" || results[1].Title != "[fighter] skill 19" || results[2].Title != "[at fighter] skill 20" {
		t.Fatalf("titles = %#v", results)
	}
	if results[0].Start >= results[1].Start || results[1].Start >= results[2].Start {
		t.Fatalf("anchors are not ordered: %#v", results)
	}
}

func TestAnnotateRepeatedTokenWithOffset(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "records.offset",
		Target: TargetSpec{
			Kind: "token", Section: "records", Index: intPtr(0),
			Offset: 1, RecordTokens: 2,
		},
		Annotation: AnnotationSpec{Title: "记录", Type: "text"},
	})
	text := "[records]\n10 100 200 300 400 500\n[/records]"
	results := engine.Annotate("a.equ", pvf.ParseScriptView(text), nil)
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	for i, want := range []string{"100", "300"} {
		if got := text[results[i].Start:results[i].End]; got != want {
			t.Fatalf("result[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestAnnotateRepeatedTokenUsesDynamicRecordWidth(t *testing.T) {
	dynamicIndex := 0
	engine := testEngine(t, Rule{
		ID: "records.dynamic",
		Target: TargetSpec{
			Kind: "token", Section: "records", Index: intPtr(1),
			Offset: 1, RecordTokens: 3, TokensPerLineIndex: &dynamicIndex,
		},
		Annotation: AnnotationSpec{Title: "记录", Type: "text"},
	})
	text := "[records]\n2 100 200 300 400 500\n[/records]"
	results := engine.Annotate("a.equ", pvf.ParseScriptView(text), nil)
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	for i, want := range []string{"200", "400"} {
		if got := text[results[i].Start:results[i].End]; got != want {
			t.Fatalf("result[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestAnnotateStandaloneGroupsWithGroupOffset(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "world.drop",
		Target: TargetSpec{
			Kind: "token", Section: "world drop", Index: intPtr(0),
			Offset: 1, GroupOffset: 1, RecordTokens: 2,
			StandaloneValues: []int32{-1},
		},
		Annotation: AnnotationSpec{Title: "掉落", Type: "text"},
	})
	// Offset applies only once; groupOffset skips the first token of every
	// group. Trailing incomplete records and the delimiters are not annotated.
	text := "[world drop]\n999 10 100 200 300 -1 20 400 500 -1 -1 30 600 700 800\n[next]"
	results := engine.Annotate("a.etc", pvf.ParseScriptView(text), nil)
	for i, want := range []string{"100", "400", "600"} {
		if i >= len(results) {
			t.Fatalf("results = %#v", results)
		}
		if got := text[results[i].Start:results[i].End]; got != want {
			t.Fatalf("result[%d] = %q, want %q", i, got, want)
		}
	}
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
}

func TestAnnotateStandaloneGroupsDoNotMatchQuotedSentinel(t *testing.T) {
	engine := testEngine(t, Rule{
		ID: "records", Target: TargetSpec{
			Kind: "token", Section: "records", Index: intPtr(0),
			RecordTokens: 2, StandaloneValues: []int32{-1},
		},
		Annotation: AnnotationSpec{Title: "record", Type: "text"},
	})
	text := "[records]\n`-1` 5 -1 10 20"
	results := engine.Annotate("a.etc", pvf.ParseScriptView(text), nil)
	if len(results) != 2 || text[results[0].Start:results[0].End] != "`-1`" ||
		text[results[1].Start:results[1].End] != "10" {
		t.Fatalf("results = %#v", results)
	}
}

func TestDefaultIndependentDropReferencesAcrossOptionalList(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := rendering.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	text := "[independent drop]\n" +
		"1 101 201 4 5 6 7 8 9 10 11 12 13 14 15 16 17\n" +
		"[list]\n901 902\n903 904\n[/list]\n" +
		"18 102 202 21 22 23 24 25 26 27 28 29 30 31 32 33 34\n" +
		"35 103 203 38 39 40 41 42 43 44 45 46 47 48 49 50 51\n" +
		"[next]\n1"
	var resolved []string
	view := pvf.ParseScriptViewWithNestedSections(text, func(parent, child string) bool {
		for _, nested := range renderer.SectionFormat("etc/test.etc", parent).NestedSections {
			if nested == child {
				return true
			}
		}
		return false
	})
	results := engine.Annotate("etc/test.etc", view, func(relation, id string) (Reference, bool) {
		resolved = append(resolved, relation+":"+id)
		return Reference{ID: id, Name: relation + id, FileIndex: 7}, true
	})
	want := []string{"怪物:101", "物品:201", "怪物:102", "物品:202", "怪物:103", "物品:203"}
	wantResolved := []string{"怪物:101", "怪物:102", "怪物:103", "物品:201", "物品:202", "物品:203"}
	if strings.Join(resolved, ",") != strings.Join(wantResolved, ",") {
		t.Fatalf("resolved = %#v, want %#v", resolved, wantResolved)
	}
	if len(results) != len(want) {
		t.Fatalf("results = %#v", results)
	}
	for i, result := range results {
		if result.TargetFileIndex != 7 || !strings.HasSuffix(want[i], ":"+text[result.Start:result.End]) {
			t.Fatalf("result[%d] = %#v", i, result)
		}
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
