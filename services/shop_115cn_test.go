package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

const structuredShopList = "[sell item list]\n[item]\n7\n[index]\n1\n[purchase amount]\n3\n[need material]\n2 9\n[/need material]\n[/item]\n[item]\n9\n[index]\n1\n[/item]\n[/sell item list]\n"

func TestShopStructuredLayouts(t *testing.T) {
	for name, text := range map[string]string{
		"tab":               "[sell info]\n[tab]\n`商品`\n" + structuredShopList + "[/tab]\n[/sell info]",
		"implicit":          "[sell info]\n" + structuredShopList + "[/sell info]",
		"root-tab":          "[sell info]\n[tab]\n`商品`\n" + structuredShopList + "[/tab]",
		"category":          "[sell info]\n[use category]\n`expert job non filter`\n[tab]\n`商品`\n[category entry]\n[id]\n1\n" + structuredShopList + "[/category entry]\n[/tab]\n[/sell info]",
		"implicit-category": "[sell info]\n[use category]\n`expert job non filter`\n[category entry]\n[id]\n1\n" + structuredShopList + "[/category entry]\n[/sell info]",
	} {
		t.Run(name, func(t *testing.T) {
			c, s, r := editFixture(t, text)
			doc, err := s.ReadShop(r.FileIndex, r.Text)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Issues) != 0 || len(doc.Tabs) != 1 || len(doc.Tabs[0].Groups) != 1 || len(doc.Tabs[0].Groups[0].Items) != 2 {
				t.Fatalf("unexpected document: %#v", doc)
			}
			entries := doc.Tabs[0].Groups[0].Items
			if doc.Tabs[0].Implicit != strings.HasPrefix(name, "implicit") || !entries[0].HasLocalCosts || entries[1].HasLocalCosts {
				t.Fatal("frontend capability flags do not match shop structure")
			}
			if entries[0].Item.Costs[1].Quantity != "9" || len(entries[0].Item.Costs) != 2 || len(entries[1].Item.Costs) != 3 {
				t.Fatal("local override leaked into duplicate product or was not applied")
			}
			itemIndex, _ := c.archive.Find("stackable/one.stk")
			before, _ := c.archive.RawBytes(itemIndex)
			before = bytes.Clone(before)
			r.Action, r.SourceStart, r.ItemID = "edit-item", entries[0].SourceStart, "4"
			r.SetGold, r.Gold = true, "123"
			if _, err := s.ApplyShopEdit(r); err == nil {
				t.Fatal("local override cost mutation accepted")
			}
			after, _ := c.archive.RawBytes(itemIndex)
			if !bytes.Equal(before, after) || c.batchRevision != r.Revision {
				t.Fatal("rejected mutation changed archive")
			}
			r.SetGold = false
			if _, err := s.ApplyShopEdit(r); err != nil {
				t.Fatal(err)
			}
			r.Text, _ = c.archive.Text(r.FileIndex)
			parsed := parseShop(r.Text)
			got := parsed.Tabs[0].Groups[0].Items
			if got[0].Item.ID != "4" || got[1].Item.ID != "1" || !strings.Contains(r.Text, "[purchase amount]") || got[0].localCosts[0].Quantity != "9" {
				t.Fatal("replacement lost record metadata or changed duplicate")
			}
			r.Revision = c.batchRevision
			r.Action, r.ItemID, r.CategoryID = "add-item", "2", "1"
			if _, err := s.ApplyShopEdit(r); err != nil {
				t.Fatal(err)
			}
			r.Text, _ = c.archive.Text(r.FileIndex)
			r.Revision = c.batchRevision
			got = parseShop(r.Text).Tabs[0].Groups[0].Items
			if len(got) != 3 || got[2].Item.ID != "2" || got[2].localMaterials {
				t.Fatal("new record inherited overrides")
			}
			raw, _ := c.archive.RawBytes(r.FileIndex)
			tree, err := c.archive.ParseScriptDocument(raw)
			if err != nil || len(tree.Warnings()) != 0 {
				t.Fatalf("invalid binary tree: %v", err)
			}
			loc := parsed.Tabs[0].location
			list := shopItemLists(tree.Sections(loc.path)[loc.occurrence])[0]
			if list.Children()[2].Values()[0].Value != int64(10) {
				t.Fatal("entry key not allocated above existing keys")
			}
			r.Action, r.SourceStart = "delete-item", got[0].SourceStart
			if _, err := s.ApplyShopEdit(r); err != nil {
				t.Fatal(err)
			}
			r.Text, _ = c.archive.Text(r.FileIndex)
			got = parseShop(r.Text).Tabs[0].Groups[0].Items
			if len(got) != 2 || got[0].Item.ID != "1" || strings.Contains(r.Text, "[purchase amount]") {
				t.Fatal("delete did not remove exactly the selected item block")
			}
		})
	}
}

func TestShopStructuredPageOperations(t *testing.T) {
	for _, root := range []bool{false, true} {
		text := "[sell info]\n[tab]\n`one`\n" + structuredShopList + "[/tab]"
		if !root {
			text += "\n[/sell info]"
		}
		c, s, r := editFixture(t, text)
		r.Action, r.Name = "add-tab", "two"
		if _, err := s.ApplyShopEdit(r); err != nil {
			t.Fatal(err)
		}
		r.Text, _ = c.archive.Text(r.FileIndex)
		r.Revision, r.TabIndex = c.batchRevision, 1
		r.Action, r.ItemID = "add-item", "2"
		if _, err := s.ApplyShopEdit(r); err != nil {
			t.Fatal(err)
		}
		r.Text, _ = c.archive.Text(r.FileIndex)
		if !strings.Contains(r.Text, "[sell item list]") || strings.Contains(r.Text, "[item list]") {
			t.Fatal("new page used legacy list")
		}
		r.Revision, r.Action, r.Name = c.batchRevision, "rename-tab", "three"
		if _, err := s.ApplyShopEdit(r); err != nil {
			t.Fatal(err)
		}
		r.Text, _ = c.archive.Text(r.FileIndex)
		if parseShop(r.Text).Tabs[1].Name != "three" {
			t.Fatal("rename failed")
		}
		r.Revision, r.Action = c.batchRevision, "delete-tab"
		if _, err := s.ApplyShopEdit(r); err != nil {
			t.Fatal(err)
		}
		text, _ = c.archive.Text(r.FileIndex)
		if len(parseShop(text).Tabs) != 1 {
			t.Fatal("page deletion failed")
		}
	}
}

func TestShopImplicitPageAndMalformedGuards(t *testing.T) {
	for _, text := range []string{
		"[sell info]\n" + structuredShopList + "[/sell info]",
		"[sell info]\n[tab]\n`bad`\n[sell info]\n" + structuredShopList + "[/sell info]",
	} {
		c, s, r := editFixture(t, text)
		before, _ := c.archive.RawBytes(r.FileIndex)
		before = bytes.Clone(before)
		for _, action := range []string{"add-tab", "rename-tab", "delete-tab"} {
			r.Action, r.Name = action, "test"
			if _, err := s.ApplyShopEdit(r); err == nil {
				t.Fatalf("%s accepted", action)
			}
		}
		after, _ := c.archive.RawBytes(r.FileIndex)
		if !bytes.Equal(before, after) {
			t.Fatal("guarded operation changed source")
		}
	}
}

func TestShopStructuredEmptySlotsAndGlobalEntryKeys(t *testing.T) {
	text := "[sell info]\n[use category]\n`expert job non filter`\n[tab]\n`shop`\n[category entry]\n[id]\n1\n" +
		strings.Replace(structuredShopList, "[index]\n1", "[index]\n-1", 1) +
		"[/category entry]\n[category entry]\n[id]\n2\n[sell item list]\n[item]\n99\n[index]\n4\n[price]\n0\n[/item]\n[/sell item list]\n[/category entry]\n[/tab]\n[/sell info]"
	c, s, r := editFixture(t, text)
	doc, err := s.ReadShop(r.FileIndex, r.Text)
	if err != nil || len(doc.Issues) != 0 {
		t.Fatalf("document=%#v err=%v", doc, err)
	}
	if len(doc.Tabs[0].Groups[0].Items) != 1 {
		t.Fatal("empty slot shown as product")
	}
	costs := doc.Tabs[0].Groups[1].Items[0].Item.Costs
	if len(costs) == 0 || costs[len(costs)-1].Kind != "gold" || costs[len(costs)-1].Quantity != "0" {
		t.Fatal("explicit local zero lost")
	}
	r.Action, r.SetGold, r.Gold = "batch-costs", true, "42"
	before, _ := c.archive.RawBytes(r.FileIndex)
	before = bytes.Clone(before)
	if _, err := s.ApplyShopEdit(r); err == nil {
		t.Fatal("batch ignored local cost protection")
	}
	after, _ := c.archive.RawBytes(r.FileIndex)
	if !bytes.Equal(before, after) || r.Revision != c.batchRevision {
		t.Fatal("rejected batch changed the shop")
	}
	r.Action, r.SetGold, r.CategoryID, r.ItemID = "add-item", false, "3", "2"
	if _, err := s.ApplyShopEdit(r); err != nil {
		t.Fatal(err)
	}
	r.Text, _ = c.archive.Text(r.FileIndex)
	parsed := parseShop(r.Text)
	if len(parsed.Tabs[0].Groups) != 3 || parsed.Tabs[0].Groups[2].Items[0].Item.ID != "2" {
		t.Fatal("new category item missing")
	}
	raw, _ := c.archive.RawBytes(r.FileIndex)
	tree, err := c.archive.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	lists := shopItemLists(tree.Sections([]string{"sell info", "tab"})[0])
	if len(lists) != 3 || lists[2].Name() != "sell item list" || lists[2].Children()[0].Values()[0].Value != int64(100) {
		t.Fatal("new category schema or unique entry key lost")
	}
	r.Action, r.SourceStart, r.Revision = "delete-item", parsed.Tabs[0].Groups[0].Items[0].SourceStart, c.batchRevision
	if _, err := s.ApplyShopEdit(r); err != nil {
		t.Fatal(err)
	}
	raw, _ = c.archive.RawBytes(r.FileIndex)
	tree, err = c.archive.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	list := shopItemLists(tree.Sections([]string{"sell info", "tab"})[0])[0]
	if len(list.Children()) != 1 || list.Children()[0].Children()[0].Values()[0].Value != int64(-1) {
		t.Fatal("visible item deletion removed empty slot")
	}
}

func TestShopUnclassifiedProductsRemainAccessible(t *testing.T) {
	text := "[sell info]\n[use category]\n`expert job non filter`\n" + structuredShopList + "[/sell info]"
	_, s, r := editFixture(t, text)
	doc, err := s.ReadShop(r.FileIndex, r.Text)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Categories) == 0 || doc.Categories[0].ID != "" || doc.Categories[0].Name != "未分类" || len(doc.Issues) == 0 {
		t.Fatal("unclassified products must remain selectable with a diagnostic")
	}
}

func TestShopEmptyListFormat(t *testing.T) {
	if shopListName(nil, "115CN") != "sell item list" || shopListName(nil, "90CN") != "item list" {
		t.Fatal("empty shop default list schema incorrect")
	}
}

// Opt-in corpus regression: compare every visible ID and its binary edit
// target, not merely whether the shop has tabs. Never change the source PVF.
func TestShopReal115CNStructure(t *testing.T) {
	filename := os.Getenv("PVF_SHOP_TESTFILE")
	if filename == "" {
		t.Skip("PVF_SHOP_TESTFILE required")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	total, empty, issues, records, unsupported := 0, 0, 0, 0, 0
	layouts := map[string]int{}
	issueSections := map[string]int{}
	categoryWarnings := map[string]int{}
	emptyUnclassified := 0
	for i := int32(0); i < a.FileCount(); i++ {
		if !strings.HasSuffix(strings.ToLower(a.Path(i)), ".shp") {
			continue
		}
		total++
		text, err := a.Text(i)
		if err != nil {
			t.Fatal(err)
		}
		doc := parseShop(text)
		if len(doc.Tabs) == 0 {
			empty++
		}
		if len(doc.Issues) > 0 {
			issues++
			for _, issue := range doc.Issues {
				issueSections[issue.Section]++
				if issue.Section == "category entry" {
					categoryWarnings[issue.Message]++
				}
			}
		}
		raw, err := a.RawBytes(i)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := a.ParseScriptDocument(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Warnings()) > 0 {
			unsupported++
			continue
		}
		expected := 0
		for _, e := range pvf.ParseScriptView(text).Elements {
			p := strings.Join(e.SectionPath, "/")
			if e.Kind == pvf.ScriptElementToken && (strings.HasSuffix(p, "/sell item list/item/index") || strings.HasSuffix(p, "/item list")) {
				if strings.HasSuffix(p, "/sell item list/item/index") && e.Value == "-1" {
					continue
				}
				expected++
			}
		}
		actual := 0
		for _, tab := range doc.Tabs {
			loc := tab.location
			target := tree.Sections(loc.path)[loc.occurrence]
			binaryRecords := shopRecords(target)
			var entries []ShopEntry
			for _, group := range tab.Groups {
				if doc.CategoryType != "" && group.CategoryID == "" && len(group.Items) == 0 {
					emptyUnclassified++
				}
				entries = append(entries, group.Items...)
			}
			if len(binaryRecords) != len(entries) {
				t.Fatalf("shop=%d location=%v occurrence=%d binary=%d visible=%d issues=%v", i, loc.path, loc.occurrence, len(binaryRecords), len(entries), doc.Issues)
			}
			for n, entry := range entries {
				v := binaryRecords[n].index.Values()[binaryRecords[n].offset]
				if strconv.FormatInt(v.Value.(int64), 10) != entry.Item.ID {
					t.Fatal("view and edit target IDs differ")
				}
			}
			for _, list := range shopItemLists(target) {
				layouts[strings.Join(list.Path(), "/")]++
			}
			actual += len(entries)
		}
		if actual != expected {
			t.Fatalf("shop %d lost records: expected=%d actual=%d", i, expected, actual)
		}
		records += actual
	}
	t.Logf("shops=%d without-tabs=%d with-issues=%d malformed=%d visible-records=%d layouts=%v issue-sections=%v", total, empty, issues, unsupported, records, layouts, issueSections)
	t.Logf("category-warnings=%v empty-unclassified-groups=%d", categoryWarnings, emptyUnclassified)
	if total == 0 || records == 0 {
		t.Fatal("no shop records tested")
	}
}

func TestShopReal115CNEditRoundTrip(t *testing.T) {
	filename := os.Getenv("PVF_SHOP_TESTFILE")
	if filename == "" {
		t.Skip("PVF_SHOP_TESTFILE required")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	c := newCore()
	c.archive = a
	service := NewFileGUIService(c)
	type sample struct {
		index         int32
		tab           int
		category, id  string
		before, after []byte
	}
	samples := map[string]*sample{}
	for i := int32(0); i < a.FileCount() && len(samples) < 5; i++ {
		if !strings.HasSuffix(strings.ToLower(a.Path(i)), ".shp") {
			continue
		}
		text, err := a.Text(i)
		if err != nil {
			t.Fatal(err)
		}
		doc := parseShop(text)
		if len(doc.Issues) != 0 {
			continue
		}
		for ti, tab := range doc.Tabs {
			for _, group := range tab.Groups {
				if len(group.Items) == 0 {
					continue
				}
				layout := strings.Join(tab.location.path, "/")
				if tab.Implicit {
					layout += "/implicit"
				}
				if group.CategoryID != "" {
					layout += "/category"
				}
				if samples[layout] != nil {
					continue
				}
				entry := group.Items[0]
				if _, err := service.ReadItem(entry.Item.ID, nil); err != nil {
					continue
				}
				raw, err := a.RawBytes(i)
				if err != nil {
					t.Fatal(err)
				}
				tree, err := a.ParseScriptDocument(raw)
				if err != nil || len(tree.Warnings()) != 0 {
					continue
				}
				samples[layout] = &sample{index: i, tab: ti, category: group.CategoryID, id: entry.Item.ID, before: bytes.Clone(raw)}
			}
		}
	}
	if len(samples) != 5 {
		t.Fatalf("expected five layouts, got %d", len(samples))
	}
	for layout, sample := range samples {
		text, err := a.Text(sample.index)
		if err != nil {
			t.Fatal(err)
		}
		req := ShopEditRequest{FileIndex: sample.index, Path: a.Path(sample.index), Text: text, Revision: c.batchRevision, Action: "add-item", TabIndex: sample.tab, CategoryID: sample.category, ItemID: sample.id}
		result, _, _, err := service.applyShopEditLocked(req)
		if err != nil {
			t.Fatalf("layout=%s: %v", layout, err)
		}
		if len(result.Files) != 1 || result.Files[0].FileIndex != sample.index {
			t.Fatal("add modified unrelated files")
		}
		raw, err := a.RawBytes(sample.index)
		if err != nil {
			t.Fatal(err)
		}
		sample.after = bytes.Clone(raw)
	}
	output := filepath.Join(t.TempDir(), "shop-115cn-roundtrip.pvf")
	if err := a.SaveAs(output); err != nil {
		t.Fatal(err)
	}
	reopened, err := pvf.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Release()
	fresh := newCore()
	fresh.archive = reopened
	freshService := NewFileGUIService(fresh)
	for layout, sample := range samples {
		raw, err := reopened.RawBytes(sample.index)
		if err != nil || !bytes.Equal(raw, sample.after) {
			t.Fatalf("layout=%s did not survive save/reopen: %v", layout, err)
		}
		text, err := reopened.Text(sample.index)
		if err != nil {
			t.Fatal(err)
		}
		doc := parseShop(text)
		var last *ShopEntry
		for _, group := range doc.Tabs[sample.tab].Groups {
			if group.CategoryID == sample.category && len(group.Items) > 0 {
				entry := group.Items[len(group.Items)-1]
				last = &entry
				break
			}
		}
		if last == nil || last.Item.ID != sample.id {
			t.Fatalf("layout=%s added item missing", layout)
		}
		req := ShopEditRequest{FileIndex: sample.index, Path: reopened.Path(sample.index), Text: text, Revision: fresh.batchRevision, Action: "delete-item", TabIndex: sample.tab, SourceStart: last.SourceStart}
		if _, _, _, err := freshService.applyShopEditLocked(req); err != nil {
			t.Fatal(err)
		}
		raw, err = reopened.RawBytes(sample.index)
		if err != nil || !bytes.Equal(raw, sample.before) {
			t.Fatalf("layout=%s deletion did not restore original binary payload: %v", layout, err)
		}
	}
	t.Log("five layouts: add, save/reopen, delete; original shop payloads restored byte-for-byte; source PVF untouched")
}
