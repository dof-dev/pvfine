package pvf

import (
	"os"
	"testing"
)

func TestScriptListPairs(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/equipment.lst", "1008 `character/common/amulet/1008.equ` 1009 `character/common/amulet/1009.equ`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}

	pairs, err := a.ScriptListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("pair count = %d, want 2", len(pairs))
	}
	if pairs[0].ID != "1008" || pairs[0].Path != "character/common/amulet/1008.equ" {
		t.Fatalf("first pair = %#v", pairs[0])
	}
	if pairs[1].ID != "1009" || pairs[1].Path != "character/common/amulet/1009.equ" {
		t.Fatalf("second pair = %#v", pairs[1])
	}
}

func TestRemoveListPairs(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/common/amulet/1008.equ` 1009 `character/common/amulet/1009.equ` 1008 `character/common/amulet/1008.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := a.RemoveListPairs(index, []ListPair{{ID: "1008", Path: "character/common/amulet/1008.equ"}})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	pairs, err := a.ScriptListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].ID != "1009" {
		t.Fatalf("remaining pairs = %#v", pairs)
	}
}

func TestScriptName(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/character/amulet/1008.equ", "[name]\n`烈火之心项链`\n[grade]\n1", TypeScript)
	if err != nil {
		t.Fatal(err)
	}

	name, ok, err := a.ScriptName(index)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || name != "烈火之心项链" {
		t.Fatalf("name = %q, ok = %v", name, ok)
	}

	withoutName, err := a.AddFileText("equipment/character/amulet/empty.equ", "[grade]\n1", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	name, ok, err = a.ScriptName(withoutName)
	if err != nil {
		t.Fatal(err)
	}
	if ok || name != "" {
		t.Fatalf("missing name = %q, ok = %v", name, ok)
	}
}

func TestScriptMetadata(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/character/amulet/1008.equ",
		"[name]\n`烈火之心项链`\n[icon]\n`Item/new_equipment/08_necklace/necklace.img`\n69\n[field image]\n`Item/FieldImage.img`\n6",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := a.ScriptMetadata(index)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.HasName || metadata.Name != "烈火之心项链" {
		t.Fatalf("metadata name = %#v", metadata)
	}
	if metadata.Icon == nil || metadata.Icon.Path != "Item/new_equipment/08_necklace/necklace.img" || metadata.Icon.Index != 69 {
		t.Fatalf("metadata icon = %#v", metadata.Icon)
	}
	if metadata.FieldImage == nil || metadata.FieldImage.Path != "Item/FieldImage.img" || metadata.FieldImage.Index != 6 {
		t.Fatalf("metadata field image = %#v", metadata.FieldImage)
	}
}

func TestScriptListPairsMalformedPayload(t *testing.T) {
	a := New()
	index := a.AddFile("equipment/equipment.lst", []byte{6, 1, 2}, TypeScript)
	if _, err := a.ScriptListPairs(index); err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestRealScriptMetadata(t *testing.T) {
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	index, ok := a.Find("equipment/character/common/amulet/100300001.equ")
	if !ok {
		t.Skip("真实 PVF 中没有样本装备")
	}
	metadata, err := a.ScriptMetadata(index)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Icon == nil || metadata.Icon.Path == "" || metadata.Icon.Index < 0 {
		t.Fatalf("real icon = %#v", metadata.Icon)
	}
	if metadata.FieldImage == nil || metadata.FieldImage.Path == "" || metadata.FieldImage.Index < 0 {
		t.Fatalf("real field image = %#v", metadata.FieldImage)
	}
}
