package pvf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/encoding/traditionalchinese"
)

func twFixture(t *testing.T) []byte {
	t.Helper()
	return twFixtureWithScript(t, nil)
}

func twFixtureWithScript(t *testing.T, nativeScript []byte) []byte {
	t.Helper()
	encode := func(s string) []byte {
		b, e := traditionalchinese.Big5.NewEncoder().Bytes([]byte(s))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	values := []string{"[name]", "測試裝備", "[rarity]", "text/test.str", "equip_name"}
	table := make([]byte, 4+(len(values)+1)*4)
	binary.LittleEndian.PutUint32(table, uint32(len(values)))
	for i, s := range values {
		binary.LittleEndian.PutUint32(table[4+i*4:], uint32(len(table)-4))
		table = append(table, encode(s)...)
	}
	binary.LittleEndian.PutUint32(table[4+len(values)*4:], uint32(len(table)-4))
	// Native TW numbering and two-byte script prefix, including reference pairs.
	script := []byte{0xb0, 0xd0, 5, 0, 0, 0, 0, 7, 1, 0, 0, 0, 5, 2, 0, 0, 0, 2, 4, 0, 0, 0}
	// Legacy writers sometimes declare padding as part of the script.
	script = append(script, 0, 0)
	if nativeScript != nil {
		script = nativeScript
	}
	reference := []byte{0xb0, 0xd0, 5, 0, 0, 0, 0, 9, 0, 0, 0, 0, 10, 4, 0, 0, 0}
	list := []byte{0xb0, 0xd0, 2, 0, 0, 0, 0, 7, 3, 0, 0, 0}
	var tree, body bytes.Buffer
	paths := []string{"stringtable.bin", "equipment/test.equ", "equipment/ref.equ", "n_string.lst", "text/test.str", "etc/raw.bin", "etc/empty.bin", "sqr/common.nut"}
	rawFiles := [][]byte{table, script, reference, list, encode("equip_name>繁體名稱\r\n"), {0, 1, 2, 3, 255}, {}, encode("// 繁體註解\r\nfunction test() { return 1; }\r\n")}
	for i, path := range paths {
		number := uint32(i + 7)
		enc, crc := twEncrypt(rawFiles[i], number)
		if len(rawFiles[i]) == 0 {
			crc = 0
		}
		binary.Write(&tree, binary.LittleEndian, number)
		binary.Write(&tree, binary.LittleEndian, uint32(len(path)))
		tree.WriteString(path)
		for _, v := range []uint32{uint32(len(rawFiles[i])), crc, uint32(body.Len())} {
			binary.Write(&tree, binary.LittleEndian, v)
		}
		body.Write(enc)
	}
	enc, crc := twEncrypt(tree.Bytes(), uint32(len(paths)))
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(36))
	out.WriteString("00000000-0000-0000-0000-000000000000")
	for _, v := range []uint32{48984, uint32(len(enc)), crc, uint32(len(paths))} {
		binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(enc)
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestTWOpaqueNumericTokenRoundTrip(t *testing.T) {
	native := []byte{0xb0, 0xd0, 5, 2, 0, 0, 0}
	for _, v := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
		native = append(native, 3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(native[len(native)-4:], v)
	}
	original := twFixtureWithScript(t, native)
	a, err := Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	i, _ := a.Find("equipment/test.equ")
	raw, err := a.RawBytes(i)
	if err != nil {
		t.Fatal(err)
	}
	for p := 5; p < len(raw); p += 5 {
		if raw[p] != 1 || !bytes.Equal(raw[p+1:p+5], native[p+3:p+7]) {
			t.Fatal("native token type/value not preserved")
		}
	}
	var untouched bytes.Buffer
	if err := a.SaveTo(&untouched); err != nil || !bytes.Equal(original, untouched.Bytes()) {
		t.Fatal("untouched archive changed")
	}
	text, err := a.Text(i)
	if err != nil || !strings.Contains(text, "{1=-2147483648}") || !strings.Contains(text, "{1=-1}") {
		t.Fatal("opaque tokens not rendered losslessly")
	}
	reader := a.NewFileComparisonReader()
	fp, err := reader.CanonicalFingerprint(context.Background(), i, raw)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(raw)
	changed[6]++
	next, err := reader.CanonicalFingerprint(context.Background(), i, changed)
	if err != nil || fp.Digest == next.Digest {
		t.Fatal("comparison ignored opaque token value")
	}
	if err := a.SetText(i, text); err != nil {
		t.Fatal(err)
	}
	var saved bytes.Buffer
	if err := a.SaveTo(&saved); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(saved.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Release()
	i, _ = b.Find("equipment/test.equ")
	got, err := b.tw.payload(b.data, i)
	if err != nil || !bytes.Equal(native, got) {
		t.Fatal("native token bytes changed after text edit/save/reopen")
	}
}

func TestTWRoundTrip(t *testing.T) {
	original := twFixture(t)
	a, err := Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	if a.Format() != FormatTW || a.ClientVersion() != "TW" || a.Modified() {
		t.Fatal("incorrect TW detection/state")
	}
	if cap := a.WriteCapabilities(); !cap.CanSave || !cap.CanChangeStructure {
		t.Fatal(cap)
	}
	var unchanged bytes.Buffer
	if err := a.SaveTo(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, unchanged.Bytes()) {
		t.Fatal("untouched TW changed")
	}
	i, _ := a.Find("equipment/test.equ")
	text, err := a.Text(i)
	if err != nil || !strings.Contains(text, "測試裝備") || !strings.Contains(text, "[rarity]") {
		t.Fatalf("TW script: %q %v", text, err)
	}
	ref, _ := a.Find("equipment/ref.equ")
	if name, ok := a.ItemName(ref); !ok || name != "繁體名稱" {
		t.Fatalf("TW name reference: %q %v", name, ok)
	}
	rawIndex, _ := a.Find("etc/raw.bin")
	raw, _ := a.RawBytes(rawIndex)
	if !bytes.Equal(raw, []byte{0, 1, 2, 3, 255}) {
		t.Fatal("binary/padding mismatch")
	}
	if err := a.SetText(i, "[name]\n`修改裝備`\n[rarity]\n5"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("equipment/new.equ", "[name]\n`新增裝備`", TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := a.SetText(ref, "[name]\n{8=`<0::equip_name>`}\n[rarity]\n3"); err != nil {
		t.Fatal(err)
	}
	str, _ := a.Find("text/test.str")
	if err := a.SetText(str, "equip_name>更新名稱\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveFiles([]int32{rawIndex}); err != nil {
		t.Fatal(err)
	}
	clone := a.CloneForBatch()
	var edited bytes.Buffer
	if err := clone.SaveTo(&edited); err != nil {
		t.Fatal(err)
	}
	if !a.Modified() || clone.Modified() {
		t.Fatal("clone save changed live state")
	}
	b, err := Parse(edited.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"equipment/test.equ": "修改裝備", "equipment/new.equ": "新增裝備", "text/test.str": "更新名稱"} {
		i, ok := b.Find(path)
		if !ok {
			t.Fatal("missing", path)
		}
		text, err := b.Text(i)
		if err != nil || !strings.Contains(text, want) {
			t.Fatalf("%s: %q %v", path, text, err)
		}
	}
	if _, ok := b.Find("etc/raw.bin"); ok {
		t.Fatal("removed file remains")
	}
	ref, _ = b.Find("equipment/ref.equ")
	if name, ok := b.ItemName(ref); !ok || name != "更新名稱" {
		t.Fatalf("saved reference: %q %v", name, ok)
	}
	if err := b.SetText(ref, "[name]\n`再次修改`"); err != nil {
		t.Fatal(err)
	}
	identityBeforeSave := b.FileIdentity(ref)
	if err := b.SaveTo(io.Discard); err != nil {
		t.Fatal(err)
	}
	if b.FileIdentity(ref) != identityBeforeSave {
		t.Fatal("TW save changed surviving entry identity")
	}
	if b.Modified() {
		t.Fatal("save did not adopt TW state")
	}
	visited := 0
	if err := b.ForEachRawFile(context.Background(), func(index int32, path string, file File, raw []byte) bool {
		visited++
		if int32(len(raw)) != file.DataSize {
			t.Errorf("logical size mismatch: %s", path)
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if visited != int(b.FileCount()) {
		t.Fatal("scan missed files")
	}
}

func TestTWNutTextRoundTrip(t *testing.T) {
	a, err := Parse(twFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	i, ok := a.Find("sqr/common.nut")
	if !ok || a.File(i).DataType != TypeUnicode {
		t.Fatal("NUT source is not editable text")
	}
	original := "// 繁體註解\r\nfunction test() { return 1; }\r\n"
	if text, err := a.Text(i); err != nil || text != original {
		t.Fatalf("NUT read mismatch: %v", err)
	}
	edited := "// 修改註解\r\nfunction test() { return 2; }\r\n"
	if err := a.SetText(i, edited); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("sqr/new.nut", "function added() { return 3; }", TypeUnicode); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	i, _ = b.Find("sqr/common.nut")
	if text, err := b.Text(i); err != nil || text != edited {
		t.Fatalf("NUT save/reopen mismatch: %v", err)
	}
	raw, err := b.tw.payload(b.data, b.items[i].chunk)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, expected) {
		t.Fatal("NUT was saved as tokens or UTF-16 instead of Big5 source")
	}
	i, ok = b.Find("sqr/new.nut")
	if !ok || b.File(i).DataType != TypeUnicode {
		t.Fatal("new NUT is not editable")
	}
	if text, err := b.Text(i); err != nil || text != "function added() { return 3; }" {
		t.Fatal("new NUT source changed")
	}
}

func TestTWRejectMalformed(t *testing.T) {
	fixture := twFixture(t)
	for _, n := range []int{0, 4, 48, 55, len(fixture) - 1} {
		if _, err := Parse(fixture[:n]); err == nil {
			t.Fatalf("accepted truncated TW at %d", n)
		}
	}
	corrupt := bytes.Clone(fixture)
	corrupt[60] ^= 1
	if _, err := Parse(corrupt); err == nil {
		t.Fatal("accepted corrupt directory")
	}
	corrupt = bytes.Clone(fixture)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := Parse(corrupt); err == nil {
		t.Fatal("accepted corrupt payload")
	}
	a, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	i, _ := a.Find("equipment/test.equ")
	if err := a.SetText(i, "[name]\n`😀`"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err == nil {
		t.Fatal("accepted text outside Big5")
	}
	if out.Len() != 0 || !a.Modified() {
		t.Fatal("failed save mutated state/output")
	}
}

func TestTWRealArchive(t *testing.T) {
	path := os.Getenv("PVF_TW_TESTFILE")
	if path == "" {
		t.Skip("PVF_TW_TESTFILE not set")
	}
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	if a.Format() != FormatTW || a.FileCount() == 0 {
		t.Fatal("TW not recognized")
	}
	t.Logf("TW: %d files, %d strings", a.FileCount(), len(a.tw.strings))
	var target int32 = -1
	for i, it := range a.items {
		if it.typ == TypeScript && strings.HasSuffix(a.Path(int32(i)), ".equ") {
			target = int32(i)
			break
		}
	}
	if target < 0 {
		t.Fatal("no equipment script")
	}
	nutIndex, ok := a.Find("sqr/common.nut")
	if !ok || a.File(nutIndex).DataType != TypeUnicode {
		t.Fatal("real NUT is not editable")
	}
	nutRaw, err := a.tw.payload(a.data, a.items[nutIndex].chunk)
	if err != nil {
		t.Fatal(err)
	}
	nutText, err := a.Text(nutIndex)
	if err != nil || nutText != twDecodeText(nutRaw) {
		t.Fatal("real NUT source decode mismatch")
	}
	text, err := a.Text(target)
	if err != nil || !strings.Contains(text, "[name]") {
		t.Fatalf("equipment read: %v", err)
	}
	hash := sha256.New()
	if err := a.SaveTo(hash); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.SourceHash() {
		t.Fatal("untouched real TW bytes changed")
	}
	if err := a.ForEachRawFile(context.Background(), func(index int32, path string, file File, raw []byte) bool {
		if int32(len(raw)) != file.DataSize {
			t.Errorf("TW logical size mismatch at entry %d", index)
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	editedNut := nutText + "\r\n// TW NUT regression\r\n"
	if err := a.SetText(nutIndex, editedNut); err != nil {
		t.Fatal(err)
	}
	if err := a.SetText(target, text+"\n[zz tw regression]\n123"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("etc/zz_tw_test.equ", "[name]\n`繁體測試`", TypeScript); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Release()
	nutIndex, ok = b.Find("sqr/common.nut")
	if !ok {
		t.Fatal("real NUT missing after save")
	}
	if text, err := b.Text(nutIndex); err != nil || text != editedNut {
		t.Fatal("real NUT save/reopen mismatch")
	}
	nutRaw, err = b.tw.payload(b.data, b.items[nutIndex].chunk)
	if err != nil {
		t.Fatal(err)
	}
	expectedNut, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(editedNut))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nutRaw, expectedNut) {
		t.Fatal("real NUT native encoding changed")
	}
	i, ok := b.Find("etc/zz_tw_test.equ")
	if !ok {
		t.Fatal("added file missing")
	}
	if text, err := b.Text(i); err != nil || !strings.Contains(text, "繁體測試") {
		t.Fatalf("edited TW reopen: %v", err)
	}
	i, ok = b.Find(a.Path(target))
	if !ok {
		t.Fatal("edited equipment missing")
	}
	if text, err := b.Text(i); err != nil || !strings.Contains(text, "[zz tw regression]") {
		t.Fatalf("equipment edit reopen: %v", err)
	}
}
