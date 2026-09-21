package pvf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRewindStepsInvertsForwardStepping pins the rewind affine map against a
// direct simulation: rewinding m dword steps from the state at step m must
// recover the seed.
func TestRewindStepsInvertsForwardStepping(t *testing.T) {
	for _, magic := range []uint32{magicMain, magicAlt} {
		for _, seed := range []uint32{0, 1, 0x4A454634, 0xFFFFFFFF, 0x1FBB7078} {
			for _, steps := range []int{0, 1, 2, 7, 100, 9606} {
				s := seed
				for i := 0; i < steps; i++ {
					t1 := lcgMul*s + magic
					s = lcgMul*t1 + magic
				}
				if got := rewindSteps(steps, magic).apply(s); got != seed {
					t.Errorf("magic %#x seed %#x steps %d: rewind gave %#x", magic, seed, steps, got)
				}
			}
		}
	}
}

// TestRecoverZlibSeedFindsNonStandardSeed encrypts a zlib stream with a
// deliberately non-standard seed and checks that recovery finds it back.
func TestRecoverZlibSeedFindsNonStandardSeed(t *testing.T) {
	payload := bytes.Repeat([]byte("variant payload; "), 4096)
	comp, err := zlibCompress(payload)
	if err != nil {
		t.Fatal(err)
	}
	const wantSeed, wantMagic = 0xDD4FF706, magicMain
	enc := append([]byte(nil), comp...)
	cryptSeed(wantSeed, wantMagic, enc)

	got, ok := recoverZlibSeed(enc, len(payload))
	if !ok {
		t.Fatal("recoverZlibSeed did not find the seed")
	}
	if got.seed != wantSeed || got.magic != wantMagic {
		t.Errorf("recovered %#x/%#x, want %#x/%#x", got.seed, got.magic, wantSeed, wantMagic)
	}
}

// TestRecoverGRPISeedFindsNonStandardSeed builds a GRPI table whose cumulative
// compressed sizes end at bodySize and checks the seed is recovered.
func TestRecoverGRPISeedFindsNonStandardSeed(t *testing.T) {
	const count = 128
	var bodySize int32
	plain := make([]byte, count*8)
	for i := 0; i < count; i++ {
		bodySize += int32(1000 + i*7)
		binary.LittleEndian.PutUint32(plain[i*8:], uint32(bodySize))
		binary.LittleEndian.PutUint32(plain[i*8+4:], uint32(2000+i))
	}
	const wantSeed = 0x1FBB7078
	enc := append([]byte(nil), plain...)
	cryptSeed(wantSeed, magicMain, enc)

	got, ok := recoverGRPISeed(enc, count, bodySize)
	if !ok {
		t.Fatal("recoverGRPISeed did not find the seed")
	}
	if got.seed != wantSeed || got.magic != magicMain {
		t.Errorf("recovered %#x/%#x, want %#x/%#x", got.seed, got.magic, wantSeed, magicMain)
	}
	// A corrupted table (non-monotonic sizes) must be rejected.
	bad := append([]byte(nil), plain...)
	binary.LittleEndian.PutUint32(bad[8:], 1)
	cryptSeed(wantSeed, magicMain, bad)
	if _, ok := recoverGRPISeed(bad, count, bodySize); ok {
		t.Error("recoverGRPISeed accepted a non-monotonic table")
	}
}

// TestParseAcceptsStandardArchive confirms the standard path is untouched.
func TestParseAcceptsStandardArchive(t *testing.T) {
	a := New()
	a.AddFileText("equip/a.equ", "[name]\n`x`", TypeScript)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b.keys.header != (sectionKey{keySeed(keyHead), magicMain}) {
		t.Errorf("standard archive did not take the standard key path: %+v", b.keys.header)
	}
}

// TestVariantKeysAreFixedConstants pins the alternate variant's seeds. They are
// properties of the variant rather than of one archive: two revisions of the
// same source, one of them edited by third-party tooling, share all six values.
func TestVariantKeysAreFixedConstants(t *testing.T) {
	k := variantKeys()
	want := []struct {
		name string
		got  sectionKey
		want sectionKey
	}{
		{"header", k.header, sectionKey{0x4A454634, magicMain}},
		{"hash", k.hash, sectionKey{wideSeed(keyHashVariant), magicMain}},
		{"grpi", k.grpi, sectionKey{0x1FBB7078, magicMain}},
		{"body", k.body, sectionKey{0xDD4FF706, magicMain}},
		{"strA", k.strA, sectionKey{0x712A98D4, magicAlt}},
		{"strW", k.strW, sectionKey{0x712AE776, magicAlt}},
	}
	for _, w := range want {
		if w.got != w.want {
			t.Errorf("%s key = %+v, want %+v", w.name, w.got, w.want)
		}
	}
	if k.hash.seed == 0 {
		t.Error("variantKeys must pin the HASH seed")
	}
}

// TestVariantHashRegeneratedOnSave checks that a rebuilt variant archive
// re-encrypts the HASH section instead of copying it: the regenerated table
// indexes the current file list, so an added entry is registered, which the
// carried-over original could not do.
func TestVariantHashRegeneratedOnSave(t *testing.T) {
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping", testFileEnv)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.keys.hash.seed == 0 {
		t.Skip("archive HASH seed unknown; nothing to regenerate")
	}
	origHashKey := a.keys.hash
	origGRPIKey := a.keys.grpi
	origBodyKey := a.keys.body
	origHash := append([]byte(nil), a.data[a.hashOff:a.hashOff+a.hashSize]...)

	// Force a rebuild: an entry edit plus a newly added file.
	target, ok := a.Find("equipment/character/common/amulet/100300001.equ")
	if !ok {
		t.Skip("sample entry not present in this fixture")
	}
	if err := a.SetText(target, scriptText); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("zz_test/carry.txt", "[a]\n`b`", TypeScript); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "rebuilt.pvf")
	if err := a.SaveAs(out); err != nil {
		t.Fatal(err)
	}

	b, err := Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if b.keys.hash != origHashKey {
		t.Errorf("rebuilt HASH key changed: before=%#x/%#x after=%#x/%#x",
			origHashKey.seed, origHashKey.magic, b.keys.hash.seed, b.keys.hash.magic)
	}
	gotHash := b.data[b.hashOff : b.hashOff+b.hashSize]
	if bytes.Equal(gotHash, origHash) {
		t.Error("HASH section was carried over instead of regenerated")
	}
	// The regenerated section must decrypt into a table that covers the new
	// file list, and the added entry must be indexed.
	entries, sorted := parseHashTable(decryptHashSection(b))
	if len(entries) != int(b.FileCount()) {
		t.Errorf("hash entries = %d, file count = %d", len(entries), b.FileCount())
	}
	if len(sorted) == 0 {
		t.Fatal("hash lookup list is empty")
	}
	added := mustFind(b, "zz_test/carry.txt")
	if !hashEntryHasName(entries, b.items[added].nameOff) {
		t.Error("added entry is missing from the regenerated hash table")
	}

	// The rebuild must still be functional.
	if text, err := b.Text(mustFind(b, "equipment/character/common/amulet/100300001.equ")); err != nil || text != scriptTextDecoded {
		t.Errorf("edited script unreadable after rebuild: err=%v text=%q", err, text)
	}
	if _, ok := b.Find("zz_test/carry.txt"); !ok {
		t.Error("added entry missing after rebuild")
	}
	if b.keys.body != origBodyKey || b.keys.grpi != origGRPIKey {
		t.Errorf("variant seeds changed after rebuild: body=%#x/%#x grpi=%#x/%#x",
			b.keys.body.seed, b.keys.body.magic, b.keys.grpi.seed, b.keys.grpi.magic)
	}
}

// decryptHashSection returns the plaintext HASH section of the archive.
func decryptHashSection(a *Archive) []byte {
	cipher := a.data[a.hashOff : a.hashOff+a.hashSize]
	plain := make([]byte, len(cipher))
	copy(plain, cipher)
	cryptSeed(a.keys.hash.seed, a.keys.hash.magic, plain)
	return plain
}

func hashEntryHasName(entries []hashEntry, nameOff int32) bool {
	for _, entry := range entries {
		if entry.nameOff == nameOff {
			return true
		}
	}
	return false
}

// TestRealVariantParse opens the variant archive supplied via PVF_TESTFILE and
// verifies the recovered seeds actually decode names, scripts and chunk data.
// The header seed, GRPI seed, Body seed and both string-pool seeds of this
// variant are all non-standard, so this exercises the full recovery path.
func TestRealVariantParse(t *testing.T) {
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping", testFileEnv)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Skipf("stat %s: %v", p, err)
	}
	// The small synthetic archives used by other tests are opened too; only the
	// large real archive is expected to take the recovery path.
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.FileCount() <= 0 {
		t.Fatalf("FileCount = %d", a.FileCount())
	}
	if int64(len(a.data)) != st.Size() {
		t.Fatalf("data size %d != file size %d", len(a.data), st.Size())
	}

	// The layout equation must hold exactly for a correctly recovered header.
	hdr := a.Header()
	declared := int64(headerSize) + int64(hdr.FileCount)*0x18 +
		int64(hdr.HashTableSize) + int64(hdr.NameTableSize) +
		int64(hdr.GroupCount)*8 + int64(hdr.BodySize)
	if declared != st.Size() {
		t.Errorf("declared layout %d != file size %d", declared, st.Size())
	}

	// Names must resolve: every entry's path should be non-empty for a sample.
	named := 0
	sample := int(a.FileCount())
	if sample > 5000 {
		sample = 5000
	}
	for i := 0; i < sample; i++ {
		if a.Path(int32(i)) != "" {
			named++
		}
	}
	if named < sample/2 {
		t.Errorf("only %d/%d sampled entries resolved a path; string pools did not decode", named, sample)
	}

	// Chunk decompression must work for a spread of chunks.
	okChunks := 0
	for ci := int32(0); ci < int32(hdr.GroupCount); ci += 1 + int32(hdr.GroupCount)/50 {
		raw, err := a.Chunk(ci)
		if err != nil {
			t.Errorf("chunk %d: %v", ci, err)
			continue
		}
		if int64(len(raw)) != int64(a.groups[ci].origSize) {
			t.Errorf("chunk %d size %d != GRPI origSize %d", ci, len(raw), a.groups[ci].origSize)
			continue
		}
		okChunks++
	}
	if okChunks == 0 {
		t.Fatal("no chunk decompressed")
	}
	t.Logf("verified %d chunks", okChunks)

	// At least one TypeScript entry must decompile to text containing a
	// top-level section tag, which proves string resolution and token decoding.
	texts := 0
	for i := 0; i < sample; i++ {
		if a.items[i].typ != TypeScript {
			continue
		}
		text, err := a.Text(int32(i))
		if err != nil {
			continue
		}
		if strings.Contains(text, "[") && len(text) > 4 {
			texts++
			if texts == 1 {
				t.Logf("sample script %s:\n%.200s", a.Path(int32(i)), text)
			}
		}
		if texts >= 3 {
			break
		}
	}
	if texts == 0 {
		t.Error("no sampled script entry decompiled to text")
	}
}

// TestVariantNewItemRecipe walks the new-item workflow on a real variant-family
// archive supplied via PVF_TESTFILE: add a file, register it in the equipment
// list, save and reopen. This is the case that used to be blocked by the HASH
// section — the archive's HASH key is now known (wideSeed("hash")) and the
// section is regenerated, so a structural edit no longer writes a stale table.
func TestVariantNewItemRecipe(t *testing.T) {
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping", testFileEnv)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.keys.hash.seed == 0 {
		t.Fatal("HASH seed was not established")
	}
	if a.IsPaged110() {
		t.Skip("Paged110 archive: covered by the Paged110 round-trip tests")
	}

	listIndex, ok := a.FindList("equipment/equipment.lst")
	if !ok {
		t.Skip("fixture has no equipment list")
	}
	listPath := a.Path(listIndex)
	// Entries are relative to the list's own directory in this layout.
	newPath := "equipment/character/common/amulet/zz_recipe_test.equ"
	if _, err := a.AddFileText(newPath, "[name]\n`配方测试装备`\n[rarity]\n4", TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := a.SetListPair(listIndex, "900000001", "character/common/amulet/zz_recipe_test.equ"); err != nil {
		t.Fatalf("registering the new file: %v", err)
	}

	out := filepath.Join(t.TempDir(), "recipe.pvf")
	if err := a.SaveAs(out); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	b, err := Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	index, ok := b.Find(newPath)
	if !ok {
		t.Fatalf("added entry missing after %s round trip", listPath)
	}
	if text, err := b.Text(index); err != nil || !strings.Contains(text, "配方测试装备") {
		t.Errorf("added entry unreadable: err=%v text=%q", err, text)
	}
	if name, ok := b.ItemName(index); !ok || name != "配方测试装备" {
		t.Errorf("added entry name = %q, %v", name, ok)
	}
	reopenedList, ok := b.FindList("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list missing after reopen")
	}
	pairs, err := b.ScriptListPairs(reopenedList)
	if err != nil {
		t.Fatal(err)
	}
	registered := false
	for _, pair := range pairs {
		if pair.ID == "900000001" && strings.Contains(strings.ToLower(pair.Path), "zz_recipe_test") {
			registered = true
		}
	}
	if !registered {
		t.Error("new file is not registered in the equipment list")
	}
	if b.keys.hash.seed == 0 {
		t.Error("HASH seed lost after the round trip")
	}
}
