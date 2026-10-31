package services

import (
	"bytes"
	"fmt"
	"testing"

	"pvfine/internal/pvf"
)

func TestReadItemNamesEquipmentStackableAndMissing(t *testing.T) {
	c, _ := shopFixture(t)
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	// Names must not depend on whether an item's unrelated price is well formed.
	index, _ := c.archive.Find("equipment/four.equ")
	if err := c.archive.SetText(index, "[name]\n`关联装备😀`\n[price]\n`invalid price`"); err != nil {
		t.Fatal(err)
	}
	before, err := c.archive.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	before = append([]byte(nil), before...)
	modified := c.archive.ModifiedCount()
	names, err := NewFileGUIService(c).ReadItemNames([]string{"1", "4", "1", " 2 ", "999999", ""})
	if err != nil {
		t.Fatal(err)
	}
	if names["1"] != "长名称药剂" || names["4"] != "关联装备😀" || names["2"] != "材料甲" || len(names) != 3 {
		t.Fatalf("names=%#v", names)
	}
	after, _ := c.archive.RawBytes(index)
	if !bytes.Equal(before, after) || c.archive.ModifiedCount() != modified {
		t.Fatal("name lookup modified archive")
	}
}

func TestReadItemNamesResolvesLocalizedReferences(t *testing.T) {
	c, _, _, _ := paged110LayoutFixture(t)
	names, err := NewFileGUIService(c).ReadItemNames([]string{"514530375"})
	if err != nil {
		t.Fatal(err)
	}
	if names["514530375"] != "白色兽语腰带 [A款]" {
		t.Fatalf("names=%#v", names)
	}
}

func TestReadItemNamesRefreshesAfterItemEdit(t *testing.T) {
	c, _ := shopFixture(t)
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	service := NewFileGUIService(c)
	if _, err := service.ReadItemNames([]string{"1"}); err != nil {
		t.Fatal(err)
	}
	index, _ := c.archive.Find("stackable/one.stk")
	if err := NewEditorService(c).SetText(index, "[name]\n`新的关联名称`"); err != nil {
		t.Fatal(err)
	}
	names, err := service.ReadItemNames([]string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	if names["1"] != "新的关联名称" {
		t.Fatalf("cached old name: %#v", names)
	}
}

func TestReadItemNamesRequiresArchive(t *testing.T) {
	if _, err := NewFileGUIService(NewCore()).ReadItemNames([]string{"1"}); err != ErrNoArchive {
		t.Fatalf("err=%v", err)
	}
	a := pvf.New()
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	names, err := NewFileGUIService(c).ReadItemNames(nil)
	if err != nil || len(names) != 0 {
		t.Fatalf("names=%v err=%v", names, err)
	}
}

func TestReadItemIconsMetadataAndMissing(t *testing.T) {
	c, _ := shopFixture(t)
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	index, _ := c.archive.Find("equipment/four.equ")
	if err := c.archive.SetText(index, "[name]\n`图标装备`\n[icon]\n`Item/test.img` 133\n[price]\n`invalid price`"); err != nil {
		t.Fatal(err)
	}
	modified := c.archive.ModifiedCount()
	icons, err := NewFileGUIService(c).ReadItemIcons([]string{"4", " 4 ", "999999", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(icons) != 1 || icons["4"] == nil || icons["4"].Path != "Item/test.img" || icons["4"].Index != 133 {
		t.Fatalf("icons=%#v", icons)
	}
	if c.archive.ModifiedCount() != modified {
		t.Fatal("icon lookup modified archive")
	}
	if err := c.archive.SetText(index, "[icon]\n`Item/changed.img` 2"); err != nil {
		t.Fatal(err)
	}
	icons, err = NewFileGUIService(c).ReadItemIcons([]string{"4"})
	if err != nil || icons["4"] == nil || icons["4"].Path != "Item/changed.img" {
		t.Fatalf("icons=%#v err=%v", icons, err)
	}
}

func TestReadItemIconsRequiresArchive(t *testing.T) {
	if _, err := NewFileGUIService(NewCore()).ReadItemIcons([]string{"1"}); err != ErrNoArchive {
		t.Fatalf("err=%v", err)
	}
}

func TestReadItemAppearancesRarityAndRefresh(t *testing.T) {
	c, _ := shopFixture(t)
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	index, _ := c.archive.Find("equipment/four.equ")
	service := NewFileGUIService(c)
	for rarity := int32(-1); rarity <= 8; rarity++ {
		text := "[name]\n`装备`\n[icon]\n`Item/test.img` 133\n[price]\n`invalid price`"
		if rarity >= 0 {
			text += fmt.Sprintf("\n[rarity]\n%d", rarity)
		}
		if err := NewEditorService(c).SetText(index, text); err != nil {
			t.Fatal(err)
		}
		modified := c.archive.ModifiedCount()
		appearances, err := service.ReadItemAppearances([]string{"4", " 4 ", "999999", ""})
		if err != nil {
			t.Fatal(err)
		}
		item := appearances["4"]
		if len(appearances) != 1 || item == nil || item.Rarity != rarity || item.Icon == nil || item.Icon.Index != 133 {
			t.Fatalf("rarity=%d appearances=%#v item=%#v", rarity, appearances, item)
		}
		if c.archive.ModifiedCount() != modified {
			t.Fatal("appearance lookup modified archive")
		}
	}
}

func TestReadItemAppearancesRequiresArchive(t *testing.T) {
	if _, err := NewFileGUIService(NewCore()).ReadItemAppearances([]string{"1"}); err != ErrNoArchive {
		t.Fatalf("err=%v", err)
	}
}
