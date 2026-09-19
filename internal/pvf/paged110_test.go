package pvf

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// testdataFile returns the path of name under the repository testdata dir.
func testdataFile(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

func openPaged110Fixture(t *testing.T) (*Archive, string) {
	t.Helper()
	archive := os.Getenv(testFileEnv)
	if archive == "" {
		t.Skipf("%s not set; skipping Paged110 integration test", testFileEnv)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Skipf("stat %s: %v", archive, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(archive), sealedPageKeyName)); err != nil {
		t.Skipf("stat %s: %v", filepath.Join(filepath.Dir(archive), sealedPageKeyName), err)
	}
	a, err := Open(archive)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return a, archive
}

// TestPaged110Open parses the retail 110US container end to end: page guards
// unlocked from sk.dat, header, file table, name pools and body chunks.
func TestPaged110Open(t *testing.T) {
	a, archive := openPaged110Fixture(t)
	if !a.paged110 {
		t.Fatalf("archive was not recognised as Paged110")
	}
	h := a.Header()
	if h.Signature != MagicSignature {
		t.Fatalf("signature = %#x, want %#x", h.Signature, MagicSignature)
	}
	if got, want := a.FileCount(), int32(4311296); got != want {
		t.Errorf("FileCount() = %d, want %d", got, want)
	}
	if got, want := h.GroupCount, int32(58372); got != want {
		t.Errorf("GroupCount = %d, want %d", got, want)
	}
	if got, want := h.BodySize, int32(364769260); got != want {
		t.Errorf("BodySize = %d, want %d", got, want)
	}
	if got, want := h.NameTableSize, int32(33525673); got != want {
		t.Errorf("NameTableSize = %d, want %d", got, want)
	}

	// Name pools must resolve: every entry needs a non-empty path.
	missing := 0
	for i := int32(0); i < 200; i++ {
		if a.Path(i) == "" {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d of the first 200 paths did not resolve", missing)
	}

	// Known files parse to the expected text.
	if idx, ok := a.Find("appendage/creature/creature.lst"); !ok {
		t.Error("creature.lst not found")
	} else if text, err := a.Text(idx); err != nil {
		t.Errorf("read creature.lst: %v", err)
	} else if !bytes.Contains([]byte(text), []byte("Faras/faras.cre")) {
		t.Errorf("creature.lst content unexpected: %.80q", text)
	}

	if idx, ok := a.Find("aradadventure/equipment/equipment_1.equ"); !ok {
		t.Error("equipment_1.equ not found")
	} else if text, err := a.Text(idx); err != nil {
		t.Errorf("read equipment_1.equ: %v", err)
	} else if !bytes.Contains([]byte(text), []byte("[name]")) {
		t.Errorf("equipment_1.equ is not a PVF script: %.80q", text)
	}

	// Saving an untouched container must reproduce the original file byte for
	// byte: decrypting and re-encrypting the page guards has to be an exact
	// inverse, and everything else is copied through.
	if len(a.pageKeys) == 0 {
		t.Fatal("page keys were not retained on the archive")
	}
	original, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	originalHash := md5.New()
	if _, err := io.Copy(originalHash, original); err != nil {
		t.Fatal(err)
	}
	rewrittenHash := md5.New()
	if err := a.SaveTo(rewrittenHash); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	if !bytes.Equal(originalHash.Sum(nil), rewrittenHash.Sum(nil)) {
		t.Errorf("rewritten archive differs from the source (md5 %x vs %x)",
			rewrittenHash.Sum(nil), originalHash.Sum(nil))
	}

	// The HASH seed is not part of the Paged110 key set, but it is solved from
	// the section itself, which is what lets a rebuild regenerate the table and
	// therefore allows structural edits (see the round-trip test below).
	if a.keys.hash.seed == 0 {
		t.Error("HASH seed was not recovered")
	}
}

// TestPaged110StructuralEditRoundTrip walks the whole new-item recipe on the
// retail container — add a file, give it a string-table name, register it in the
// equipment list — then writes it back and reopens it: the HASH section is
// regenerated with the recovered seed and the rebuilt name pool uses the
// container's own keys, so the new item is complete and everything else still
// reads.
func TestPaged110StructuralEditRoundTrip(t *testing.T) {
	a, archive := openPaged110Fixture(t)
	before := a.FileCount()
	const (
		newPath = "zz_probe/structural_test.equ"
		newKey  = "zz_probe_name"
		newName = "新增条目"
	)
	if _, err := a.AddFileText(newPath, "[name]\n{8=`<3::"+newKey+">`}\n[rarity]\n4", TypeScript); err != nil {
		t.Fatal(err)
	}
	// The text the new file references, created in the equipment table.
	tablePath, err := a.SetStringTableEntry(3, newKey, newName)
	if err != nil {
		t.Fatalf("creating the string entry: %v", err)
	}
	t.Logf("string entry created in %s", tablePath)
	// Register the new file so the client can find it.
	listIndex, ok := a.FindList("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list not found")
	}
	if err := a.SetListPair(listIndex, "900000001", newPath); err != nil {
		t.Fatalf("registering the new file: %v", err)
	}

	dir := t.TempDir()
	sealed, err := os.ReadFile(filepath.Join(filepath.Dir(archive), sealedPageKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sealedPageKeyName), sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "Script.pvf")
	file, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SaveTo(file); err != nil {
		file.Close()
		t.Fatalf("SaveTo: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := Open(out)
	if err != nil {
		t.Fatalf("reopening the saved archive: %v", err)
	}
	if got := b.FileCount(); got != before+1 {
		t.Errorf("file count = %d, want %d", got, before+1)
	}
	index, ok := b.Find(newPath)
	if !ok {
		t.Fatal("added entry missing after the round trip")
	}
	if text, err := b.Text(index); err != nil || !strings.Contains(text, "<3::"+newKey+">") {
		t.Errorf("added entry unreadable: err=%v text=%q", err, text)
	}
	// The new text and the registration survived; the name resolves to it.
	if got, ok := b.LookupStringTable(3, newKey); !ok || got != newName {
		t.Errorf("created string entry = %q, %v", got, ok)
	}
	if name, ok := b.ItemName(index); !ok || name != newName {
		t.Errorf("new item name = %q, %v", name, ok)
	}
	reopenedList, ok := b.FindList("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list missing after the round trip")
	}
	pairs, err := b.ScriptListPairs(reopenedList)
	if err != nil {
		t.Fatal(err)
	}
	registered := false
	for _, pair := range pairs {
		if pair.ID == "900000001" && strings.EqualFold(pair.Path, newPath) {
			registered = true
		}
	}
	if !registered {
		t.Error("new file is not registered in the equipment list")
	}
	// The regenerated HASH must index the new file list.
	entries, sorted := parseHashTable(decryptHashSection(b))
	if len(entries) != int(b.FileCount()) {
		t.Errorf("hash entries = %d, file count = %d", len(entries), b.FileCount())
	}
	if !hashEntryHasName(entries, b.items[index].nameOff) {
		t.Error("added entry is missing from the regenerated hash table")
	}
	if len(sorted) == 0 {
		t.Error("hash lookup list is empty")
	}
	// An existing item still resolves.
	itemIndex, ok := b.Find("character/demoniclancer/avatar/belt/514530375.equ")
	if !ok {
		t.Fatal("existing item missing after the round trip")
	}
	if name, ok := b.ItemName(itemIndex); !ok || name != "白色兽语腰带 [A款]" {
		t.Errorf("existing item name = %q, %v", name, ok)
	}
}

// TestPaged110Keys pins the recovered constants of the Paged110 scheme.
func TestPaged110Keys(t *testing.T) {
	if got, want := wideSeed(paged110KeyHeader), uint32(0x1AAEB306); got != want {
		t.Errorf("wideSeed(iNfO) = %#x, want %#x", got, want)
	}
	// The variant family's empirically-known seeds must come out of the same
	// formula, which is what ties the two schemes together.
	for _, tc := range []struct {
		name string
		want uint32
	}{
		{"hEAd", 0x4A454634},
		{"grpi", 0x1FBB7078},
		{"bODy", 0xDD4FF706},
		{"StRa", 0x712A98D4},
		{"StRw", 0x712AE776},
	} {
		if got := wideSeed(tc.name); got != tc.want {
			t.Errorf("wideSeed(%q) = %#x, want %#x", tc.name, got, tc.want)
		}
	}
	if keys := paged110Keys(); keys.maskA != paged110MaskStrA || keys.maskW != paged110MaskStrW {
		t.Errorf("paged110 pool masks = %d/%d", keys.maskA, keys.maskW)
	}
}

// TestPaged110SaveEditRoundTrip edits a string-table payload in place, writes
// the container back (page guards re-encrypted) and reopens it: the edit
// survives, the container is still a valid Paged110 archive, and untouched
// content still reads the same.
func TestPaged110SaveEditRoundTrip(t *testing.T) {
	a, archive := openPaged110Fixture(t)
	fileCount := a.FileCount()

	// Edit one entry of a small string table (String/AradAdventure.uv.str, the
	// table 31 used by aradadventure items).
	const tablePath = "String/AradAdventure.uv.str"
	strIndex, ok := a.Find(tablePath)
	if !ok {
		t.Fatalf("%s not found", tablePath)
	}
	raw, err := a.RawBytes(strIndex)
	if err != nil {
		t.Fatal(err)
	}
	text := decodeUTF16(raw)
	const key = "Baskervile_exp"
	if !strings.Contains(text, key+">") {
		t.Fatalf("%s does not contain %q", tablePath, key)
	}
	const newValue = "改过的文本"
	edited := replaceStringTableValue(text, key, newValue)
	if edited == text {
		t.Fatal("edited text is unchanged")
	}
	if err := a.SetRawBytes(strIndex, encodeUTF16LE(edited)); err != nil {
		t.Fatal(err)
	}

	// Write it next to a copy of the sidecar key file so it can be reopened.
	dir := t.TempDir()
	sealed, err := os.ReadFile(filepath.Join(filepath.Dir(archive), sealedPageKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sealedPageKeyName), sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "Script.pvf")
	file, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SaveTo(file); err != nil {
		file.Close()
		t.Fatalf("SaveTo: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(a.data)) {
		t.Errorf("saved size = %d, want %d", info.Size(), len(a.data))
	}

	reopened, err := Open(out)
	if err != nil {
		t.Fatalf("reopening the saved archive: %v", err)
	}
	if !reopened.paged110 {
		t.Error("saved archive is no longer recognised as Paged110")
	}
	if got := reopened.FileCount(); got != fileCount {
		t.Errorf("file count = %d, want %d", got, fileCount)
	}
	if got, ok := reopened.LookupStringTable(31, key); !ok || got != newValue {
		t.Errorf("edited entry = %q, %v; want %q", got, ok, newValue)
	}
	// An untouched table and an item that resolves through it are unaffected.
	if got, ok := reopened.LookupStringTable(3, "name_514530375"); !ok || got != "白色兽语腰带 [A款]" {
		t.Errorf("untouched equipment name = %q, %v", got, ok)
	}
	if idx, ok := reopened.Find("character/demoniclancer/avatar/belt/514530375.equ"); !ok {
		t.Error("item missing after the round trip")
	} else if name, ok := reopened.ItemName(idx); !ok || name != "白色兽语腰带 [A款]" {
		t.Errorf("item name after the round trip = %q, %v", name, ok)
	}
}

// replaceStringTableValue rewrites the value of one `key>value` line.
func replaceStringTableValue(text, key, value string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, key+">") {
			lines[i] = key + ">" + value
			return strings.Join(lines, "\n")
		}
	}
	return text
}

func encodeUTF16LE(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r > 0xFFFF {
			r1, r2 := utf16.EncodeRune(r)
			out = append(out, byte(r1), byte(r1>>8), byte(r2), byte(r2>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

// TestPaged110Unseal exercises the RSA/AES key table pipeline against the
// retail key file: 15 RSA blocks unseal to 52 page keys.
func TestPaged110Unseal(t *testing.T) {
	sealed, err := os.ReadFile(testdataFile(sealedPageKeyName))
	if err != nil {
		t.Skip("testdata/sk.dat not present")
	}
	table, err := unsealPageKeyTable(sealed)
	if err != nil {
		t.Fatalf("unseal: %v", err)
	}
	if want := 52 * paged110PageKeySize; len(table) != want {
		t.Fatalf("page key table = %d bytes, want %d", len(table), want)
	}
	keys := paged110Candidates("")
	if len(keys) == 0 {
		t.Fatal("no metadata key candidates")
	}
	unwrapped := make([]byte, len(table))
	copy(unwrapped, table)
	if !unwrapPageKeyTable(unwrapped, keys[0]) {
		t.Fatal("unwrap failed")
	}
	// Unwrapping must actually change the table (sanity check on the key).
	if bytes.Equal(unwrapped[:paged110MetadataAlign], table[:paged110MetadataAlign]) {
		t.Fatal("unwrap left the table unchanged")
	}
}

// TestPaged110CandidatesFromExecutable checks the hex-run scan against the
// executable when it is available.
func TestPaged110CandidatesFromExecutable(t *testing.T) {
	exe, err := os.ReadFile(testdataFile(executableName))
	if err != nil {
		t.Skip("testdata/DFO.exe not present")
	}
	got := metadataKeyCandidates(exe)
	if len(got) != 3 {
		t.Fatalf("found %d metadata key candidates, want 3", len(got))
	}
	want := "b2bfb46886cd87d5b6a92c4f2f71850e271d715ad3ad4119be4b90eb6585caa5"
	if fmt.Sprintf("%x", got[0]) != want {
		t.Errorf("first candidate = %x, want %s", got[0], want)
	}
	// The embedded constant must be one of the runs found in the executable.
	embedded := paged110EmbeddedMetadataKey()
	found := false
	for _, c := range got {
		if bytes.Equal(c, embedded) {
			found = true
		}
	}
	if !found {
		t.Error("embedded metadata key is not present in DFO.exe")
	}
	_ = binary.LittleEndian
}
