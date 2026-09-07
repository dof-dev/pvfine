package pvf

import (
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

func TestScriptListPairsMalformedPayload(t *testing.T) {
	a := New()
	index := a.AddFile("equipment/equipment.lst", []byte{6, 1, 2}, TypeScript)
	if _, err := a.ScriptListPairs(index); err == nil {
		t.Fatal("expected malformed payload error")
	}
}
