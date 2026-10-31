package services

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"pvfine/internal/pvf"
	"strings"
	"testing"
	"unicode/utf16"
)

const dropTestRow = "0 20 3015 1000000 200000 300000 400000 500000 1 2 3 4 5 0 99 -1 "

func TestIndependentDropStructure(t *testing.T) {
	text := "# 😀\n[independent drop]\n" + dropTestRow + "0\n" + dropTestRow + "1\n[list]\n3176 20 3030 80\n[/list]\n" + dropTestRow + "5\n[list]\n100 1\n[/list]\n[replace list]\n[dungeon condition]\n1 20 1 2 300 100\n[/replace list]\n[/independent drop]\n[dungeon drop rate balance]\n1 2 3 4 5 6\n"
	doc, err := parseIndependentDrop(text, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Rows) != 3 || len(doc.Rows[1].Items) != 2 || !doc.Rows[2].HasExtensions || len(doc.Balance) != 6 || len(doc.Issues) != 1 {
		t.Fatalf("bad structure: %+v", doc)
	}
	if doc.Rows[1].Fields[1].Value != 20 || doc.Rows[1].ListEnd <= doc.Rows[1].ListStart {
		t.Fatal("bad fields")
	}
	a := pvf.New()
	i, err := a.AddFileText(independentDropPath, text, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	c.archive = a
	service := NewFileGUIService(c)
	if _, err := service.ReadIndependentDrop(i, text); err != nil {
		t.Fatal(err)
	}
	// The projection must not mutate the archive overlay.
	if a.ModifiedCount() != 1 {
		t.Fatal("read changed overlay")
	}
}
func TestIndependentDropExternalAndMalformed(t *testing.T) {
	doc, err := parseIndependentDrop("[independent drop]\n"+dropTestRow+"2\n[list]\n17\n[/list]\n[/independent drop]", false)
	if err != nil || len(doc.Rows) != 1 || len(doc.Rows[0].Items[0].Fields) != 1 {
		t.Fatalf("external: %v", err)
	}
	doc, err = parseIndependentDrop("[list]\n10 30\n20 70\n[/list]", true)
	if err != nil || len(doc.Rows[0].Items) != 2 {
		t.Fatalf("list: %v", err)
	}
	for _, text := range []string{"[independent drop]\n1 2\n[/independent drop]", "[independent drop]\n" + dropTestRow + "1\n[/independent drop]", "[independent drop]\n" + dropTestRow + "1\n[list]\n1\n[/list]\n[/independent drop]", "[independent drop]\n" + dropTestRow + "0", "[list]\n1 2 3\n[/list]"} {
		if _, err := parseIndependentDrop(text, strings.HasPrefix(text, "[list]")); err == nil {
			t.Errorf("accepted malformed input %q", text)
		}
	}
}

// Use the existing archive selection convention; no personal sample paths.
func TestIndependentDropReal(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	i, ok := a.Find(independentDropPath)
	if !ok {
		t.Skip("该变体没有独立掉落文件")
	}
	text, err := a.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	c.archive = a
	doc, err := NewFileGUIService(c).ReadIndependentDrop(i, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Rows) == 0 {
		t.Fatal("empty records")
	}
	// Exercise the normal editor save path without touching the source archive.
	token := doc.Rows[0].Fields[3]
	units := utf16.Encode([]rune(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")))
	edited := string(utf16.Decode(units[:token.Start])) + "123456" + string(utf16.Decode(units[token.End:]))
	original, err := a.RawBytes(i)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte(nil), original...)
	// Native script tokens are five bytes; the first token is the section tag.
	if len(expected) < 25 || expected[20] != 0 || int32(binary.LittleEndian.Uint32(expected[21:25])) != int32(token.Value) {
		t.Fatal("unexpected script layout")
	}
	binary.LittleEndian.PutUint32(expected[21:25], 123456)
	if err := a.SetText(i, edited); err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(i)
	if err != nil || !bytes.Equal(raw, expected) {
		t.Fatal("editing changed unrelated binary tokens", err)
	}
	// Also validate every registered external list through the same projection.
	if li, ok := a.Find("etc/independentdrop.lst"); ok {
		pairs, err := a.ListPairs(li)
		if err != nil {
			t.Fatal(err)
		}
		for _, pair := range pairs {
			p := filepath.ToSlash(filepath.Join("etc", pair.Path))
			idx, ok := a.Find(p)
			if !ok {
				t.Fatalf("missing external list %s", pair.ID)
			}
			source, err := a.Text(idx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseIndependentDrop(source, true); err != nil {
				t.Fatalf("list %s: %v", pair.ID, err)
			}
		}
	}
	if os.Getenv("PVF_DROP_SAVE_ROUNDTRIP") == "1" {
		output := filepath.Join(t.TempDir(), "independent-roundtrip.pvf")
		if err := a.SaveAs(output); err != nil {
			t.Fatal(err)
		}
		reopened, err := pvf.Open(output)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Release()
		index, ok := reopened.Find(independentDropPath)
		if !ok {
			t.Fatal("saved file missing")
		}
		raw, err := reopened.RawBytes(index)
		if err != nil || !bytes.Equal(raw, expected) {
			t.Fatal("save/reopen changed script tokens", err)
		}
		source, err := reopened.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		after, err := parseIndependentDrop(source, false)
		if err != nil || len(after.Rows) != len(doc.Rows) || after.Rows[0].Fields[3].Value != 123456 {
			t.Fatal("save/reopen projection differs", err)
		}
	}
	counts := map[float64]int{}
	for _, r := range doc.Rows {
		counts[r.Fields[16].Value]++
	}
	t.Logf("%s: %d records, modes %v, balance %d", a.ClientVersion(), len(doc.Rows), counts, len(doc.Balance))
}

func TestIndependentDropCommentBoundaries(t *testing.T) {
	text := "[independent drop]\n" + dropTestRow + "1\n[list] # [/list]\n3176 20\n[/list]\n# [/independent drop]\n[/independent drop]"
	doc, err := parseIndependentDrop(text, false)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].ListEnd != strings.LastIndex(text, "[/list]") || doc.BodyEnd != strings.LastIndex(text, "[/independent drop]") {
		t.Fatal("comment mistaken for closing section")
	}
}

func TestIndependentDropExternalService(t *testing.T) {
	a := pvf.New()
	for path, source := range map[string]string{
		"etc/independentdrop.lst":        "17 `IndependentDrop/shared.etc`",
		"etc/independentdrop/shared.etc": "[list]\n3176 20\n3030 80\n[/list]",
		independentDropPath:              "[independent drop]\n" + dropTestRow + "2\n[list]\n17\n[/list]\n[/independent drop]",
		"aicharacter/aicharacter.lst":    "20 `example.aic`",
		"aicharacter/example.aic":        "[name]\n`测试 APC`",
	} {
		if _, err := a.AddFileText(path, source, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	c.archive = a
	service := NewFileGUIService(c)
	index, _ := a.Find(independentDropPath)
	text, _ := a.Text(index)
	doc, err := service.ReadIndependentDrop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Lists) != 1 || doc.Lists[0].FileIndex < 0 || doc.Lists[0].ID != "17" {
		t.Fatal("external list not resolved")
	}
	idx := doc.Lists[0].FileIndex
	source, _ := a.Text(idx)
	list, err := service.ReadIndependentDrop(idx, source)
	if err != nil || !list.IsList || len(list.Rows[0].Items) != 2 {
		t.Fatal("external list unavailable", err)
	}
	apcText := strings.Replace(text, "0\t20", "1\t20", 1)
	apc, err := service.ReadIndependentDrop(index, apcText)
	if err != nil {
		t.Fatal(err)
	}
	if apc.Rows[0].MonsterName != "测试 APC" {
		t.Fatalf("APC name = %q", apc.Rows[0].MonsterName)
	}
}
