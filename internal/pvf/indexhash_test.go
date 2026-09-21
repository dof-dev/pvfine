package pvf

import (
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

// buildIndexHash encodes `id -> value` pairs the way the shipped files do: an
// integer token for the id and a string token for the decimal value.
func buildIndexHash(a *Archive, pairs [][2]uint32) []byte {
	var raw []byte
	for _, pair := range pairs {
		raw = append(raw, encodeBatchTokens([]batchToken{
			{typ: 0, value: int32(pair[0])},
			{typ: 6, value: a.UnicodeStringOffset(strconv.FormatUint(uint64(pair[1]), 10))},
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

func TestIndexHashValue(t *testing.T) {
	for _, test := range []struct {
		id   uint32
		want uint32
	}{
		{0, 0x00000000},
		{1, 0x31251ba7},
		{2, 0x66a79298},
		{3, 0xdfb6d245},
		{4, 0xcd4f2531},
		{10, 0x46a636a4},
		{100, 0x5c663f0c},
		{1000, 0xf0d473eb},
	} {
		if got := IndexHashValue(test.id); got != test.want {
			t.Errorf("IndexHashValue(%d) = %#x, want %#x", test.id, got, test.want)
		}
	}
}

func TestSetIndexHashEntryForID(t *testing.T) {
	a := New()
	a.AddFile("list/equipment_indexhash.etc", buildIndexHash(a, nil), TypeScript)
	if err := a.SetIndexHashEntryForID("list/equipment_indexhash.etc", 10020); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.IndexHashPairs("list/equipment_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].ID != 10020 || pairs[0].Value != IndexHashValue(10020) {
		t.Fatalf("generated pair = %#v", pairs)
	}
}

func TestSetIndexHashEntryForIDUsesUTF16Pool(t *testing.T) {
	a := New()
	// A previously damaged 110US archive may already have an ASCII value in
	// sTrA. New index-hash values must still go to sTrW.
	a.strA = []byte("old-value\x00")
	a.AddFile("list/equipment_indexhash.etc", buildIndexHash(a, nil), TypeScript)
	if err := a.SetIndexHashEntryForID("list/equipment_indexhash.etc", 10020); err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(mustFind(a, "list/equipment_indexhash.etc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 10 || raw[5] != 6 {
		t.Fatalf("raw index-hash entry = % X", raw)
	}
	offset := int32(binary.LittleEndian.Uint32(raw[6:10]))
	if offset&1 == 0 || a.ResolveString(offset) != strconv.FormatUint(uint64(IndexHashValue(10020)), 10) {
		t.Fatalf("value offset = %d, resolved = %q", offset, a.ResolveString(offset))
	}
}

func TestIndexHashIDsNeedingUpdateDetectsWrongPool(t *testing.T) {
	a := New()
	a.strA = []byte("old-value\x00")
	badRaw := encodeBatchTokens([]batchToken{
		{typ: 0, value: 10020},
		{typ: 6, value: a.StringOffset(strconv.FormatUint(uint64(IndexHashValue(10020)), 10))},
	})
	a.AddFile("list/equipment_indexhash.etc", badRaw, TypeScript)
	ids, err := a.IndexHashIDsNeedingUpdate("list/equipment_indexhash.etc", []uint32{10020, 10021})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 10020 || ids[1] != 10021 {
		t.Fatalf("ids needing update = %v", ids)
	}
	if err := a.SetIndexHashEntriesForIDs("list/equipment_indexhash.etc", ids); err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(mustFind(a, "list/equipment_indexhash.etc"))
	if err != nil {
		t.Fatal(err)
	}
	for pos := 0; pos < len(raw); pos += 10 {
		if offset := int32(binary.LittleEndian.Uint32(raw[pos+6 : pos+10])); offset&1 == 0 {
			t.Fatalf("entry %d kept even value offset %d", pos/10, offset)
		}
	}
}

func TestSetIndexHashEntriesForIDs(t *testing.T) {
	a := New()
	a.AddFile("list/equipment_indexhash.etc", buildIndexHash(a, [][2]uint32{{10018, 1}}), TypeScript)
	if err := a.SetIndexHashEntriesForIDs("list/equipment_indexhash.etc", []uint32{10018, 10019, 10019}); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.IndexHashPairs("list/equipment_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].ID != 10018 || pairs[0].Value != IndexHashValue(10018) ||
		pairs[1].ID != 10019 || pairs[1].Value != IndexHashValue(10019) {
		t.Fatalf("batch generated pairs = %#v", pairs)
	}
}

func TestSetIndexHashEntriesForListIDsPreservesLiveListOrder(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("list/equipment.lst", "100 `equipment/a.equ` 200 `equipment/b.equ`", TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("list/equipment_indexhash.etc", buildIndexHash(a, [][2]uint32{{100, 1}, {999, 2}}), TypeScript)
	if err := a.SetIndexHashEntriesForListIDs("list/equipment_indexhash.etc", "list/equipment.lst", []uint32{200}); err != nil {
		t.Fatal(err)
	}
	pairs, err := a.IndexHashPairs("list/equipment_indexhash.etc")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 3 || pairs[0].ID != 100 || pairs[1].ID != 200 || pairs[2].ID != 999 {
		t.Fatalf("list-ordered pairs = %#v", pairs)
	}
}

// TestIndexHashRealArchive checks the reader against the retail container: every
// listed id has an entry, and the file is a superset of the list.
func TestIndexHashRealArchive(t *testing.T) {
	a, _ := openPaged110Fixture(t)
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

// TestIndexHashGeneratedValuesRealArchive confirms the recovered generator
// against the active entries of the primary retail 110US lists. A few newer
// lists also contain historical/special rows with a different value policy.
func TestIndexHashGeneratedValuesRealArchive(t *testing.T) {
	a, _ := openPaged110Fixture(t)

	for _, path := range []string{
		"list/equipment_indexhash.etc",
		"list/stackable_indexhash.etc",
		"list/monster_indexhash.etc",
		"list/npc_indexhash.etc",
		"list/appendage_indexhash.etc",
	} {
		pairs, err := a.IndexHashPairs(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		records, err := a.indexHashRecords(mustFind(a, path))
		if err != nil {
			t.Fatalf("%s records: %v", path, err)
		}
		liveIDs := make(map[uint32]bool)
		listPath := strings.TrimSuffix(path, "_indexhash.etc") + ".lst"
		listIndex, ok := a.FindList(listPath)
		if !ok {
			t.Fatalf("%s: paired list not found", path)
		}
		listPairs, err := a.ScriptListPairs(listIndex)
		if err != nil {
			t.Fatalf("%s: %v", listPath, err)
		}
		for _, listPair := range listPairs {
			if id, parseErr := strconv.ParseUint(strings.TrimSpace(listPair.ID), 10, 32); parseErr == nil {
				liveIDs[uint32(id)] = true
			}
		}
		for position, pair := range pairs {
			// equipment_indexhash.etc has six old rows that are no longer in
			// equipment.lst and do not follow the current generator.
			if path == "list/equipment_indexhash.etc" && !liveIDs[pair.ID] {
				continue
			}
			if got := IndexHashValue(pair.ID); got != pair.Value {
				t.Fatalf("%s id %d: generated value %#x, archive value %#x", path, pair.ID, got, pair.Value)
			}
			if records[position].valToken.typ != 6 || records[position].valToken.value&1 == 0 {
				t.Fatalf("%s id %d: value token offset %d is not in sTrW", path, pair.ID, records[position].valToken.value)
			}
		}
		t.Logf("%s: verified %d entries", path, len(pairs))
	}
}
