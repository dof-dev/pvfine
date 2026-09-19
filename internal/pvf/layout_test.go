package pvf

import (
	"testing"
)

// TestFindListLayout covers the two `.lst` layouts: 90US keeps a list next to
// the files it indexes, 110US collects every list under `list/`.
func TestFindListLayout(t *testing.T) {
	a := New()
	legacy, err := a.AddFileText("equipment/equipment.lst", "1008 `character/a.equ`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/stackable.lst", "2008 `stackable/b.stk`", TypeScript); err != nil {
		t.Fatal(err)
	}

	if index, ok := a.FindList("equipment/equipment.lst"); !ok || index != legacy {
		t.Errorf("90US path resolved to %d, %v", index, ok)
	}
	// A 90US-configured path also finds the 110US location.
	if index, ok := a.FindList("stackable/stackable.lst"); !ok {
		t.Error("stackable/stackable.lst did not fall back to list/stackable.lst")
	} else if got := a.Path(index); got != "list/stackable.lst" {
		t.Errorf("stackable fallback = %s", got)
	}
	// ... and a 110US-configured path finds the 90US location.
	if index, ok := a.FindList("list/equipment.lst"); !ok || index != legacy {
		t.Errorf("list/equipment.lst resolved to %d, %v", index, ok)
	}
	if _, ok := a.FindList("equipment/missing.lst"); ok {
		t.Error("missing list resolved")
	}
	if _, ok := a.FindList(""); ok {
		t.Error("empty list path resolved")
	}
}

// TestScriptMetadataPaged110Name checks that the type 8/10 pool references the
// newer clients use for [name] are read and that `<table::key>` placeholders
// are resolved into display text.
func TestScriptMetadataPaged110Name(t *testing.T) {
	a := New()
	strTable := make([]byte, 0, 64)
	for _, r := range "name_1>白色兽语腰带 [A款]\r\nname_2>\r\n" {
		strTable = append(strTable, byte(r), byte(r>>8))
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", strTable, TypeScript)
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	korTable := make([]byte, 0, 64)
	for _, r := range "name_2>포니 비즈 뱅글[A타입]\r\n" {
		korTable = append(korTable, byte(r), byte(r>>8))
	}
	a.AddFile("String/Equipment.kor.str", korTable, TypeScript)

	named, err := a.AddFileText("equipment/a.equ", "[name]\n{8=`<3::name_1>`}\n[rarity]\n4", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := a.AddFileText("equipment/b.equ", "[name]\n{10=`<3::name_2>`}", TypeScript)
	if err != nil {
		t.Fatal(err)
	}

	metadata, err := a.ScriptMetadata(named)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.HasName || metadata.Name != "白色兽语腰带 [A款]" {
		t.Errorf("metadata = %#v", metadata)
	}
	// The name is display text: a value only the overlay can answer still comes
	// out as text (without the UI marker, which is a services concern).
	metadata, err = a.ScriptMetadata(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.HasName || metadata.Name != "포니 비즈 뱅글[A타입]" {
		t.Errorf("fallback metadata = %#v", metadata)
	}
}
