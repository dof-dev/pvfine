package pvf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPaged110ItemNames resolves the `<table::key>` placeholders the newer
// clients use instead of storing display text in the item file.
func TestPaged110ItemNames(t *testing.T) {
	archive := filepath.Join("..", "..", "testdata", "110US.pvf")
	if _, err := os.Stat(archive); err != nil {
		t.Skip("testdata/110US.pvf not present")
	}
	a, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}

	// Unit level: the placeholder parser and the table map.
	idx, key, ok := parsePlaceholder("<31::equip_name_1>")
	if !ok || idx != 31 || key != "equip_name_1" {
		t.Fatalf("parsePlaceholder = %d, %q, %v", idx, key, ok)
	}
	if _, _, ok := parsePlaceholder("not a placeholder"); ok {
		t.Error("parsePlaceholder accepted plain text")
	}

	if entries := a.stringTableMap("list/n_string.lst"); len(entries) < 30 {
		t.Errorf("n_string.lst yielded %d entries", len(entries))
	} else {
		found := false
		for _, e := range entries {
			if e.index == 13 && strings.Contains(strings.ToLower(e.path), "stackable") {
				found = true
			}
		}
		if !found {
			t.Errorf("table 13 is not mapped to a stackable .str file: %v", entries[:3])
		}
	}

	// End to end: names of a few known items come out as text, not placeholders.
	// A few files reference keys that the shipped tables simply do not contain
	// (e.g. aradadventure/equipment/equipment_1.equ -> <31::equip_name_1>), so
	// the check is a ratio rather than all-or-nothing.
	resolved := 0
	checked := 0
	for i := int32(0); i < a.FileCount() && checked < 40; i++ {
		p := strings.ToLower(a.Path(i))
		if !strings.HasSuffix(p, ".stk") && !strings.HasSuffix(p, ".equ") {
			continue
		}
		text, err := a.Text(i)
		if err != nil || !strings.Contains(text, "<") || !strings.Contains(text, "::") {
			continue
		}
		checked++
		name, ok := a.ItemName(i)
		if !ok {
			t.Errorf("%s: no [name] resolved", a.Path(i))
			continue
		}
		if strings.Contains(name, "::") || strings.Contains(name, "<") {
			t.Logf("%s: name not resolvable (missing table entry): %q", a.Path(i), name)
			continue
		}
		resolved++
		if resolved <= 5 {
			t.Logf("%s -> %q", a.Path(i), name)
		}
	}
	if checked < 5 {
		t.Fatalf("only %d placeholder-bearing item files inspected", checked)
	}
	if resolved*10 < checked*9 {
		t.Errorf("only %d of %d item names resolved", resolved, checked)
	}

	// The base localization must win over the Korean overlay. These two items
	// exist in both `String/equipment.uv.str` (Chinese) and
	// `String/equipment.kor.str` (Korean).
	for _, tc := range []struct{ path, want string }{
		{"equipment/character/archer/avatar/belt/117530002.equ", "稀有克隆装扮腰部"},
		{"equipment/character/archer/avatar/belt/117530004.equ", "混沌极武碎空腰带"},
	} {
		i, ok := a.Find(tc.path)
		if !ok {
			t.Logf("%s absent from this archive", tc.path)
			continue
		}
		name, ok := a.ItemName(i)
		if !ok || name != tc.want {
			t.Errorf("%s -> %q, want %q", tc.path, name, tc.want)
		}
	}
}

// TestStringTablePrecedence locks in the table ranking: the base localization
// (`list/n_string.lst` -> `*.uv.str`) answers first, language overlays only fill
// the keys whose base value is empty or absent. Ranking the overlays first made
// the 110US item names come out Korean although the base table has Chinese.
func TestStringTablePrecedence(t *testing.T) {
	a := New()
	encode := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str",
		encode("name_1>稀有克隆装扮腰部\r\nname_2>\r\n"), TypeScript)
	a.AddFile("String/Equipment.kor.str",
		encode("name_1>레어 허리 클론 아바타\r\nname_2>포니 비즈 뱅글\r\n"), TypeScript)

	if got, ok := a.LookupStringTable(3, "name_1"); !ok || got != "稀有克隆装扮腰部" {
		t.Errorf("base value lost: %q, %v", got, ok)
	}
	if got, ok := a.LookupStringTable(3, "name_2"); !ok || got != "포니 비즈 뱅글" {
		t.Errorf("empty base value did not fall through to the overlay: %q, %v", got, ok)
	}
	if got, ok := a.LookupStringTable(3, "name_3"); ok {
		t.Errorf("unknown key resolved to %q", got)
	}

	// Provenance: the base answer is not a fallback, the overlay answer is.
	res, ok := a.ResolveStringTable(3, "name_1")
	if !ok || res.Fallback || res.Source != "String/Equipment.uv.str" {
		t.Errorf("base resolution = %#v", res)
	}
	res, ok = a.ResolveStringTable(3, "name_2")
	if !ok || !res.Fallback || res.Source != "String/Equipment.kor.str" {
		t.Errorf("overlay resolution = %#v", res)
	}
	if got := a.ResolvePlaceholdersMarked("<3::name_1> / <3::name_2>", "（未翻译）"); got != "稀有克隆装扮腰部 / 포니 비즈 뱅글（未翻译）" {
		t.Errorf("marked resolution = %q", got)
	}
	if got := a.ResolvePlaceholders("<3::name_2>"); got != "포니 비즈 뱅글" {
		t.Errorf("plain resolution = %q", got)
	}
}

// TestSetStringTableEntry edits entries at the byte level: only the value of the
// addressed line changes, the terminator and every other byte stay as they are.
func TestSetStringTableEntry(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", utf16LEBytes("name_1>旧名字\r\nname_2>别的\r\n"), TypeScript)
	a.AddFile("String/Equipment.kor.str", utf16LEBytes("name_9>韩文\r\n"), TypeScript)

	path, err := a.SetStringTableEntry(3, "name_1", "新名字")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(path, "String/Equipment.uv.str") {
		t.Errorf("edited %s", path)
	}
	if got, ok := a.LookupStringTable(3, "name_1"); !ok || got != "新名字" {
		t.Errorf("edited value = %q, %v", got, ok)
	}
	// Byte-exact: only the value bytes differ, the CRLF terminator survives.
	raw, err := a.RawBytes(mustFind(a, "String/Equipment.uv.str"))
	if err != nil {
		t.Fatal(err)
	}
	if want := utf16LEBytes("name_1>新名字\r\nname_2>别的\r\n"); !bytes.Equal(raw, want) {
		t.Errorf("payload = % x, want % x", raw, want)
	}

	// A key that only the overlay knows is edited there.
	path, err = a.SetStringTableEntry(3, "name_9", "改过的韩文")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(path, "String/Equipment.kor.str") {
		t.Errorf("overlay edit went to %s", path)
	}
	if got, ok := a.LookupStringTable(3, "name_9"); !ok || got != "改过的韩文" {
		t.Errorf("overlay value = %q, %v", got, ok)
	}

	// A brand new key is appended to the base table with its terminator.
	if _, err := a.SetStringTableEntry(3, "name_new", "新增"); err != nil {
		t.Fatal(err)
	}
	if got, ok := a.LookupStringTable(3, "name_new"); !ok || got != "新增" {
		t.Errorf("appended value = %q, %v", got, ok)
	}
	raw, _ = a.RawBytes(mustFind(a, "String/Equipment.uv.str"))
	if want := utf16LEBytes("name_1>新名字\r\nname_2>别的\r\nname_new>新增\r\n"); !bytes.Equal(raw, want) {
		t.Errorf("payload after append = % x", raw)
	}

	// Unreadable payloads are refused, never overwritten.
	if _, err := a.SetStringTableEntry(9, "whatever", "x"); err == nil {
		t.Error("editing an unmapped table index succeeded")
	}
	if _, err := a.SetStringTableEntry(3, "", "x"); err == nil {
		t.Error("empty key accepted")
	}
}

// TestStringTableUnresolvedKept ensures unknown placeholders are preserved.
func TestStringTableUnresolvedKept(t *testing.T) {
	a := New()
	if got := a.ResolvePlaceholders("plain text"); got != "plain text" {
		t.Errorf("plain text rewritten: %q", got)
	}
	in := "keep <99::missing_key> as is"
	if got := a.ResolvePlaceholders(in); got != in {
		t.Errorf("unresolved placeholder rewritten: %q", got)
	}
}
