package services

import (
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

func shopFixture(t *testing.T) (*core, int32) {
	t.Helper()
	a := pvf.New()
	for p, text := range map[string]string{
		"stackable/stackable.lst": "1 `one.stk` 2 `two.stk` 3 `three.stk`",
		"stackable/one.stk":       "[name]\n`长名称药剂`\n[icon]\n`item/test.img` 7\n[price]\n100000\n[need material]\n2 10 3 20\n[/need material]",
		"stackable/two.stk":       "[name]\n`材料甲`\n[icon]\n`item/test.img` 8",
		"stackable/three.stk":     "[name]\n`材料乙`",
		"equipment/equipment.lst": "4 `four.equ`",
		"equipment/four.equ":      "[name]\n`佩刀`\n[price]\n3500",
		"character/character.lst": "0 `sword.chr` 1 `fighter.chr`",
		"character/sword.chr":     "[name]\n`不能用此名称`\n[growtype name]\n`鬼剑士` `剑魂`",
		"character/fighter.chr":   "[name]\n`格斗家`",
		"npc/npc.lst":             "10 `shop.npc`",
		"npc/shop.npc":            "[name]\n`商人`",
	} {
		if _, err := a.AddFileText(p, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	i, err := a.AddFileText("itemshop/test.shp", "[NPC]\n10", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	c.archive = a
	return c, i
}

func TestShopDraftCostsAndReadOnly(t *testing.T) {
	c, i := shopFixture(t)
	before, _ := c.archive.RawBytes(i)
	saved := append([]byte(nil), before...)
	modified := c.archive.ModifiedCount()
	text := "[NPC]\n10\n[sell info]\n[tab]\n`珍贵😀`\n[item list]\n1 1 2 4 999\n[/item list]\n[/tab]\n[tab]\n`空`\n[item list]\n[/item list]\n[/tab]\n[/sell info]"
	doc, err := NewFileGUIService(c).ReadShop(i, text)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "商人" || len(doc.Tabs) != 2 || len(doc.Categories) != 0 {
		t.Fatalf("document = %#v", doc)
	}
	entries := doc.Tabs[0].Groups[0].Items
	if len(entries) != 5 || entries[0].Item.ID != entries[1].Item.ID {
		t.Fatalf("entries = %#v", entries)
	}
	item := entries[0].Item
	if item.Name != "长名称药剂" || item.Icon == nil || item.Icon.Index != 7 || len(item.Costs) != 3 {
		t.Fatalf("item = %#v", item)
	}
	if item.Costs[0].Kind != "gold" || item.Costs[0].Quantity != "100000" || item.Costs[1].ItemID != "2" || item.Costs[1].Quantity != "10" || item.Costs[1].Name != "材料甲" || item.Costs[1].Icon == nil {
		t.Fatalf("costs = %#v", item.Costs)
	}
	if len(entries[2].Item.Costs) != 0 || entries[3].Item.Costs[0].Quantity != "3500" || !strings.Contains(entries[4].Item.Name, "999") || len(doc.Issues) != 1 {
		t.Fatal("missing price / equipment / unknown item handling")
	}
	runes := utf16.Encode([]rune(text))
	for _, entry := range entries {
		if string(utf16.Decode(runes[entry.SourceStart:entry.SourceEnd])) != entry.Item.ID {
			t.Fatal("source range mismatch")
		}
	}
	after, _ := c.archive.RawBytes(i)
	if string(after) != string(saved) || c.archive.ModifiedCount() != modified {
		t.Fatal("GUI read mutated archive")
	}
}

func TestShopCategoryKinds(t *testing.T) {
	c, i := shopFixture(t)
	for _, tc := range []struct{ kind, id, want string }{
		{"basic job", "0", "鬼剑士"}, {"basic job", "1", "格斗家"}, {"expert job", "1", "附魔师"}, {"expert job", "0", "炼金术师"}, {"expert job", "3", "分解师"}, {"expert job", "2", "控偶师"}, {"basic job", "99", "分类 99"},
		{"expert job non filter", "1", "附魔师"}, {"expert job non filter", "0", "炼金术师"}, {"expert job non filter", "3", "分解师"}, {"expert job non filter", "2", "控偶师"},
	} {
		t.Run(tc.kind+tc.id, func(t *testing.T) {
			text := "[sell info]\n[use category]\n`" + tc.kind + "`\n[tab]\n`分类商品`\n[category entry]\n[id]\n" + tc.id + "\n[item list]\n1 2\n[/item list]\n[/category entry]\n[/tab]\n[/sell info]"
			doc, err := NewFileGUIService(c).ReadShop(i, text)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Categories) != 1 || doc.Categories[0].Name != tc.want || doc.Tabs[0].Groups[0].CategoryID != tc.id || len(doc.Tabs[0].Groups[0].Items) != 2 {
				t.Fatalf("document = %#v", doc)
			}
			if doc.CategoryType != tc.kind || len(doc.Issues) != 0 {
				t.Fatalf("category type = %q, issues = %#v", doc.CategoryType, doc.Issues)
			}
		})
	}
}

func TestShopMalformedAndEmpty(t *testing.T) {
	for _, text := range []string{"", "[item list]\n1\n[/item list]", "[sell info]\n[tab]\n`bad`\n[item list]\n`not an id`\n[/item list]\n[/tab]\n[/sell info]"} {
		if len(parseShop(text).Issues) == 0 {
			t.Fatalf("missing issue for %q", text)
		}
	}
	doc := parseShop("")
	costs := shopCosts("[price]\n-1\n[need material]\n2 10 3\n[/need material]", "1", doc)
	if len(costs) != 1 || costs[0].ItemID != "2" || len(doc.Issues) < 3 {
		t.Fatalf("costs=%#v issues=%#v", costs, doc.Issues)
	}
	doc = parseShop("")
	if costs := shopCosts("[price]\n0", "1", doc); len(costs) != 1 || costs[0].Quantity != "0" {
		t.Fatal("explicit zero price must be preserved")
	}
}

func TestShopReal90CN(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	if a.ClientVersion() != "90CN" {
		t.Skip("商店样例使用 90CN")
	}
	c := NewCore()
	c.archive = a
	service := NewFileGUIService(c)
	before := a.ModifiedCount()
	for _, p := range []string{"itemshop/101_joann_box.shp", "itemshop/equipmentshop1.shp"} {
		i, ok := a.Find(p)
		if !ok {
			t.Fatal("sample missing")
		}
		text, err := a.Text(i)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := service.ReadShop(i, text)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(p, "joann") {
			if len(doc.Tabs) != 3 || len(doc.Categories) != 0 {
				t.Fatal("joann structure")
			}
			for ti, count := range []int{5, 6, 6} {
				if len(doc.Tabs[ti].Groups[0].Items) != count {
					t.Fatal("joann item count")
				}
			}
			cost := doc.Tabs[0].Groups[0].Items[0].Item.Costs
			if len(cost) != 1 || cost[0].ItemID != "10092849" || cost[0].Quantity != "10" {
				t.Fatalf("joann cost=%#v", cost)
			}
		} else {
			if len(doc.Tabs) != 4 || len(doc.Categories) != 16 || doc.Categories[0].Name != "鬼剑士" {
				t.Fatalf("equipment categories=%#v tabs=%d", doc.Categories, len(doc.Tabs))
			}
			if doc.Tabs[0].Groups[0].Items[0].Item.Costs[0].Quantity != "10" {
				t.Fatal("equipment gold price")
			}
		}
		if len(doc.Issues) > 0 {
			t.Fatalf("sample has issues: %#v", doc.Issues)
		}
	}
	if a.ModifiedCount() != before {
		t.Fatal("real archive mutated")
	}
}
