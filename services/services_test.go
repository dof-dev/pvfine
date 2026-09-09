package services

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

func openForTest(path string) (*pvf.Archive, error) {
	return pvf.Open(path)
}

func mustReopen(t *testing.T, path string) *pvf.Archive {
	t.Helper()
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testArchive(t *testing.T) *core {
	t.Helper()
	src := os.Getenv("PVF_TESTFILE")
	if src == "" {
		t.Skip("PVF_TESTFILE not set; skipping service integration test")
	}
	// 硬链接到临时目录,编辑/保存测试不会触碰源文件
	link := filepath.Join(t.TempDir(), "Script.pvf")
	if err := os.Link(src, link); err != nil {
		// 跨盘等情况下退回复制
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	a, err := openForTest(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	t.Cleanup(c.closeArchive)
	return c
}

func waitForSearchIndex(t *testing.T, c *core) IndexStatus {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	svc := NewArchiveService(c)
	for time.Now().Before(deadline) {
		status := svc.IndexStatus()
		if status.State == IndexStateReady || status.State == IndexStateError {
			if status.State == IndexStateError {
				t.Fatalf("search index failed: %s", status.Error)
			}
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("search index did not finish: %#v", svc.IndexStatus())
	return IndexStatus{}
}

func writeSearchFixture(t *testing.T, name string) string {
	t.Helper()
	a := pvf.New()
	_, err := a.AddFileText("equipment/equipment.lst", "1008 `character/common/amulet/1008.equ` 1010 `character/common/amulet/1008.equ` 1011 `character/common/amulet/missing.equ`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.AddFileText("stackable/stackable.lst", "1008 `consumable/1008.stk`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.AddFileText("equipment/character/common/amulet/1008.equ", "[name]\n`烈火之心项链`\n[grade]\n1", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.AddFileText("stackable/consumable/1008.stk", "[name]\n`回复药`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	a.AddFile("misc/readme.txt", []byte("hello"), pvf.TypeScript)

	path := filepath.Join(t.TempDir(), name)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyntheticSearchIndex(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	path := writeSearchFixture(t, "search.pvf")
	if _, err := svc.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	status := waitForSearchIndex(t, c)
	if status.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1", status.Skipped)
	}

	byID, err := svc.Search("1008", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(byID.Hits) != 3 {
		t.Fatalf("id hits = %d, want 3", len(byID.Hits))
	}
	counts := map[string]int{}
	for _, hit := range byID.Hits {
		counts[hit.Category]++
	}
	if counts[SearchCategoryEquipment] != 2 || counts[SearchCategoryStackable] != 1 {
		t.Fatalf("id categories = %#v", counts)
	}

	byName, err := svc.Search("烈火之心项链", 0, 20)
	if err != nil || len(byName.Hits) != 2 {
		t.Fatalf("name hits = %d, err = %v", len(byName.Hits), err)
	}
	if byName.Hits[0].ID == "" || byName.Hits[0].Path != "equipment/character/common/amulet/1008.equ" {
		t.Fatalf("name hit = %#v", byName.Hits[0])
	}

	byPath, err := svc.Search("misc/readme.txt", 0, 20)
	if err != nil || len(byPath.Hits) != 1 {
		t.Fatalf("path hits = %d, err = %v", len(byPath.Hits), err)
	}
	if byPath.Hits[0].Category != SearchCategoryFile || byPath.Hits[0].Name != "readme.txt" {
		t.Fatalf("path hit = %#v", byPath.Hits[0])
	}

	exactName, err := svc.SearchExact("readme.txt", 0, 20)
	if err != nil || len(exactName.Hits) != 1 {
		t.Fatalf("exact name hits = %d, err = %v", len(exactName.Hits), err)
	}
	exactPartial, err := svc.SearchExact("readme", 0, 20)
	if err != nil || len(exactPartial.Hits) != 0 {
		t.Fatalf("exact partial hits = %d, err = %v", len(exactPartial.Hits), err)
	}

	amulet, err := svc.ListChildren("equipment/character/common/amulet")
	if err != nil {
		t.Fatal(err)
	}
	var amuletFile *TreeNode
	for _, node := range amulet {
		if node.Path == "equipment/character/common/amulet/1008.equ" {
			amuletFile = node
			break
		}
	}
	if amuletFile == nil || len(amuletFile.Tags) != 2 {
		t.Fatalf("tree tags = %#v, want two equipment mappings", amuletFile)
	}
	if amuletFile.Tags[0].ID != "1008" || amuletFile.Tags[0].Name != "烈火之心项链" ||
		amuletFile.Tags[1].ID != "1010" || amuletFile.Tags[1].Name != "烈火之心项链" {
		t.Fatalf("tree tags = %#v", amuletFile.Tags)
	}
}

func TestSearchSupportsBasicWildcards(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	path := writeSearchFixture(t, "wildcard-search.pvf")
	if _, err := svc.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)

	byExtension, err := svc.Search("*.equ", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(byExtension.Hits) != 2 {
		t.Fatalf("extension wildcard hits = %d, want 2", len(byExtension.Hits))
	}
	for _, hit := range byExtension.Hits {
		if !strings.HasSuffix(hit.Path, ".equ") {
			t.Fatalf("extension wildcard escaped pattern: %q", hit.Path)
		}
	}

	byQuestion, err := svc.Search("misc/readme.tx?", 0, 20)
	if err != nil || len(byQuestion.Hits) != 1 || byQuestion.Hits[0].Path != "misc/readme.txt" {
		t.Fatalf("question wildcard result = %#v, err = %v", byQuestion.Hits, err)
	}

	byExactGlob, err := svc.SearchExact("misc/*.txt", 0, 20)
	if err != nil || len(byExactGlob.Hits) != 1 || byExactGlob.Hits[0].Path != "misc/readme.txt" {
		t.Fatalf("exact wildcard result = %#v, err = %v", byExactGlob.Hits, err)
	}

	withoutWildcard, err := svc.Search("readme", 0, 20)
	if err != nil || len(withoutWildcard.Hits) != 1 || withoutWildcard.Hits[0].Path != "misc/readme.txt" {
		t.Fatalf("plain search result = %#v, err = %v", withoutWildcard.Hits, err)
	}
}

func TestListDescendantFiles(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	path := writeSearchFixture(t, "descendants.pvf")
	if _, err := svc.Open(path); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	root, err := svc.ListDescendantFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 5 {
		t.Fatalf("root files = %d, want 5", len(root))
	}
	seenIndexes := make(map[int32]bool, len(root))
	for i, node := range root {
		if node == nil || node.IsDir || node.FileIndex < 0 {
			t.Fatalf("root file %d invalid: %#v", i, node)
		}
		if i > 0 && root[i-1].Path >= node.Path {
			t.Fatalf("root files are not sorted: %q then %q", root[i-1].Path, node.Path)
		}
		if seenIndexes[node.FileIndex] {
			t.Fatalf("duplicate file index %d", node.FileIndex)
		}
		seenIndexes[node.FileIndex] = true
	}

	equipment, err := svc.ListDescendantFiles("equipment")
	if err != nil {
		t.Fatal(err)
	}
	if len(equipment) != 2 {
		t.Fatalf("equipment files = %d, want 2", len(equipment))
	}
	for _, node := range equipment {
		if !strings.HasPrefix(node.Path, "equipment/") {
			t.Fatalf("equipment scope escaped: %q", node.Path)
		}
	}

	amulet, err := svc.ListDescendantFiles("equipment/character/common/amulet/")
	if err != nil {
		t.Fatal(err)
	}
	if len(amulet) != 1 || amulet[0].Path != "equipment/character/common/amulet/1008.equ" {
		t.Fatalf("amulet files = %#v", amulet)
	}

	missing, err := svc.ListDescendantFiles("does/not/exist")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing scope files = %d, want 0", len(missing))
	}
}

func TestResolveFiles(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	path := writeSearchFixture(t, "resolve-files.pvf")
	if _, err := svc.Open(path); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	waitForSearchIndex(t, c)

	resolved, err := svc.ResolveFiles([]string{
		"misc/readme.txt",
		"equipment/character/common/amulet/1008.equ",
		"misc/readme.txt",
		"does/not/exist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved files = %d, want 2", len(resolved))
	}
	if resolved[0].Path != "misc/readme.txt" || resolved[0].Name != "readme.txt" {
		t.Fatalf("first resolved file = %#v", resolved[0])
	}
	if resolved[1].Path != "equipment/character/common/amulet/1008.equ" ||
		len(resolved[1].Tags) != 2 {
		t.Fatalf("second resolved file = %#v", resolved[1])
	}
}

func TestSyntheticSkillSearchIndex(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	mustAddText(t, a, "skill/fighterskill.lst", "20 `Fighter/Skill20.skl`", pvf.TypeScript)
	skillIndex := mustAddText(t, a, "skill/fighter/skill20.skl", "[name]\n`旋风腿`", pvf.TypeScript)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	result, err := NewArchiveService(c).Search("旋风腿", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Category != SearchCategorySkill ||
		result.Hits[0].ID != "20" || result.Hits[0].FileIndex != skillIndex {
		t.Fatalf("skill search result = %#v", result.Hits)
	}
	children, err := NewArchiveService(c).ListChildren("skill/fighter")
	if err != nil {
		t.Fatal(err)
	}
	skill := findTreeNode(children, "skill20.skl")
	if skill == nil || len(skill.Tags) != 1 || skill.Tags[0].ID != "20" || skill.Tags[0].Name != "旋风腿" {
		t.Fatalf("skill tree tags = %#v", skill)
	}
}

func TestConfiguredListIndexesExposeTreeTags(t *testing.T) {
	type listCase struct {
		listPath  string
		entryPath string
		target    string
		id        string
		name      string
	}
	cases := []listCase{
		{listPath: "appendage/appendage.lst", entryPath: "apd/example.apd", target: "appendage/apd/example.apd", id: "apd-1", name: "附加对象"},
		{listPath: "character/character.lst", entryPath: "example.chr", target: "character/example.chr", id: "character-1", name: "鬼剑士"},
		{listPath: "creature/creature.lst", entryPath: "example.cre", target: "creature/example.cre", id: "creature-1", name: "测试宠物"},
		{listPath: "dungeon/dungeon.lst", entryPath: "example.dgn", target: "dungeon/example.dgn", id: "dungeon-1", name: "测试地下城"},
		{listPath: "itemshop/itemshop.lst", entryPath: "example.shop", target: "itemshop/example.shop", id: "shop-1", name: "测试商店"},
		{listPath: "map/map.lst", entryPath: "example.map", target: "map/example.map", id: "map-1", name: "测试地图"},
		{listPath: "monster/monster.lst", entryPath: "example.mon", target: "monster/example.mon", id: "monster-1", name: "测试怪物"},
		{listPath: "n_quest/quest.lst", entryPath: "example.qst", target: "n_quest/example.qst", id: "quest-1", name: "测试任务"},
		{listPath: "npc/npc.lst", entryPath: "example.npc", target: "npc/example.npc", id: "npc-1", name: "测试 NPC"},
		{listPath: "passiveobject/passiveobject.lst", entryPath: "example.obj", target: "passiveobject/example.obj", id: "object-1", name: "测试被动对象"},
		{listPath: "region/region.lst", entryPath: "example.reg", target: "region/example.reg", id: "region-1", name: "测试区域"},
		{listPath: "town/town.lst", entryPath: "example.twn", target: "town/example.twn", id: "town-1", name: "测试城镇"},
		{listPath: "worldmap/worldmap.lst", entryPath: "example.wmp", target: "worldmap/example.wmp", id: "worldmap-1", name: "测试副本接口"},
	}

	c := NewCore()
	a := pvf.New()
	for _, item := range cases {
		if _, err := a.AddFileText(item.listPath, item.id+" `"+item.entryPath+"`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AddFileText(item.target, "[name]\n`"+item.name+"`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	svc := NewArchiveService(c)
	for _, item := range cases {
		targetIndex, ok := a.Find(item.target)
		if !ok {
			t.Fatalf("fixture target missing: %s", item.target)
		}
		slash := strings.LastIndexByte(item.target, '/')
		parent, base := item.target[:slash], item.target[slash+1:]
		nodes, err := svc.ListChildren(parent)
		if err != nil {
			t.Fatal(err)
		}
		node := findTreeNode(nodes, base)
		if node == nil || node.FileIndex != targetIndex {
			t.Fatalf("tree node for %s = %#v", item.target, node)
		}
		found := false
		for _, tag := range node.Tags {
			if tag.ID == item.id && tag.Name == item.name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tree tags for %s = %#v", item.target, node.Tags)
		}
	}
}

func TestSearchIndexUsesConfiguredRelationListPaths(t *testing.T) {
	indexToken := 0
	engine, err := annotationrules.Compile(annotationrules.Document{
		Version: 1,
		Relations: map[string]annotationrules.RelationSpec{
			"装备": {
				ListPath: "custom/equipment.lst", IDToken: 0, PathToken: 1,
				RecordTokens: 2, NameSection: "name",
			},
		},
		Rules: []annotationrules.Rule{{
			ID: "name", Target: annotationrules.TargetSpec{Kind: "token", Section: "name", Index: &indexToken},
			Annotation: annotationrules.AnnotationSpec{Title: "名称", Type: "text"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &core{annotationEngine: engine}
	a := pvf.New()
	mustAddText(t, a, "custom/equipment.lst", "1008 `item.equ`", pvf.TypeScript)
	itemIndex := mustAddText(t, a, "custom/item.equ", "[name]\n`自定义装备`", pvf.TypeScript)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	result, err := NewArchiveService(c).Search("自定义装备", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Category != SearchCategoryEquipment || result.Hits[0].FileIndex != itemIndex {
		t.Fatalf("configured relation search result = %#v", result.Hits)
	}
}

func TestArchiveServiceCreateAndDeleteFiles(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	first := a.AddFile("dir/first.equ", []byte("first"), pvf.TypeScript)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	svc := NewArchiveService(c)
	created, err := svc.CreateFile("dir/new.str", pvf.TypeUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if created == nil || created.Path != "dir/new.str" || created.FileIndex <= first {
		t.Fatalf("created node = %#v", created)
	}
	if got := svc.Info().FileCount; got != 2 {
		t.Fatalf("file count after create = %d", got)
	}

	removed, err := svc.DeleteFiles([]int32{first, created.FileIndex})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || svc.Info().FileCount != 0 {
		t.Fatalf("removed=%#v info=%#v", removed, svc.Info())
	}
	children, err := svc.ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Fatalf("root children after delete = %#v", children)
	}
}

func TestArchiveServiceDeleteFileRegistrations(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	_, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/common/amulet/1008.equ` 1009 `character/common/amulet/1009.equ`",
		pvf.TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := a.AddFileText(
		"equipment/character/common/amulet/1008.equ",
		"[name]\n`目标装备`",
		pvf.TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText(
		"equipment/character/common/amulet/1009.equ",
		"[name]\n`保留装备`",
		pvf.TypeScript,
	); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	svc := NewArchiveService(c)
	registrations, err := svc.FindFileRegistrations([]int32{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 1 || registrations[0].ID != "1008" || registrations[0].ListPath != "equipment/equipment.lst" {
		t.Fatalf("registrations = %#v", registrations)
	}

	if _, err := svc.DeleteFilesWithRegistrations([]int32{target}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.archive.Find("equipment/character/common/amulet/1008.equ"); ok {
		t.Fatal("deleted target is still indexed")
	}
	listIndex, ok := c.archive.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("list file was deleted unexpectedly")
	}
	listText, err := c.archive.Text(listIndex)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listText, "1008") || !strings.Contains(listText, "1009") {
		t.Fatalf("list after synced delete = %q", listText)
	}
}

func TestSearchRequiresReadyIndex(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	a := pvf.New()
	if _, err := a.AddFileText("misc/readme.txt", "hello", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	if _, err := svc.Search("readme", 0, 20); !errors.Is(err, ErrSearchIndexing) {
		t.Fatalf("search error = %v, want ErrSearchIndexing", err)
	}
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	if res, err := svc.Search("readme", 0, 20); err != nil || len(res.Hits) != 1 {
		t.Fatalf("ready search = %d hits, err = %v", len(res.Hits), err)
	}
}

func TestSearchIndexNameRefresh(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	path := writeSearchFixture(t, "refresh.pvf")
	if _, err := svc.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)

	before, err := svc.Search("烈火之心项链", 0, 20)
	if err != nil || len(before.Hits) != 2 {
		t.Fatalf("before hits = %d, err = %v", len(before.Hits), err)
	}
	index := before.Hits[0].FileIndex
	editor := NewEditorService(c)
	if err := editor.SetText(index, "[name]\n`改名后的项链`\n[grade]\n1"); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Search("改名后的项链", 0, 20)
	if err != nil || len(after.Hits) != 2 {
		t.Fatalf("after hits = %d, err = %v", len(after.Hits), err)
	}
	old, err := svc.Search("烈火之心项链", 0, 20)
	if err != nil || len(old.Hits) != 0 {
		t.Fatalf("old hits = %d, err = %v", len(old.Hits), err)
	}
}

func TestSearchIndexResetOnCloseAndReopen(t *testing.T) {
	c := NewCore()
	svc := NewArchiveService(c)
	first := writeSearchFixture(t, "first.pvf")
	second := writeSearchFixture(t, "second.pvf")
	if _, err := svc.Open(first); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	if status := svc.IndexStatus(); status.State != IndexStateIdle {
		t.Fatalf("status after close = %#v", status)
	}
	if _, err := svc.Open(second); err != nil {
		t.Fatal(err)
	}
	waitForSearchIndex(t, c)
	res, err := svc.Search("1008", 0, 20)
	if err != nil || len(res.Hits) != 3 {
		t.Fatalf("reopen hits = %d, err = %v", len(res.Hits), err)
	}
}

func TestArchiveServiceTreeAndSearch(t *testing.T) {
	c := testArchive(t)
	svc := NewArchiveService(c)

	if svc.Info().FileCount != 1008171 {
		t.Fatalf("file count = %d", svc.Info().FileCount)
	}
	root, err := svc.ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) == 0 || !root[0].IsDir {
		t.Fatalf("root children invalid: %d entries", len(root))
	}

	// 展开一个已知目录
	monster, err := svc.ListChildren("monster")
	if err != nil || len(monster) == 0 {
		t.Fatalf("monster dir: %v, %d entries", err, len(monster))
	}

	// 分页搜索
	res, err := svc.Search("100300001", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || !strings.HasSuffix(res.Hits[0].Path, "100300001.equ") {
		t.Fatalf("search result unexpected: %d hits", len(res.Hits))
	}
}

func TestEditorServiceEditAndSave(t *testing.T) {
	c := testArchive(t)
	archiveSvc := NewArchiveService(c)
	editorSvc := NewEditorService(c)

	// 通过搜索定位目标文件(与前端行为一致)
	res, err := archiveSvc.Search("equipment/character/common/amulet/100300001.equ", 0, 5)
	if err != nil || len(res.Hits) == 0 {
		t.Fatalf("locate target: %v", err)
	}
	idx := res.Hits[0].FileIndex

	meta, err := editorSvc.GetFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Editable || !strings.Contains(meta.Text, "[name]") {
		t.Fatalf("unexpected meta: editable=%v len=%d", meta.Editable, len(meta.Text))
	}

	if err := editorSvc.SetText(idx, "[name]\n`测试项链`\n"); err != nil {
		t.Fatal(err)
	}
	if c.archive.ModifiedCount() != 1 {
		t.Fatalf("modified count = %d", c.archive.ModifiedCount())
	}

	info, err := editorSvc.Save()
	if err != nil {
		t.Fatal(err)
	}
	if info.ModifiedCount != 0 {
		t.Fatalf("modified after save = %d", info.ModifiedCount)
	}

	// 重新打开验证落盘
	if err := c.setArchive(mustReopen(t, c.archive.SourcePath())); err != nil {
		t.Fatal(err)
	}
	meta2, err := editorSvc.GetFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta2.Text, "测试项链") {
		t.Fatalf("edit not persisted: %.80s", meta2.Text)
	}
}
