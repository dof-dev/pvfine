package services

import (
	"strings"
	"testing"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

const vendingTestText = "# 😀 [/item group]\r\n[name]\r\n`抽奖机 [/output]`\r\n[item group]\r\n[group num]\r\n1\r\n[material]\r\n2 1\r\n[output]\r\n1 3000 2 0\r\n1 20 4 1\r\n[/output]\r\n[unknown]\r\n`保留`\r\n[/item group]\r\n"

func TestVendingMachineGroupsAndSource(t *testing.T) {
	text := vendingTestText + "[item group][group num]2[material]3 2[output][/output][/item group]"
	doc, err := parseVendingMachine(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Groups) != 2 || len(doc.Groups[0].Rewards) != 2 || len(doc.Groups[1].Rewards) != 0 {
		t.Fatalf("groups: %+v", doc.Groups)
	}
	units := utf16.Encode([]rune(strings.ReplaceAll(text, "\r\n", "\n")))
	g := doc.Groups[0]
	if string(utf16.Decode(units[g.Start:g.End])) != "[item group]\n[group num]\n1\n[material]\n2 1\n[output]\n1 3000 2 0\n1 20 4 1\n[/output]\n[unknown]\n`保留`\n[/item group]" {
		t.Fatal("incorrect group span")
	}
	if string(utf16.Decode(units[g.OutputClose:g.OutputClose+9])) != "[/output]" {
		t.Fatal("incorrect insertion offset")
	}
	for _, row := range g.Rewards {
		for _, f := range row.Fields {
			if string(utf16.Decode(units[f.Start:f.End])) == "" {
				t.Fatal("empty numeric span")
			}
		}
	}
	if g.Rewards[1].Fields[2].Value != 4 || g.Rewards[1].Fields[3].Value != 1 {
		t.Fatal("reward columns")
	}
}

func TestVendingMachineRejectsInvalidStructure(t *testing.T) {
	good := "[item group][group num]1[material]2 1[output]1 10 1 0[/output][/item group]"
	cases := []string{
		strings.ReplaceAll(good, "[/item group]", ""),
		strings.ReplaceAll(good, "[/output]", ""),
		strings.ReplaceAll(good, "1 10 1 0", "1 10 1"),
		strings.ReplaceAll(good, "1 10 1 0", "1 10 1 2"),
		strings.ReplaceAll(good, "1 10 1 0", "1 -1 1 0"),
		strings.ReplaceAll(good, "1 10 1 0", "1 2147483648 1 0"),
		strings.ReplaceAll(good, "1 10 1 0", "1 1.0 1 0"),
		strings.ReplaceAll(good, "[material]2 1", "[material]2 1[material]2 1"),
		strings.ReplaceAll(good, "[material]2 1", "[material]2"),
		strings.ReplaceAll(good, "[group num]1", ""),
		good + good,
		"[material]2 1",
		"[item group]" + good + "[/item group]",
	}
	for _, text := range cases {
		if _, err := parseVendingMachine(text); err == nil {
			t.Errorf("accepted invalid: %s", text)
		}
	}
	if doc, err := parseVendingMachine("# empty\n[name]`新抽奖机`"); err != nil || len(doc.Groups) != 0 {
		t.Fatalf("empty: %+v %v", doc, err)
	}
}

func TestVendingMachineListAndRead(t *testing.T) {
	c, _ := shopFixture(t)
	a := c.archive
	i, err := a.AddFileText("etc/vending/a.vm", vendingTestText, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.AddFileText("etc/vendingmachine.lst", "10 `vending/a.vm` 11 `etc/vending/a.vm` 12 `missing.vm` 13 `../stackable/one.stk`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	service := NewFileGUIService(c)
	before := a.ModifiedCount()
	entries, err := service.ListVendingMachines()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != "10" || entries[1].ID != "11" || entries[0].FileIndex != i {
		t.Fatalf("entries: %+v", entries)
	}
	doc, err := service.ReadVendingMachine(i, vendingTestText)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Groups[0].MaterialName != "材料甲" || doc.Groups[0].Rewards[0].Name != "长名称药剂" {
		t.Fatalf("names: %+v", doc.Groups[0])
	}
	if a.ModifiedCount() != before {
		t.Fatal("read mutated archive")
	}
	other, _ := a.Find("stackable/one.stk")
	if _, err := service.ReadVendingMachine(other, vendingTestText); err == nil {
		t.Fatal("accepted non vm")
	}
	if _, err := service.ReadVendingMachine(-1, vendingTestText); err == nil {
		t.Fatal("accepted bad index")
	}
}

func TestVendingMachineNoArchiveOrList(t *testing.T) {
	service := NewFileGUIService(NewCore())
	if _, err := service.ListVendingMachines(); err == nil {
		t.Fatal("accepted no archive")
	}
	if _, err := service.ReadVendingMachine(0, ""); err == nil {
		t.Fatal("accepted no archive")
	}
	service.c.archive = pvf.New()
	if _, err := service.ListVendingMachines(); err == nil {
		t.Fatal("accepted missing list")
	}
}
