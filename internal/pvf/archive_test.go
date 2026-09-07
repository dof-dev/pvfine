package pvf

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

const scriptText = "[name]\n`测试物品`\n[grade]\n74\n[rarity]\n3\n[price]\n12.5\n[icon]\n`item/x.img` 3\n"

// Canonical decompiled form: tags are line-oriented; values are indented and
// compact values on the same line are separated by a leading tab.
const scriptTextDecoded = "[name]\n\t`测试物品`\n[grade]\n\t74\n[rarity]\n\t3\n[price]\n\t12.5\n[icon]\n\t`item/x.img`\t3"

func TestSyntheticRoundTrip(t *testing.T) {
	a := New()
	a.AddFileText("equip/a/b.equ", scriptText, TypeScript)
	a.AddFileText("equip/a/c.equ", scriptText, TypeScript)
	a.AddFile("str/korean.str", utf16le("한국스크립트"), TypeUnicode)
	a.AddFile("bin/data.bin", []byte{0, 1, 2, 3, 250, 251}, TypeScript)

	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatalf("save: %v", err)
	}

	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if b.FileCount() != 4 {
		t.Fatalf("file count = %d, want 4", b.FileCount())
	}
	for _, p := range []string{"equip/a/b.equ", "equip/a/c.equ", "str/korean.str", "bin/data.bin"} {
		if _, ok := b.Find(p); !ok {
			t.Errorf("path %q missing after round trip", p)
		}
	}
	i, _ := b.Find("equip/a/b.equ")
	got, err := b.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	if got != scriptTextDecoded {
		t.Errorf("script text round trip mismatch:\n%q\n%q", got, scriptTextDecoded)
	}
	i, _ = b.Find("str/korean.str")
	raw, _ := b.RawBytes(i)
	if !bytes.Equal(raw, utf16le("한국스크립트")) {
		t.Errorf("utf16 payload mismatch")
	}
}

func TestSyntheticEditAndRebuild(t *testing.T) {
	a := New()
	a.AddFile("x/a.txt", []byte("[a]\n`one`"), TypeScript)
	a.AddFile("x/b.txt", []byte("[b]\n`two`"), TypeScript)
	a.AddFile("x/c.txt", []byte("[c]\n`three`"), TypeScript)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	// Edit the middle file: its chunk must be rebuilt, neighbors untouched.
	ib, _ := b.Find("x/b.txt")
	if err := b.SetText(ib, "[b]\n`TWO modified`"); err != nil {
		t.Fatal(err)
	}
	var out2 bytes.Buffer
	if err := b.SaveTo(&out2); err != nil {
		t.Fatal(err)
	}

	c, err := Parse(out2.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := c.Text(mustFind(c, "x/b.txt"))
	if !strings.Contains(got, "TWO modified") {
		t.Errorf("edited content missing: %q", got)
	}
	ia, _ := c.Find("x/a.txt")
	rawA, _ := c.RawBytes(ia)
	if string(rawA) != "[a]\n`one`" {
		t.Errorf("neighbor file corrupted: %q", rawA)
	}

	// Add a file on top of the edited archive.
	c.AddFileText("x/d.txt", "[d]\n`four`", TypeScript)
	var out3 bytes.Buffer
	if err := c.SaveTo(&out3); err != nil {
		t.Fatal(err)
	}
	d, err := Parse(out3.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if d.FileCount() != 4 {
		t.Fatalf("count after add = %d", d.FileCount())
	}
	got, _ = d.Text(mustFind(d, "x/d.txt"))
	if !strings.Contains(got, "four") {
		t.Errorf("added content missing: %q", got)
	}
	got, _ = d.Text(mustFind(d, "x/b.txt"))
	if !strings.Contains(got, "TWO modified") {
		t.Errorf("earlier edit lost after add: %q", got)
	}
}

func TestKoreanMojibake(t *testing.T) {
	original := "한국스크립트용 문자열 데이터 파일"
	painted, err := EncodeKoreanMojibake(original)
	if err != nil {
		t.Fatal(err)
	}
	paintedStr := string(utf16.Decode(u16le(painted)))
	if !looksLikeCP437Painting(paintedStr) {
		t.Fatalf("encoded form should look CP437-painted")
	}
	if got := fixKoreanMojibake(paintedStr); got != original {
		t.Errorf("restore mismatch: %q", got)
	}
}

func mustFind(a *Archive, path string) int32 {
	i, ok := a.Find(path)
	if !ok {
		panic("missing path " + path)
	}
	return i
}

// ---- tests against the real archive ---------------------------------------
// Set PVF_TESTFILE to a Script.pvf path to enable.

const testFileEnv = "PVF_TESTFILE"

func openReal(t *testing.T) *Archive {
	t.Helper()
	p := os.Getenv(testFileEnv)
	if p == "" {
		t.Skipf("%s not set; skipping real-archive tests", testFileEnv)
	}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return a
}

func TestRealParse(t *testing.T) {
	a := openReal(t)
	if a.FileCount() != 1008171 {
		t.Errorf("FileCount = %d", a.FileCount())
	}
	if got := a.Path(0); got != "aicharacter/_bizarre/atgunner/mirror_atgunner/action/proc.act" {
		t.Errorf("path[0] = %q", got)
	}
	i, ok := a.Find("equipment/character/common/amulet/100300001.equ")
	if !ok {
		t.Fatal("amulet not found")
	}
	text, err := a.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "烈火之心项链") {
		t.Errorf("amulet name missing; got: %.120s", text)
	}
	i, ok = a.Find("aicharacter/aicharacter.kor.str")
	if !ok {
		t.Fatal("kor str not found")
	}
	text, _ = a.Text(i)
	if !strings.Contains(text, "한국스크립트") {
		t.Errorf("korean restore failed: %.120s", text)
	}
}

func TestRealRoundTripByteIdentical(t *testing.T) {
	a := openReal(t)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(a.data)
	got := sha256.Sum256(out.Bytes())
	if want != got {
		t.Errorf("unmodified save is not byte-identical")
	}
}

func TestRealEditRoundTrip(t *testing.T) {
	a := openReal(t)
	tmp := t.TempDir()

	const target = "equipment/character/common/amulet/100300001.equ"
	i, _ := a.Find(target)
	if err := a.SetText(i, scriptText); err != nil {
		t.Fatal(err)
	}
	const added = "zz_test/hello.txt"
	if _, err := a.AddFileText(added, "[hello]\n`world`", TypeScript); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(tmp, "out.pvf")
	if err := a.SaveAs(outPath); err != nil {
		t.Fatal(err)
	}

	b, err := Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.FileCount() != a.FileCount() {
		t.Fatalf("count = %d, want %d", b.FileCount(), a.FileCount())
	}
	got, _ := b.Text(mustFind(b, target))
	if got != scriptTextDecoded {
		t.Errorf("edited script mismatch:\n%q", got)
	}
	got, _ = b.Text(mustFind(b, added))
	if !strings.Contains(got, "world") {
		t.Errorf("added file content: %q", got)
	}

	// A different, untouched file in the same chunk region must survive.
	j, ok := b.Find("monster/spirit/magedarkhigherspirit/action/hiveattack.act")
	if !ok {
		t.Fatal("untouched file lost")
	}
	raw, err := b.RawBytes(j)
	if err != nil || len(raw) == 0 {
		t.Errorf("untouched file unreadable: %v", err)
	}
}

func TestSaveToSourceAndExtract(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.pvf")

	a := New()
	a.AddFileText("dir/sub/file.txt", "[hello]\n`world`", TypeScript)
	if err := a.SaveAs(p); err != nil {
		t.Fatal(err)
	}

	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.SourcePath() != p {
		t.Fatalf("source path = %q", b.SourcePath())
	}
	i, _ := b.Find("dir/sub/file.txt")
	if err := b.SetText(i, "[hello]\n`changed`"); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}

	c, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := c.Text(mustFind(c, "dir/sub/file.txt"))
	if !strings.Contains(got, "changed") {
		t.Errorf("save-to-source lost the edit: %q", got)
	}

	edir := filepath.Join(tmp, "out")
	if err := c.ExtractTo(edir, nil, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(edir, "dir", "sub", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := c.RawBytes(mustFind(c, "dir/sub/file.txt"))
	if !bytes.Equal(raw, want) {
		t.Errorf("extracted bytes differ from payload")
	}
}
