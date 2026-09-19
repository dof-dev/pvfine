package pvf

import (
	"os"
	"strconv"
	"testing"
)

// buildIndexHash encodes `id -> value` pairs the way the shipped files do: an
// integer token for the id and a string token for the decimal value.
func buildIndexHash(a *Archive, pairs [][2]uint32) []byte {
	var raw []byte
	for _, pair := range pairs {
		raw = append(raw, encodeBatchTokens([]batchToken{
			{typ: 0, value: int32(pair[0])},
			{typ: 6, value: a.StringOffset(strconv.FormatUint(uint64(pair[1]), 10))},
		})...)
	}
	return raw
}

func TestIndexHashReadWrite(t *testing.T) {
	a := New()
	if got, ok := IndexHashCompanionPath("list/equipment.lst"); !ok || got != "list/equipment_indexhash.etc" {
		t.Fatalf("companion path = %q, %v", got, ok)
	}
	if _, ok := IndexHashCompanionPath("list/equipment_indexhash.etc"); ok {
		t.Error("non-.lst path accepted")
	}

	a.AddFile("list/equipment_indexhash.etc", buildIndexHash(a, [][2]uint32{
		{10018, 4292442212},
		{10019, 3912882972},
	}), TypeScript)
	if _, err := a.AddFileText("list/equipment.lst",
		"10018 `equipment/a.equ` 10019 `equipment/b.equ` 10020 `equipment/c.equ`", TypeScript); err != nil {
		t.Fatal(err)
	}

	pairs, err := a.IndexHashPairs("list/equipment_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].ID != 10018 || pairs[0].Value != 4292442212 || pairs[1].Value != 3912882972 {
		t.Fatalf("pairs = %#v", pairs)
	}
	if got := a.IndexHashSiblingPaths("list/equipment.lst"); len(got) != 1 || got[0] != "list/equipment_indexhash.etc" {
		t.Errorf("siblings = %v", got)
	}

	// 10020 has no index entry.
	gaps, err := a.IndexHashGaps("list/equipment.lst")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0] != 10020 {
		t.Fatalf("gaps = %v", gaps)
	}

	// Updating an existing entry keeps its position; adding one appends.
	if err := a.SetIndexHashEntry("list/equipment_indexhash.etc", 10018, 12345); err != nil {
		t.Fatal(err)
	}
	if err := a.SetIndexHashEntry("list/equipment_indexhash.etc", 10020, 99); err != nil {
		t.Fatal(err)
	}
	pairs, err = a.IndexHashPairs("list/equipment_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 3 || pairs[0].ID != 10018 || pairs[0].Value != 12345 ||
		pairs[1].ID != 10019 || pairs[1].Value != 3912882972 || pairs[2].ID != 10020 || pairs[2].Value != 99 {
		t.Fatalf("pairs after write = %#v", pairs)
	}
	gaps, err = a.IndexHashGaps("list/equipment.lst")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Errorf("gaps after write = %v", gaps)
	}

	// The payload is a plain token stream, 10 bytes per entry.
	raw, err := a.RawBytes(mustFind(a, "list/equipment_indexhash.etc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 30 {
		t.Errorf("payload = %d bytes, want 30", len(raw))
	}
}

// TestIndexHashRealArchive checks the reader against the retail container: every
// listed id has an entry, and the file is a superset of the list.
func TestIndexHashRealArchive(t *testing.T) {
	archive := testdataFile("110US.pvf")
	if _, err := os.Stat(archive); err != nil {
		t.Skip("testdata/110US.pvf not present")
	}
	if _, err := os.Stat(testdataFile(sealedPageKeyName)); err != nil {
		t.Skip("testdata/sk.dat not present")
	}
	a, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, listPath := range []string{"list/equipment.lst", "list/stackable.lst"} {
		gaps, err := a.IndexHashGaps(listPath)
		if err != nil {
			t.Fatalf("%s: %v", listPath, err)
		}
		if len(gaps) != 0 {
			t.Errorf("%s: %d ids missing from the companion index (first: %v)", listPath, len(gaps), gaps[:min(5, len(gaps))])
		}
		siblings := a.IndexHashSiblingPaths(listPath)
		if len(siblings) == 0 {
			t.Fatalf("%s: no companion index found", listPath)
		}
		pairs, err := a.IndexHashPairs(siblings[0])
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s -> %s: %d entries", listPath, siblings[0], len(pairs))
	}

	// A newly registered id shows up as a gap until an entry is written.
	listIndex, ok := a.FindList("list/stackable.lst")
	if !ok {
		t.Fatal("stackable list not found")
	}
	if err := a.SetListPair(listIndex, "900000777", "stackable/gold.stk"); err != nil {
		t.Fatal(err)
	}
	gaps, err := a.IndexHashGaps("list/stackable.lst")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0] != 900000777 {
		t.Fatalf("gaps after registering a new id = %v", gaps)
	}
	if err := a.SetIndexHashEntry("list/stackable_indexhash.etc", 900000777, 123456789); err != nil {
		t.Fatal(err)
	}
	if gaps, err = a.IndexHashGaps("list/stackable.lst"); err != nil || len(gaps) != 0 {
		t.Fatalf("gaps after writing the entry = %v, %v", gaps, err)
	}
	got, err := a.IndexHashPairs("list/stackable_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no pairs")
	}
	last := got[len(got)-1]
	if last.ID != 900000777 || last.Value != 123456789 {
		t.Errorf("appended entry = %#v", last)
	}
}
