package services

import (
	"bytes"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

const editableShop = "[NPC]\n10\n[sell info]\n[tab]\n`第一页`\n[item list]\n1 1 4\n[/item list]\n[/tab]\n[tab]\n`第二页`\n[item list]\n2\n[/item list]\n[/tab]\n[/sell info]\n[message]\n`保留的消息`"

func editFixture(t *testing.T, text string) (*core, *FileGUIService, ShopEditRequest) {
	t.Helper()
	c, i := shopFixture(t)
	if err := c.archive.SetText(i, text); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c, NewFileGUIService(c), ShopEditRequest{FileIndex: i, Path: c.archive.Path(i), Text: text, Revision: c.batchRevision, TabIndex: 0}
}
func TestShopEditItemCostsAndDrafts(t *testing.T) {
	c, s, r := editFixture(t, editableShop)
	doc, err := s.ReadShop(r.FileIndex, r.Text)
	if err != nil {
		t.Fatal(err)
	}
	r.Action = "edit-item"
	r.SourceStart = doc.Tabs[0].Groups[0].Items[0].SourceStart
	r.ItemID = "1"
	r.SetGold = true
	r.Gold = "123"
	r.SetMaterials = true
	r.Materials = []ShopMaterialInput{{ItemID: "3", Quantity: "7"}}
	i, _ := c.archive.Find("stackable/one.stk")
	text, err := c.archive.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	r.Drafts = []ShopDraft{{FileIndex: i, Path: c.archive.Path(i), Text: text + "\n[explain]\n`草稿中的其他修改`"}}
	result, err := s.ApplyShopEdit(r)
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedItems != 1 || len(result.Files) != 1 {
		t.Fatalf("result=%#v", result)
	}
	got, err := c.archive.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	if firstSectionValue(got, "price") != "123" || !strings.Contains(got, "草稿中的其他修改") {
		t.Fatalf("text=%s", got)
	}
	material := shopCosts(got, "1", &ShopDocument{})
	if len(material) != 2 || material[1].ItemID != "3" || material[1].Quantity != "7" {
		t.Fatalf("cost=%#v", material)
	}
	// Both references reflect the shared product cost, but the shop structure is unchanged.
	shop, _ := c.archive.Text(r.FileIndex)
	if !strings.Contains(shop, "保留的消息") {
		t.Fatal("unrelated shop field lost")
	}
}
func TestShopEditReplacesOneOccurrenceAndLeavesOldItem(t *testing.T) {
	c, s, r := editFixture(t, editableShop)
	doc, _ := s.ReadShop(r.FileIndex, r.Text)
	oldIndex, _ := c.archive.Find("stackable/one.stk")
	old, _ := c.archive.RawBytes(oldIndex)
	old = append([]byte(nil), old...)
	r.Action = "edit-item"
	r.SourceStart = doc.Tabs[0].Groups[0].Items[0].SourceStart
	r.ItemID = "4"
	r.SetGold = true
	r.Gold = "3500"
	r.SetMaterials = true
	if _, err := s.ApplyShopEdit(r); err != nil {
		t.Fatal(err)
	}
	text, _ := c.archive.Text(r.FileIndex)
	entries := parseShop(text).Tabs[0].Groups[0].Items
	if entries[0].Item.ID != "4" || entries[1].Item.ID != "1" {
		t.Fatal("wrong duplicate replaced")
	}
	current, _ := c.archive.RawBytes(oldIndex)
	if !bytes.Equal(old, current) {
		t.Fatal("old product mutated")
	}
}
func TestShopBatchAllCategoriesAtomic(t *testing.T) {
	text := "[sell info]\n[use category]\n`expert job non filter`\n[tab]\n`商品`\n[category entry]\n[id]\n1\n[item list]\n1 1\n[/item list]\n[/category entry]\n[category entry]\n[id]\n2\n[item list]\n4\n[/item list]\n[/category entry]\n[/tab]\n[/sell info]"
	c, s, r := editFixture(t, text)
	r.Action = "batch-costs"
	r.SetGold = true
	r.Gold = "500"
	r.SetMaterials = true
	r.Materials = []ShopMaterialInput{{ItemID: "999999", Quantity: "1"}}
	before := map[int32][]byte{}
	for i := int32(0); i < c.archive.FileCount(); i++ {
		raw, _ := c.archive.RawBytes(i)
		before[i] = append([]byte(nil), raw...)
	}
	if _, err := s.ApplyShopEdit(r); err == nil {
		t.Fatal("unknown material accepted")
	}
	if c.batchRevision != r.Revision {
		t.Fatal("failed edit advanced revision")
	}
	for i, old := range before {
		raw, _ := c.archive.RawBytes(i)
		if !bytes.Equal(raw, old) {
			t.Fatal("partially applied batch")
		}
	}
	r.Materials = []ShopMaterialInput{{ItemID: "2", Quantity: "3"}}
	result, err := s.ApplyShopEdit(r)
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedItems != 2 || len(result.Files) != 2 {
		t.Fatalf("result=%#v", result)
	}
	for _, p := range []string{"stackable/one.stk", "equipment/four.equ"} {
		i, _ := c.archive.Find(p)
		text, _ := c.archive.Text(i)
		costs := shopCosts(text, p, &ShopDocument{})
		if len(costs) != 2 || costs[0].Quantity != "500" || costs[1].Quantity != "3" {
			t.Fatalf("costs=%#v", costs)
		}
	}
}
func TestShopTabOperationsAndDeletion(t *testing.T) {
	c, s, r := editFixture(t, editableShop)
	apply := func(action string) {
		t.Helper()
		r.Action = action
		r.Revision = c.batchRevision
		r.Text, _ = c.archive.Text(r.FileIndex)
		if _, err := s.ApplyShopEdit(r); err != nil {
			t.Fatal(err)
		}
	}
	r.Name = "重命名"
	apply("rename-tab")
	text, _ := c.archive.Text(r.FileIndex)
	if parseShop(text).Tabs[0].Name != "重命名" {
		t.Fatal("rename failed")
	}
	r.Name = "新分页"
	apply("add-tab")
	text, _ = c.archive.Text(r.FileIndex)
	doc := parseShop(text)
	if len(doc.Tabs) != 3 || len(doc.Tabs[2].Groups) != 1 {
		t.Fatal("new tab layout")
	}
	r.TabIndex = 2
	r.ItemID = "4"
	apply("add-item")
	text, _ = c.archive.Text(r.FileIndex)
	doc = parseShop(text)
	if len(doc.Tabs[2].Groups[0].Items) != 1 {
		t.Fatal("new product")
	}
	r.SourceStart = doc.Tabs[2].Groups[0].Items[0].SourceStart
	apply("delete-item")
	text, _ = c.archive.Text(r.FileIndex)
	if len(parseShop(text).Tabs[2].Groups[0].Items) != 0 {
		t.Fatal("delete entry")
	}
	apply("delete-tab")
	text, _ = c.archive.Text(r.FileIndex)
	if len(parseShop(text).Tabs) != 2 || !strings.Contains(text, "保留的消息") {
		t.Fatal("delete tab changed other sections")
	}
	if _, ok := c.archive.Find("equipment/four.equ"); !ok {
		t.Fatal("deleted item file")
	}
}
func TestShopEditRejectsStaleAndInvalid(t *testing.T) {
	c, s, r := editFixture(t, editableShop)
	r.Action = "rename-tab"
	r.Name = "new"
	r.Revision++
	if _, err := s.ApplyShopEdit(r); err == nil {
		t.Fatal("stale accepted")
	}
	r.Revision = c.batchRevision
	r.Name = "bad`name"
	if _, err := s.ApplyShopEdit(r); err == nil {
		t.Fatal("invalid name accepted")
	}
	r.Name = "valid"
	r.Path = "itemshop/other.shp"
	if _, err := s.ApplyShopEdit(r); err == nil {
		t.Fatal("wrong path accepted")
	}
}
func TestShopItemSearchScopeBeforePagination(t *testing.T) {
	for _, disk := range []bool{false, true} {
		c, _, _ := editFixture(t, editableShop)
		if _, err := c.archive.AddFileText("npc/npc2.npc", "[name]\n`材料商人`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
		if disk {
			index, _, err := openSQLiteArchiveIndex(c.archive)
			if err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			c.diskIndex = index
			c.installDiskArchiveIndexesLocked(c.archive)
			c.mu.Unlock()
		}
		c.startSearchIndex()
		waitForSearchIndex(t, c)
		exact, err := NewArchiveService(c).SearchItems("2", 0, 10)
		if err != nil || len(exact.Hits) == 0 || exact.Hits[0].ID != "2" || exact.Hits[0].Name != "材料甲" {
			t.Fatalf("exact match first: %#v, %v", exact, err)
		}
		cursor := 0
		ids := []string{}
		for page := 0; page < 10; page++ {
			res, err := NewArchiveService(c).SearchItems("材料", cursor, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, hit := range res.Hits {
				if hit.Category != SearchCategoryStackable {
					t.Fatal("non-item result")
				}
				ids = append(ids, hit.ID)
			}
			if res.NextCursor < 0 {
				break
			}
			cursor = res.NextCursor
		}
		if len(ids) != 2 {
			t.Fatalf("disk=%v ids=%v", disk, ids)
		}
	}
}

func TestShopCategorizedTabCanBeRecreatedAfterLastDeletion(t *testing.T) {
	text := "[sell info]\n[use category]\n`expert job`\n[tab]\n`last`\n[category entry]\n[id]\n1\n[item list]\n1\n[/item list]\n[/category entry]\n[/tab]\n[/sell info]"
	c, s, r := editFixture(t, text)
	r.Action = "delete-tab"
	if _, err := s.ApplyShopEdit(r); err != nil {
		t.Fatal(err)
	}
	r.Text, _ = c.archive.Text(r.FileIndex)
	r.Revision = c.batchRevision
	r.Action = "add-tab"
	r.Name = "重新创建"
	if _, err := s.ApplyShopEdit(r); err != nil {
		t.Fatal(err)
	}
	saved, _ := c.archive.Text(r.FileIndex)
	doc := parseShop(saved)
	if len(doc.Tabs) != 1 || len(doc.Tabs[0].Groups) != 4 {
		t.Fatalf("category groups=%#v", doc.Tabs)
	}
}

func TestShopReadItemUsesDraftCosts(t *testing.T) {
	c, s, _ := editFixture(t, editableShop)
	i, _ := c.archive.Find("stackable/one.stk")
	item, err := s.ReadItem("1", []ShopDraft{{FileIndex: i, Path: c.archive.Path(i), Text: "[price]\n42\n[need material]\n3 6\n[/need material]"}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Costs[0].Quantity != "42" || item.Costs[1].ItemID != "3" {
		t.Fatalf("draft costs=%#v", item.Costs)
	}
}

func TestShopSearchExactIDPagination(t *testing.T) {
	for _, disk := range []bool{false, true} {
		c, _, _ := editFixture(t, editableShop)
		if _, err := c.archive.AddFileText("stackable/12.stk", "[name]\n`other`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
		list, _ := c.archive.Find("stackable/stackable.lst")
		text, _ := c.archive.Text(list)
		if err := c.archive.SetText(list, text+"\n12 `12.stk`"); err != nil {
			t.Fatal(err)
		}
		if err := c.setArchive(c.archive); err != nil {
			t.Fatal(err)
		}
		if disk {
			index, _, err := openSQLiteArchiveIndex(c.archive)
			if err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			c.diskIndex = index
			c.installDiskArchiveIndexesLocked(c.archive)
			c.mu.Unlock()
		}
		c.startSearchIndex()
		waitForSearchIndex(t, c)
		ids := []string{}
		cursor := 0
		for page := 0; page < 10; page++ {
			result, err := NewArchiveService(c).SearchItems("2", cursor, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Hits) > 1 {
				t.Fatal("limit exceeded")
			}
			for _, hit := range result.Hits {
				ids = append(ids, hit.ID)
			}
			if result.NextCursor < 0 {
				break
			}
			if result.NextCursor <= cursor {
				t.Fatal("cursor did not advance")
			}
			cursor = result.NextCursor
		}
		if len(ids) != 2 || ids[0] != "2" || ids[1] != "12" {
			t.Fatalf("disk=%v ids=%v", disk, ids)
		}
	}
}
