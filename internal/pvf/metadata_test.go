package pvf

import (
	"bytes"
	"os"
	"strings"
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

func TestSetListPairsUpdatesInPlaceAndAppends(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/common/amulet/1008.equ` 1009 `character/common/amulet/1009.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Rewriting an existing id must keep it in place; a new id appends.
	if err := a.SetListPairs(index, []ListPair{
		{ID: "1008", Path: "character/common/amulet/9001.equ"},
		{ID: "1010", Path: "character/common/amulet/1010.equ"},
	}); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	want := []ListPair{
		{ID: "1008", Path: "character/common/amulet/9001.equ"},
		{ID: "1009", Path: "character/common/amulet/1009.equ"},
		{ID: "1010", Path: "character/common/amulet/1010.equ"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %#v, want %#v", pairs, want)
	}
	for position := range want {
		if pairs[position] != want[position] {
			t.Fatalf("pair %d = %#v, want %#v", position, pairs[position], want[position])
		}
	}
	// The untouched record must survive, and the rewrite must be visible in
	// the decompiled text.
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "1009") || !strings.Contains(text, "9001") {
		t.Fatalf("text = %q", text)
	}
}

func TestSetListPairsCollapsesDuplicateIDs(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `a/1.equ` 1009 `a/2.equ` 1008 `a/3.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetListPair(index, "1008", "a/final.equ"); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("pairs = %#v, want 2 entries with the duplicate collapsed", pairs)
	}
	if pairs[0].ID != "1008" || pairs[0].Path != "a/final.equ" {
		t.Fatalf("first pair = %#v", pairs[0])
	}
	if pairs[1].ID != "1009" || pairs[1].Path != "a/2.equ" {
		t.Fatalf("second pair = %#v", pairs[1])
	}
}

func TestRemoveListIDsRemovesAllMatching(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `a/1.equ` 1009 `a/2.equ` 1008 `a/3.equ` 1010 `a/4.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveListIDs(index, []string{"1008"})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].ID != "1009" || pairs[1].ID != "1010" {
		t.Fatalf("remaining pairs = %#v", pairs)
	}
	// Removing an absent id is a no-op.
	removed, err = a.RemoveListIDs(index, []string{"9999"})
	if err != nil || removed != 0 {
		t.Fatalf("removed = %d err = %v, want 0", removed, err)
	}
}

func TestListIDResolvesRegisteredPath(t *testing.T) {
	a := New()
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `a/1.equ` 1009 `a/2.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, ok, err := a.ListID(index, "a/2.equ")
	if err != nil || !ok || id != "1009" {
		t.Fatalf("ListID = %q, %v, err = %v", id, ok, err)
	}
	if _, ok, err = a.ListID(index, "missing.equ"); err != nil || ok {
		t.Fatalf("ListID matched an unregistered path: ok = %v err = %v", ok, err)
	}
}

func TestSetListPairsRoundTrip(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/equipment.lst", "1008 `a/1.equ`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetListPair(index, "1010", "b/2.equ"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reparsedIndex, ok := reparsed.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("list file missing after round trip")
	}
	pairs, err := reparsed.ListPairs(reparsedIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[1].ID != "1010" || pairs[1].Path != "b/2.equ" {
		t.Fatalf("round-trip pairs = %#v", pairs)
	}
}

func TestSetListPairsRejectsEmptyFields(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/equipment.lst", "1008 `a/1.equ`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetListPair(index, "  ", "a/2.equ"); err == nil {
		t.Fatal("expected an empty id to be rejected")
	}
	if err := a.SetListPair(index, "1009", "  "); err == nil {
		t.Fatal("expected an empty path to be rejected")
	}
	pairs, err := a.ListPairs(index)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("rejected writes changed the list: %#v err=%v", pairs, err)
	}
}

func TestSetListPairsPreservesUntouchedRecordBytes(t *testing.T) {
	a := New()
	// Two records: only the first is rewritten, so the second must keep its
	// exact original tokens including string-pool offsets.
	index, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `a/1.equ` 1009 `a/2.equ`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	// Capture the untouched trailing 10 bytes (the 1009 record).
	untouched := append([]byte(nil), before[10:]...)

	if err := a.SetListPair(index, "1008", "a/rewritten.equ"); err != nil {
		t.Fatal(err)
	}
	after, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("payload length = %d, want %d (rewrite must stay in place)", len(after), len(before))
	}
	if !bytes.Equal(after[10:], untouched) {
		t.Fatalf("untouched record changed:\nbefore=%v\nafter =%v", untouched, after[10:])
	}
	// The id token of the rewritten record must also be preserved.
	if !bytes.Equal(after[:5], before[:5]) {
		t.Fatalf("id token changed: before=%v after=%v", before[:5], after[:5])
	}
}

func TestSetListPairsSupportsNonNumericIDs(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/equipment.lst", "1008 `a/1.equ`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	// A non-numeric id cannot use the integer token, so it must round-trip
	// through the quoted-string form instead.
	if err := a.SetListPair(index, "booster", "b/2.equ"); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[1].ID != "booster" || pairs[1].Path != "b/2.equ" {
		t.Fatalf("pairs = %#v", pairs)
	}
	if id, ok, err := a.ListID(index, "b/2.equ"); err != nil || !ok || id != "booster" {
		t.Fatalf("ListID = %q, %v, err = %v", id, ok, err)
	}
	if removed, err := a.RemoveListIDs(index, []string{"booster"}); err != nil || removed != 1 {
		t.Fatalf("removed = %d err = %v", removed, err)
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

func TestScriptNameIgnoresNestedNameSection(t *testing.T) {
	a := New()

	// A nested [name] is a sub-record name and must not shadow the file name
	// even when it appears first in the document.
	topLevelLast, err := a.AddFileText(
		"npc/nested-first.npc",
		"[info]\n[name]\n`嵌套名`\n[/info]\n[name]\n`顶层名`",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok, err := a.ScriptName(topLevelLast); err != nil || !ok || name != "顶层名" {
		t.Fatalf("nested-first name = %q, ok = %v, err = %v", name, ok, err)
	}

	nestedOnly, err := a.AddFileText(
		"npc/nested-only.npc",
		"[info]\n[name]\n`嵌套名`\n[/info]\n[grade]\n1",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok, err := a.ScriptName(nestedOnly); err != nil || ok || name != "" {
		t.Fatalf("nested-only name = %q, ok = %v, err = %v", name, ok, err)
	}

	// Sibling unpaired sections are still top level, so a legacy [name] keeps
	// resolving without a closing tag.
	unpaired, err := a.AddFileText(
		"npc/unpaired.npc",
		"[name]\n`平级名`\n[grade]\n1",
		TypeScript,
	)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok, err := a.ScriptName(unpaired); err != nil || !ok || name != "平级名" {
		t.Fatalf("unpaired name = %q, ok = %v, err = %v", name, ok, err)
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

func TestScriptMetadataRarity(t *testing.T) {
	a := New()
	for _, tc := range []struct {
		name string
		text string
		want int32
	}{
		{
			name: "equipment/rarity.equ",
			text: "[name]\n`史诗项链`\n[rarity]\n4\n[usable job]\n`[all]`",
			want: 4,
		},
		{
			name: "stackable/rarity.stk",
			text: "[name]\n`魔法药剂`\n[rarity]\n1\n[price]\n100",
			want: 1,
		},
		{
			name: "equipment/no-rarity.equ",
			text: "[name]\n`无稀有度`\n[usable job]\n`[all]`",
			want: RarityUnknown,
		},
		{
			name: "equipment/quoted.equ",
			text: "[name]\n`引号`\n[rarity]\n`3`",
			want: 3,
		},
		{
			name: "equipment/broken.equ",
			text: "[name]\n`非法`\n[rarity]\n`极高`",
			want: RarityUnknown,
		},
		{
			name: "equipment/first-wins.equ",
			text: "[name]\n`取首个`\n[rarity]\n2\n[random option]\n[rarity]\n5",
			want: 2,
		},
	} {
		index, err := a.AddFileText(tc.name, tc.text, TypeScript)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := a.ScriptMetadata(index)
		if err != nil {
			t.Fatal(err)
		}
		if got := metadata.RarityValue(); got != tc.want {
			t.Fatalf("%s rarity = %d, want %d", tc.name, got, tc.want)
		}
		if tc.want == RarityUnknown && metadata.HasRarity {
			t.Fatalf("%s reported HasRarity for an absent value", tc.name)
		}
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

func TestCharacterMetadataUsesFirstGrowtypeName(t *testing.T) {
	a := New()
	for _, tc := range []struct{ text, want string }{
		{"[name]\n`generic`\n[growtype name]\n`鬼剑士` `剑魂`", "鬼剑士"},
		{"[growtype name]\n`鬼剑士` `剑魂`\n[name]\n`generic`", "鬼剑士"},
		{"[name]\n`fallback`", "fallback"},
		{"[growtype name]\n`` `not the first`\n[name]\n`fallback`", "fallback"},
	} {
		i, err := a.AddFileText("character/test.chr", tc.text, TypeScript)
		if err != nil {
			t.Fatal(err)
		}
		m, err := a.ScriptMetadata(i)
		if err != nil {
			t.Fatal(err)
		}
		if m.Name != tc.want {
			t.Fatalf("name=%q want=%q", m.Name, tc.want)
		}
		if err := a.SetText(i, "[growtype name]\n`新职业` `转职`"); err != nil {
			t.Fatal(err)
		}
		m, err = a.ScriptMetadata(i)
		if err != nil || m.Name != "新职业" {
			t.Fatalf("updated metadata=%#v, err=%v", m, err)
		}
	}
}
