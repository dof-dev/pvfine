package services

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"pvfine/internal/pvf"
)

func TestShopEditReal90CNRoundTrip(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	if a.ClientVersion() != "90CN" {
		t.Skip("需要 90CN 样例")
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	index, ok := a.Find("itemshop/101_joann_box.shp")
	if !ok {
		t.Fatal("sample missing")
	}
	untouchedIndex, _ := a.Find("itemshop/equipmentshop1.shp")
	untouched, _ := a.RawBytes(untouchedIndex)
	untouched = append([]byte(nil), untouched...)
	text, _ := a.Text(index)
	service := NewFileGUIService(c)
	doc, err := service.ReadShop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	req := ShopEditRequest{FileIndex: index, Path: a.Path(index), Text: text, Revision: doc.Revision, Action: "edit-item", TabIndex: 0, SourceStart: doc.Tabs[0].Groups[0].Items[0].SourceStart, ItemID: "10093342", SetGold: true, Gold: "77"}
	result, err := service.ApplyShopEdit(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedItems != 1 {
		t.Fatal("item count")
	}
	req.Text, _ = a.Text(index)
	req.Revision = result.Revision
	req.Action = "rename-tab"
	req.Name = "测试新分页"
	req.SetGold = false
	if _, err := service.ApplyShopEdit(req); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "shop-roundtrip.pvf")
	if err := a.SaveAs(output); err != nil {
		t.Fatal(err)
	}
	reopened, err := pvf.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewCore()
	fresh.archive = reopened
	i, _ := reopened.Find("itemshop/101_joann_box.shp")
	savedText, _ := reopened.Text(i)
	after, err := NewFileGUIService(fresh).ReadShop(i, savedText)
	if err != nil {
		t.Fatal(err)
	}
	if after.Tabs[0].Name != "测试新分页" || len(after.Tabs[0].Groups[0].Items) != 5 {
		t.Fatal("shop did not round trip")
	}
	costs := after.Tabs[0].Groups[0].Items[0].Item.Costs
	gold, material := false, false
	for _, cost := range costs {
		if cost.Kind == "gold" && cost.Quantity == "77" {
			gold = true
		}
		if cost.Kind == "material" && cost.ItemID == "10092849" && cost.Quantity == "10" {
			material = true
		}
	}
	if !gold || !material {
		t.Fatalf("dual costs missing: %#v", costs)
	}
	u, _ := reopened.Find("itemshop/equipmentshop1.shp")
	raw, _ := reopened.RawBytes(u)
	if !bytes.Equal(raw, untouched) {
		t.Fatal("unrelated shop changed")
	}
}
