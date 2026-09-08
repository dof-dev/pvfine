package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

func TestAnnotationServiceReloadRulesKeepsOldEngineOnFailure(t *testing.T) {
	emptyEngine, err := annotationrules.Compile(annotationrules.Document{
		Version: 1, Relations: map[string]annotationrules.RelationSpec{}, Rules: []annotationrules.Rule{},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := pvf.New()
	fileIndex := mustAddText(t, a, "equipment/item.equ", "[rarity]\n4", pvf.TypeScript)
	c := &core{annotationEngine: emptyEngine}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	rulesPath := filepath.Join(t.TempDir(), "annotations.json")
	valid := []byte(`{
		"version": 1,
		"relations": {},
		"rules": [
			{"id":"path","match":{"glob":"equipment/**"},"target":{"kind":"path"},"annotation":{"title":"装备路径","type":"text"}},
			{"id":"rarity","match":{"extensions":[".equ"]},"target":{"kind":"token","section":"rarity","index":0},"annotation":{"title":"装备品级","type":"enum","values":{"4":"史诗"}}}
		]
	}`)
	if err := os.WriteFile(rulesPath, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	service := newAnnotationService(c, rulesPath)
	result, err := service.ReloadRules()
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleCount != 2 || result.RelationCount != 0 {
		t.Fatalf("reload result = %#v", result)
	}

	root, err := NewArchiveService(c).ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	equipment := findTreeNode(root, "equipment")
	if equipment == nil || len(equipment.Annotations) != 1 {
		t.Fatalf("equipment annotations = %#v", equipment)
	}
	meta, err := NewEditorService(c).GetFile(fileIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Annotations) != 1 || meta.Annotations[0].Title != "史诗" {
		t.Fatalf("editor annotations = %#v", meta.Annotations)
	}

	if err := os.WriteFile(rulesPath, []byte(`{"version":2,"relations":{},"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReloadRules(); err == nil {
		t.Fatal("invalid reload should fail")
	}
	meta, err = NewEditorService(c).GetFile(fileIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Annotations) != 1 || meta.Annotations[0].Title != "史诗" {
		t.Fatalf("old engine was not preserved: %#v", meta.Annotations)
	}
}

func TestAnnotationServicesAndCacheInvalidation(t *testing.T) {
	index := 0
	engine, err := annotationrules.Compile(annotationrules.Document{
		Version: 1,
		Relations: map[string]annotationrules.RelationSpec{
			"equipment": {
				ListPath: "equipment/equipment.lst", IDToken: 0, PathToken: 1,
				RecordTokens: 2, NameSection: "name",
			},
		},
		Rules: []annotationrules.Rule{
			{
				ID: "path.directory", Match: annotationrules.MatchSpec{Glob: "equipment/**"},
				Target:     annotationrules.TargetSpec{Kind: "path"},
				Annotation: annotationrules.AnnotationSpec{Title: "装备路径", Type: "text"},
			},
			{
				ID: "path.file", Match: annotationrules.MatchSpec{Extensions: []string{".equ"}},
				Target:     annotationrules.TargetSpec{Kind: "path"},
				Annotation: annotationrules.AnnotationSpec{Title: "装备文件", Type: "text"},
			},
			{
				ID: "rarity", Match: annotationrules.MatchSpec{Extensions: []string{".equ"}},
				Target: annotationrules.TargetSpec{Kind: "token", Section: "rarity", Index: &index},
				Annotation: annotationrules.AnnotationSpec{
					Title: "装备品级", Type: "enum", Values: map[string]string{"3": "神器", "4": "史诗"},
				},
			},
			{
				ID: "related", Match: annotationrules.MatchSpec{Extensions: []string{".equ"}},
				Target:     annotationrules.TargetSpec{Kind: "token", Section: "related", Index: &index},
				Annotation: annotationrules.AnnotationSpec{Title: "关联装备", Type: "reference", Relation: "equipment"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a := pvf.New()
	listIndex := mustAddText(t, a, "equipment/equipment.lst", "1008 `character/item.equ` 1008 `character/other.equ` 404 `character/missing.equ`", pvf.TypeScript)
	_ = listIndex
	targetIndex := mustAddText(t, a, "equipment/character/item.equ", "[name]\n`旧名称`", pvf.TypeScript)
	mustAddText(t, a, "equipment/character/other.equ", "[name]\n`重复 ID 文件`", pvf.TypeScript)
	sourceIndex := mustAddText(t, a, "equipment/character/source.equ", "[rarity]\n4\n[related]\n1008", pvf.TypeScript)
	unicodeIndex := mustAddText(t, a, "equipment/character/labels.str", "[rarity]\n4", pvf.TypeUnicode)

	c := &core{annotationEngine: engine}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	archiveService := NewArchiveService(c)
	editorService := NewEditorService(c)

	root, err := archiveService.ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	equipment := findTreeNode(root, "equipment")
	if equipment == nil || len(equipment.Annotations) != 1 || equipment.Annotations[0].Title != "装备路径" {
		t.Fatalf("equipment annotations = %#v", equipment)
	}
	files, err := archiveService.ListChildren("equipment/character")
	if err != nil {
		t.Fatal(err)
	}
	sourceNode := findTreeNode(files, "source.equ")
	if sourceNode == nil || len(sourceNode.Annotations) != 1 || len(sourceNode.Annotations[0].RuleIDs) != 2 {
		t.Fatalf("source path annotations = %#v", sourceNode)
	}

	meta, err := editorService.GetFile(sourceIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Annotations) != 2 {
		t.Fatalf("annotations = %#v", meta.Annotations)
	}
	rarity := findEditorAnnotation(meta.Annotations, "史诗")
	reference := findEditorAnnotation(meta.Annotations, "旧名称")
	if rarity == nil || !strings.Contains(rarity.Content, "4 - 史诗") {
		t.Fatalf("rarity = %#v", rarity)
	}
	if reference == nil || reference.TargetFileIndex != targetIndex || !strings.Contains(reference.Content, "旧名称") {
		t.Fatalf("reference = %#v", reference)
	}

	if err := editorService.SetText(sourceIndex, "[rarity]\n3\n[related]\n1008"); err != nil {
		t.Fatal(err)
	}
	updated, err := editorService.GetAnnotations(sourceIndex)
	if err != nil {
		t.Fatal(err)
	}
	if rarity = findEditorAnnotation(updated, "神器"); rarity == nil || !strings.Contains(rarity.Content, "3 - 神器") {
		t.Fatalf("updated rarity = %#v", rarity)
	}

	if err := editorService.SetText(targetIndex, "[name]\n`新名称`"); err != nil {
		t.Fatal(err)
	}
	updated, err = editorService.GetAnnotations(sourceIndex)
	if err != nil {
		t.Fatal(err)
	}
	if reference = findEditorAnnotation(updated, "新名称"); reference == nil || !strings.Contains(reference.Content, "新名称") {
		t.Fatalf("updated reference = %#v", reference)
	}

	unicodeMeta, err := editorService.GetFile(unicodeIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(unicodeMeta.Annotations) != 0 {
		t.Fatalf("TypeUnicode annotations = %#v", unicodeMeta.Annotations)
	}
}

func TestSearchIncludesAncestorPathAnnotations(t *testing.T) {
	engine, err := annotationrules.Compile(annotationrules.Document{
		Version:   1,
		Relations: map[string]annotationrules.RelationSpec{},
		Rules: []annotationrules.Rule{{
			ID: "path", Match: annotationrules.MatchSpec{Glob: "equipment/**"},
			Target:     annotationrules.TargetSpec{Kind: "path"},
			Annotation: annotationrules.AnnotationSpec{Title: "装备路径", Type: "text"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := pvf.New()
	mustAddText(t, a, "equipment/character/item.equ", "[name]\n`测试装备`", pvf.TypeScript)
	c := &core{annotationEngine: engine}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	result, err := NewArchiveService(c).Search("item.equ", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %#v", result.Hits)
	}
	hit := result.Hits[0]
	for _, path := range []string{"equipment", "equipment/character", "equipment/character/item.equ"} {
		if len(hit.PathAnnotations[path]) != 1 {
			t.Fatalf("path %q annotations = %#v", path, hit.PathAnnotations)
		}
	}
}

func TestContextualSkillReferenceAnnotations(t *testing.T) {
	targetTokenIndex := 1
	contextTokenIndex := 0
	engine, err := annotationrules.Compile(annotationrules.Document{
		Version: 1,
		Relations: map[string]annotationrules.RelationSpec{
			"skill": {
				Kind: "contextual", ContextToken: 0, IDToken: 0, PathToken: 1,
				RecordTokens: 2, NameSection: "name",
				ContextPaths: map[string]string{
					"[fighter]":    "skill/fighterskill.lst",
					"[at fighter]": "skill/atfighterskill.lst",
				},
			},
		},
		Rules: []annotationrules.Rule{{
			ID: "skill.levelup", Target: annotationrules.TargetSpec{
				Kind: "token", Section: "skill levelup", Index: &targetTokenIndex, RecordTokens: 3, ContextIndex: &contextTokenIndex,
			},
			Annotation: annotationrules.AnnotationSpec{Title: "技能", Type: "reference", Relation: "skill"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := pvf.New()
	mustAddText(t, a, "skill/fighterskill.lst", "20 `Fighter/Skill20.skl` 19 `Fighter/Skill19.skl`", pvf.TypeScript)
	mustAddText(t, a, "skill/atfighterskill.lst", "20 `ATFighter/At20.skl`", pvf.TypeScript)
	fighter20 := mustAddText(t, a, "skill/fighter/skill20.skl", "[name]\n`男格斗技能 20`", pvf.TypeScript)
	fighter19 := mustAddText(t, a, "skill/fighter/skill19.skl", "[name]\n`男格斗技能 19`", pvf.TypeScript)
	at20 := mustAddText(t, a, "skill/atfighter/at20.skl", "[name]\n`女格斗技能 20`", pvf.TypeScript)
	source := mustAddText(t, a, "skill/test.skl", "[skill levelup]\n`[fighter]` 20 1\n`[fighter]` 19 1\n`[at fighter]` 20 1\n[/skill levelup]", pvf.TypeScript)
	c := &core{annotationEngine: engine}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	annotations, err := NewEditorService(c).GetAnnotations(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 3 {
		t.Fatalf("annotations = %#v", annotations)
	}
	want := []struct {
		title string
		index int32
	}{
		{"男格斗技能 20", fighter20},
		{"男格斗技能 19", fighter19},
		{"女格斗技能 20", at20},
	}
	for i, item := range want {
		if annotations[i].Title != item.title || annotations[i].TargetFileIndex != item.index {
			t.Fatalf("annotation[%d] = %#v, want title=%q index=%d", i, annotations[i], item.title, item.index)
		}
	}
}

func mustAddText(t *testing.T, archive *pvf.Archive, path, text string, dataType int32) int32 {
	t.Helper()
	index, err := archive.AddFileText(path, text, dataType)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func findTreeNode(nodes []*TreeNode, name string) *TreeNode {
	for _, node := range nodes {
		if node != nil && node.Name == name {
			return node
		}
	}
	return nil
}

func findEditorAnnotation(annotations []EditorAnnotation, title string) *EditorAnnotation {
	for i := range annotations {
		if annotations[i].Title == title {
			return &annotations[i]
		}
	}
	return nil
}
