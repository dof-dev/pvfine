package pvf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestStringOffsetPoolConvention pins where new strings land: non-ASCII in the
// UTF-16 pool, ASCII in the UTF-8 pool when the archive has one. Writing CJK
// text into the UTF-8 pool is what made freshly typed Chinese render as mojibake
// in game.
func TestStringOffsetPoolConvention(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("equipment/a.equ", "[name]\n`a`", TypeScript); err != nil {
		t.Fatal(err)
	}
	ascii := a.StringOffset("ascii-name")
	if ascii&1 != 0 {
		t.Errorf("ASCII string went to the UTF-16 pool (offset %d)", ascii)
	}
	if got := a.ResolveString(ascii); got != "ascii-name" {
		t.Errorf("ASCII round trip = %q", got)
	}
	cjk := a.StringOffset("测试中文名称")
	if cjk&1 == 0 {
		t.Errorf("non-ASCII string went to the UTF-8 pool (offset %d)", cjk)
	}
	if got := a.ResolveString(cjk); got != "测试中文名称" {
		t.Errorf("non-ASCII round trip = %q", got)
	}
	// Existing entries keep their own offset.
	if again := a.StringOffset("测试中文名称"); again != cjk {
		t.Errorf("second lookup returned %d, want %d", again, cjk)
	}
}

func TestStringPoolEditCountsAsModified(t *testing.T) {
	source := New()
	if _, err := source.AddFileText("equipment/a.equ", "[name]\n`a`", TypeScript); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := source.SaveTo(&encoded); err != nil {
		t.Fatal(err)
	}
	a, err := Parse(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := a.ModifiedCount(); got != 0 {
		t.Fatalf("freshly parsed archive modified count = %d", got)
	}
	a.StringOffset("pool-only-edit")
	if got := a.ModifiedCount(); got != 1 {
		t.Fatalf("pool-only edit modified count = %d, want 1", got)
	}
}

func TestPaged110NewScriptStringsUseUTF16Pool(t *testing.T) {
	a := New()
	a.format = paged110Profile
	a.strA = []byte("legacy\x00")
	a.strAIdx = nil
	a.strWIdx = nil

	index, err := a.AddFileText("new/item.equ", "[name]\n{8=`<3::new_name>`}\n[icon]\n`new/icon.img`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if a.items[index].nameOff&1 == 0 || a.items[index].pathOff&1 == 0 {
		t.Fatalf("new file path strings are not in sTrW: name=%d path=%d", a.items[index].nameOff, a.items[index].pathOff)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		typ := raw[pos]
		if typ != 3 && typ != 5 && typ != 6 && typ != 7 && typ != 8 && typ != 10 {
			continue
		}
		offset := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		if offset&1 == 0 {
			t.Errorf("new script string token %d uses sTrA offset %d (%q)", pos/5, offset, a.ResolveString(offset))
		}
	}
}

func TestNormalizePaged110StringPoolsMigratesLegacyReferences(t *testing.T) {
	a := New()
	a.format = paged110Profile
	a.strA = []byte("legacy-name\x00legacy-tag\x00")
	a.strW = []byte{0, 0}
	a.strAIdx = nil
	a.strWIdx = nil
	a.resolveCache = map[int32]string{}
	index := a.AddFile("placeholder.equ", nil, TypeScript)
	a.items[index].nameOff = 0
	a.items[index].pathOff = int32(len("legacy-name\x00") << 1)
	raw := encodeBatchTokens([]batchToken{
		{typ: 3, value: 0},
		{typ: 8, value: int32(len("legacy-name\x00") << 1)},
	})
	if err := a.SetRawBytes(index, raw); err != nil {
		t.Fatal(err)
	}

	if err := a.normalizeStringPools(); err != nil {
		t.Fatal(err)
	}
	if len(a.strA) != 0 {
		t.Fatalf("sTrA was not cleared: %d bytes", len(a.strA))
	}
	if a.items[index].nameOff&1 == 0 || a.items[index].pathOff&1 == 0 {
		t.Fatalf("file table offsets were not migrated: name=%d path=%d", a.items[index].nameOff, a.items[index].pathOff)
	}
	updated, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	for pos := 0; pos+5 <= len(updated); pos += 5 {
		offset := int32(binary.LittleEndian.Uint32(updated[pos+1:]))
		if offset&1 == 0 {
			t.Errorf("payload token %d was not migrated: offset=%d", pos/5, offset)
		}
	}
	if got := a.ResolveString(a.items[index].nameOff); got != "legacy-name" {
		t.Errorf("migrated name = %q", got)
	}
}

// TestSetTextKeepsPaintedKoreanPayloads covers the Korean-server byte form:
// opening a painted `.str`, editing it and saving must re-encode it the same way
// instead of turning readable Korean into mojibake.
func TestSetTextKeepsPaintedKoreanPayloads(t *testing.T) {
	a := New()
	// The detector works on the whole payload (it needs enough painted
	// characters to be sure), so a realistic table-sized text is used.
	original := strings.Repeat("한국어 테스트 ", 8)
	edited := strings.Repeat("한국어 수정 ", 8)
	painted, err := EncodeKoreanMojibake(original)
	if err != nil {
		t.Fatal(err)
	}
	index := a.AddFile("text/painted.kor.str", painted, TypeUnicode)
	if text, err := a.Text(index); err != nil || text != original {
		t.Fatalf("read back = %q, %v", text, err)
	}
	if err := a.SetText(index, edited); err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeCP437Painting(utf16DecodeForTest(raw)) {
		t.Error("edited payload is no longer painted")
	}
	if text, err := a.Text(index); err != nil || text != edited {
		t.Errorf("edited text = %q, %v", text, err)
	}

	// A plain payload stays plain.
	plain := a.AddFile("text/plain.str", utf16le("초기 텍스트"), TypeUnicode)
	if err := a.SetText(plain, "바뀐 텍스트"); err != nil {
		t.Fatal(err)
	}
	raw, _ = a.RawBytes(plain)
	if looksLikeCP437Painting(utf16DecodeForTest(raw)) {
		t.Error("plain payload turned into a painted one")
	}
	if text, err := a.Text(plain); err != nil || text != "바뀐 텍스트" {
		t.Errorf("plain edited text = %q, %v", text, err)
	}
}

func utf16DecodeForTest(raw []byte) string {
	return string(utf16.Decode(u16le(raw)))
}

// TestVariantChineseNameEncoding edits an existing item name to Chinese on a
// real archive and checks the encoding the client expects: the new text lands in
// the UTF-16 pool, and the UTF-8 pool stays pure ASCII.
func TestVariantChineseNameEncoding(t *testing.T) {
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping", testFileEnv)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.IsPaged110() {
		t.Skip("Paged110 archive: covered elsewhere")
	}
	listIndex, ok := a.FindList("equipment/equipment.lst")
	if !ok {
		t.Skip("fixture has no equipment list")
	}
	pairs, err := a.ScriptListPairs(listIndex)
	if err != nil {
		t.Fatal(err)
	}
	var target int32 = -1
	var originalName string
	for _, pair := range pairs {
		index, ok := resolveListItem(a, listIndex, pair.Path)
		if !ok {
			continue
		}
		name, ok := a.ItemName(index)
		if !ok || name == "" || strings.Contains(name, "::") {
			continue
		}
		target, originalName = index, name
		break
	}
	if target < 0 {
		t.Skip("no item with an inline name found")
	}
	utf8NonASCIIBefore := countNonASCIIBytes(a.strA)
	text, err := a.Text(target)
	if err != nil {
		t.Fatal(err)
	}
	const newName = "测试中文名称"
	edited := strings.Replace(text, "`"+originalName+"`", "`"+newName+"`", 1)
	if edited == text {
		t.Skipf("could not locate the inline name in %s", a.Path(target))
	}
	if err := a.SetText(target, edited); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "chinese.pvf")
	if err := a.SaveAs(out); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	b, err := Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	index, ok := b.Find(a.Path(target))
	if !ok {
		t.Fatalf("%s missing after the round trip", a.Path(target))
	}
	if name, ok := b.ItemName(index); !ok || name != newName {
		t.Fatalf("name after the round trip = %q, %v", name, ok)
	}
	// The new string must live in the UTF-16 pool, like every other non-ASCII
	// string in this archive.
	raw, err := b.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		typ := raw[pos]
		if typ != 5 && typ != 6 && typ != 7 && typ != 8 && typ != 10 {
			continue
		}
		off := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		if b.ResolveString(off) != newName {
			continue
		}
		found = true
		if off&1 == 0 {
			t.Errorf("new Chinese string was stored in the UTF-8 pool (offset %d)", off)
		}
	}
	if !found {
		t.Fatalf("new name %q not found in %s", newName, a.Path(target))
	}
	// The new text must not have added non-ASCII bytes to the UTF-8 pool: this
	// archive keeps its non-ASCII strings in the UTF-16 pool (the pool does carry
	// some legacy non-ASCII bytes already, so the count is compared, not zeroed).
	if after := countNonASCIIBytes(b.strA); after > utf8NonASCIIBefore {
		t.Errorf("UTF-8 pool gained %d non-ASCII bytes", after-utf8NonASCIIBefore)
	}
}

func countNonASCIIBytes(buf []byte) int {
	count := 0
	for _, b := range buf {
		if b > 0x7F {
			count++
		}
	}
	return count
}

// resolveListItem resolves a list entry path either relative to the list's own
// directory (90US layout) or archive-root-relative (110US layout).
func resolveListItem(a *Archive, listIndex int32, entry string) (int32, bool) {
	listPath := a.Path(listIndex)
	dir := listPath
	if slash := strings.LastIndexByte(dir, '/'); slash >= 0 {
		dir = dir[:slash]
	}
	for _, candidate := range []string{dir + "/" + entry, entry} {
		if idx, ok := a.Find(candidate); ok {
			return idx, true
		}
	}
	return 0, false
}
