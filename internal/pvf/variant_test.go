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
// same source, one of them edited by third-party tooling, share all five values.
func TestVariantKeysAreFixedConstants(t *testing.T) {
	k := variantKeys()
	want := []struct {
		name string
		got  sectionKey
		want sectionKey
	}{
		{"header", k.header, sectionKey{0x4A454634, magicMain}},
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
	if k.isStandard {
		t.Error("variantKeys must not be marked standard")
	}
}

// TestVariantHashCarriedOverOnSave checks that a rebuilt variant archive keeps
// the HASH section byte-for-byte. Its seed is unknown, so rewriting it would
// replace a table the client can read with one it cannot.
func TestVariantHashCarriedOverOnSave(t *testing.T) {
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping", testFileEnv)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.keys.isStandard {
		t.Skip("archive uses the standard key set; nothing to carry over")
	}
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
	if b.keys.isStandard {
		t.Error("rebuilt variant was written with the standard key set")
	}
	if got := b.data[b.hashOff : b.hashOff+b.hashSize]; !bytes.Equal(got, origHash) {
		t.Errorf("HASH section was rewritten (%d bytes -> %d bytes)", len(origHash), len(got))
	}
	// The rebuild must still be functional.
	if text, err := b.Text(mustFind(b, "equipment/character/common/amulet/100300001.equ")); err != nil || text != scriptTextDecoded {
		t.Errorf("edited script unreadable after rebuild: err=%v text=%q", err, text)
	}
	if _, ok := b.Find("zz_test/carry.txt"); !ok {
		t.Error("added entry missing after rebuild")
	}
	if b.keys.body.seed != 0xDD4FF706 || b.keys.grpi.seed != 0x1FBB7078 {
		t.Errorf("variant seeds lost after rebuild: body=%#x grpi=%#x", b.keys.body.seed, b.keys.grpi.seed)
	}
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
