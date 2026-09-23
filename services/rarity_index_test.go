package services

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"pvfine/internal/pvf"
)

// writeRarityFixture writes an archive whose registered items cover the three
// states the explorer has to tell apart: a real rarity, the 普通 value 0 (which
// must survive as 0 rather than degrade into "unknown") and a file without any
// [rarity] section.
func writeRarityFixture(t *testing.T, name string) string {
	t.Helper()
	a := pvf.New()
	for _, file := range []struct{ path, text string }{
		{"equipment/equipment.lst", "2001 `character/common/amulet/epic.equ` 2002 `character/common/amulet/normal.equ` 2003 `character/common/amulet/plain.equ`"},
		{"stackable/stackable.lst", "3001 `consumable/potion.stk`"},
		{"equipment/character/common/amulet/epic.equ", "[name]\n`史诗项链`\n[rarity]\n4\n[usable job]\n`[all]`"},
		{"equipment/character/common/amulet/normal.equ", "[name]\n`普通项链`\n[rarity]\n0\n[usable job]\n`[all]`"},
		{"equipment/character/common/amulet/plain.equ", "[name]\n`无稀有度项链`\n[usable job]\n`[all]`"},
		{"stackable/consumable/potion.stk", "[name]\n`稀有回复药`\n[rarity]\n2"},
	} {
		if _, err := a.AddFileText(file.path, file.text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), name)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return output
}

func openRarityFixture(t *testing.T, name string) (*core, *ArchiveService) {
	t.Helper()
	c := NewCore()
	service := NewArchiveService(c)
	if _, err := service.Open(writeRarityFixture(t, name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)
	return c, service
}

func TestSearchHitsCarryRarity(t *testing.T) {
	_, service := openRarityFixture(t, "rarity-search.pvf")

	found := make(map[string]int32)
	for _, query := range []string{"项链", "回复药"} {
		result, err := service.Search(query, 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range result.Hits {
			if hit.Category == SearchCategoryFile {
				continue
			}
			found[hit.Path] = hit.Rarity
		}
	}

	for path, want := range map[string]int32{
		"equipment/character/common/amulet/epic.equ":   4,
		"equipment/character/common/amulet/normal.equ": 0,
		"equipment/character/common/amulet/plain.equ":  pvf.RarityUnknown,
		"stackable/consumable/potion.stk":              2,
	} {
		got, ok := found[path]
		if !ok {
			t.Fatalf("%s missing from search results: %#v", path, found)
		}
		if got != want {
			t.Fatalf("%s rarity = %d, want %d", path, got, want)
		}
	}
}

func TestExplorerTagsCarryRarity(t *testing.T) {
	c, service := openRarityFixture(t, "rarity-tags.pvf")

	nodes, err := service.ListChildren("equipment/character/common/amulet")
	if err != nil {
		t.Fatal(err)
	}
	tagsByPath := make(map[string]int32)
	for _, node := range nodes {
		if node.IsDir || len(node.Tags) == 0 {
			continue
		}
		tagsByPath[node.Path] = node.Tags[0].Rarity
	}
	for path, want := range map[string]int32{
		"equipment/character/common/amulet/epic.equ":   4,
		"equipment/character/common/amulet/normal.equ": 0,
		"equipment/character/common/amulet/plain.equ":  pvf.RarityUnknown,
	} {
		got, ok := tagsByPath[path]
		if !ok {
			t.Fatalf("%s has no indexed tag: %#v", path, tagsByPath)
		}
		if got != want {
			t.Fatalf("%s tag rarity = %d, want %d", path, got, want)
		}
	}

	// A file that declares no rarity keeps the unknown sentinel instead of a
	// color that would claim a rarity it never had.
	resolved, err := service.ResolveFiles([]string{"equipment/equipment.lst"})
	if err != nil || len(resolved) != 1 {
		t.Fatalf("resolve = %#v, err = %v", resolved, err)
	}
	if visuals := c.fileVisualsLocked(resolved[0].FileIndex); visuals.rarity != pvf.RarityUnknown {
		t.Fatalf("unregistered visuals rarity = %d, want %d", visuals.rarity, pvf.RarityUnknown)
	}
}

func TestRarityOnlyEditRefreshesExplorerTags(t *testing.T) {
	c, service := openRarityFixture(t, "rarity-edit.pvf")

	target, ok := c.archive.Find("equipment/character/common/amulet/plain.equ")
	if !ok {
		t.Fatal("registered target missing")
	}
	// Only the [rarity] section changes: the index must still refresh, because
	// the explorer colors the tag from this value.
	if err := NewEditorService(c).SetText(target, "[name]\n`无稀有度项链`\n[rarity]\n6\n[usable job]\n`[all]`"); err != nil {
		t.Fatal(err)
	}
	waitForSearchRefresh(t, c)

	nodes, err := service.ListChildren("equipment/character/common/amulet")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Path != "equipment/character/common/amulet/plain.equ" {
			continue
		}
		if len(node.Tags) == 0 || node.Tags[0].Rarity != 6 {
			t.Fatalf("refreshed tag = %#v, want rarity 6", node.Tags)
		}
		result, err := service.Search("无稀有度项链", 0, 5)
		if err != nil || len(result.Hits) != 1 || result.Hits[0].Rarity != 6 {
			t.Fatalf("refreshed search = %#v, err = %v", result.Hits, err)
		}
		return
	}
	t.Fatal("edited file missing from the explorer listing")
}

func TestShopItemsCarryRarity(t *testing.T) {
	c, _ := openRarityFixture(t, "rarity-shop.pvf")
	shopText := "[sell info]\n[tab]\n`测试`\n[item list]\n2001 2003\n[/item list]\n[/tab]\n[/sell info]"
	itemIndex, err := c.archive.AddFileText("itemshop/test.shp", shopText, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	shop, err := NewFileGUIService(c).ReadShop(itemIndex, shopText)
	if err != nil {
		t.Fatal(err)
	}
	if len(shop.Tabs) != 1 || len(shop.Tabs[0].Groups) != 1 || len(shop.Tabs[0].Groups[0].Items) != 2 {
		t.Fatalf("shop shape = %#v", shop.Tabs)
	}
	items := shop.Tabs[0].Groups[0].Items
	if items[0].Item.Rarity != 4 || items[1].Item.Rarity != pvf.RarityUnknown {
		t.Fatalf("shop rarities = %d, %d, want 4 and %d", items[0].Item.Rarity, items[1].Item.Rarity, pvf.RarityUnknown)
	}
}

// TestRealRarityMatchesPreview checks, on a real archive, that the value the
// index exposes is exactly what the equipment tooltip shows: the explorer tag
// and the tooltip must never disagree about an item's rarity.
func TestRealRarityMatchesPreview(t *testing.T) {
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	preview := NewPreviewService()
	checked := 0
	for _, sample := range []string{
		"equipment/character/common/amulet/100300001.equ",
		"equipment/character/common/amulet/100300002.equ",
		"stackable/10000001/10000039.stk",
	} {
		index, ok := a.Find(sample)
		if !ok {
			continue
		}
		metadata, err := a.ScriptMetadata(index)
		if err != nil {
			t.Fatal(err)
		}
		text, err := a.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		document, err := preview.ParseEQU(index, text)
		if err != nil {
			t.Fatal(err)
		}
		if got := metadata.RarityValue(); got != document.Rarity {
			t.Fatalf("%s index rarity = %d, preview rarity = %d", sample, got, document.Rarity)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("真实 PVF 中没有稀有度样本")
	}
}

// TestSQLiteIndexCarriesRarity covers the disk-backed index used by large
// archives, where the tags are joined against the per-file visuals row.
func TestSQLiteIndexCarriesRarity(t *testing.T) {
	a := pvf.New()
	files := []struct{ path, text string }{
		{"equipment/equipment.lst", "2001 `character/epic.equ` 2003 `character/plain.equ`"},
		{"equipment/character/epic.equ", "[name]\n`史诗项链`\n[rarity]\n5"},
		{"equipment/character/plain.equ", "[name]\n`无稀有度项链`"},
	}
	for _, file := range files {
		if _, err := a.AddFileText(file.path, file.text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}

	index, _, err := openSQLiteArchiveIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	defer index.close()
	c := NewCore()
	c.mu.Lock()
	c.archive = a
	c.diskIndex = index
	c.indexGen = 1
	c.mu.Unlock()
	specs := []searchableListSpec{{listPath: "equipment/equipment.lst", category: SearchCategoryEquipment}}
	if _, _, err := index.buildSemantic(context.Background(), c, a, specs, 1); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]int32{
		"equipment/character/epic.equ":  5,
		"equipment/character/plain.equ": pvf.RarityUnknown,
	} {
		fileIndex, ok := a.Find(path)
		if !ok {
			t.Fatalf("%s missing from the archive", path)
		}
		tags, err := index.tags(fileIndex)
		if err != nil || len(tags) != 1 {
			t.Fatalf("%s tags = %#v, err = %v", path, tags, err)
		}
		if tags[0].Rarity != want {
			t.Fatalf("%s tag rarity = %d, want %d", path, tags[0].Rarity, want)
		}
		if visuals := index.visuals(fileIndex); visuals.rarity != want {
			t.Fatalf("%s visuals rarity = %d, want %d", path, visuals.rarity, want)
		}
	}
}

func TestEditorItemAnnotationsCarryRarity(t *testing.T) {
	c, _ := openRarityFixture(t, "rarity-annotations.pvf")
	editor := NewEditorService(c)
	for _, sample := range []struct {
		path string
		name string
		want int32
	}{
		{"equipment/equipment.lst", "史诗项链", 4},
		{"equipment/equipment.lst", "普通项链", 0},
		{"equipment/equipment.lst", "无稀有度项链", pvf.RarityUnknown},
		{"stackable/stackable.lst", "稀有回复药", 2},
	} {
		index, ok := c.archive.Find(sample.path)
		if !ok {
			t.Fatalf("missing fixture list: %s", sample.path)
		}
		annotations, err := editor.GetAnnotations(index)
		if err != nil {
			t.Fatal(err)
		}
		annotation := findEditorAnnotation(annotations, sample.name)
		if annotation == nil || annotation.Rarity != sample.want {
			t.Fatalf("%s annotation = %#v, want rarity %d", sample.name, annotation, sample.want)
		}
	}
}
