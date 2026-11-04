package pvf

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestACT60EditStructureRoundTrip(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		a, err := Parse(act60Fixture(t, legacy))
		if err != nil {
			t.Fatal(err)
		}
		baseline := a.CloneForBatch()
		originalBinary, _ := baseline.RawBytes(1)
		identity := a.FileIdentity(1)
		if err := a.SetText(0, "[name]\r\n`修改后的长装备名称`\r\n// 保留排版\r\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.RemoveFiles([]int32{2}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AddFileText("equipment/added.equ", "[name]\n`新增装备`", TypeUnicode); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AddFileText("equipment/added.lst", "123\t`added.equ`", TypeScript); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := a.SaveTo(&out); err != nil {
			t.Fatal(err)
		}
		reopened, err := Parse(out.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if reopened.FileCount() != 5 || a.Modified() || a.FileIdentity(1) != identity {
			t.Fatal("incorrect saved state")
		}
		raw, err := reopened.RawBytes(1)
		if err != nil || !bytes.Equal(raw, originalBinary) {
			t.Fatal("unchanged binary changed", err)
		}
		for path, want := range map[string]string{"equipment/test.equ": "修改后的长装备名称", "equipment/added.equ": "新增装备", "equipment/added.lst": "added.equ"} {
			i, ok := reopened.Find(path)
			if !ok {
				t.Fatal("added/edited path missing")
			}
			text, err := reopened.Text(i)
			if err != nil || !strings.Contains(text, want) {
				t.Fatal("saved text mismatch", err)
			}
		}
		if _, ok := reopened.Find("etc/empty.txt"); ok {
			t.Fatal("deleted file returned")
		}
		// Save again from the adopted baseline, including a raw binary replacement.
		if err := a.SetRawBytes(1, []byte{7, 0, 9, 255, 0}); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if err := a.SaveTo(&out); err != nil {
			t.Fatal(err)
		}
		b, err := Parse(out.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		raw, err = b.RawBytes(1)
		if err != nil || !bytes.Equal(raw, []byte{7, 0, 9, 255, 0}) {
			t.Fatal("second save mismatch", err)
		}
		original, _ := baseline.Text(0)
		if strings.Contains(original, "修改") {
			t.Fatal("save mutated clone")
		}
		a.Release()
		baseline.Release()
		reopened.Release()
		b.Release()
	}
}

func TestACT60SaveFailurePreservesEdits(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		a, err := Parse(act60Fixture(t, legacy))
		if err != nil {
			t.Fatal(err)
		}
		source := a.act60
		for _, raw := range [][]byte{utf16le("无法编码😀"), {0xff}, utf16le("文本\x00不能包含零")} {
			if err := a.SetRawBytes(0, raw); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := a.SaveTo(&out); err == nil || out.Len() != 0 || !a.Modified() || a.act60 != source {
				t.Fatal("failed encoding mutated/wrote archive", err)
			}
		}
		if err := a.SetText(0, "正常修改"); err != nil {
			t.Fatal(err)
		}
		if err := a.SaveTo(shortProgressWriter{}); !errors.Is(err, io.ErrShortWrite) || !a.Modified() || a.act60 != source {
			t.Fatal("failed writer lost edits", err)
		}
		var out bytes.Buffer
		if err := a.SaveTo(&out); err != nil || a.Modified() {
			t.Fatal("retry save failed", err)
		}
		a.Release()
	}
}

func TestACT60UTF8BOMSave(t *testing.T) {
	a, err := Parse(act60Fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	// Seed an original UTF-8 file through the native fixture transport.
	old := a.act60.entries[0]
	native := append([]byte{0xef, 0xbb, 0xbf}, []byte("[name] `原始😀`\n")...)
	encrypted, crc := act60Encrypt(native, old.number)
	data := bytes.Clone(a.data)
	offset := len(data) - a.act60.body
	data = append(data, encrypted...)
	old.offset, old.size, old.crc = uint32(offset), uint32(len(native)), crc
	a.data = data
	a.act60.entries[0] = old
	if err := a.SetText(0, "[name] `修改😀`\n"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	raw, err := a.act60.payload(a.data, 0)
	if err != nil || !bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) || !strings.Contains(string(raw), "修改😀") {
		t.Fatal("UTF8 BOM was lost", err)
	}
	a.Release()
}

func TestRealACT60EditRoundTrip(t *testing.T) {
	a := openReal(t)
	defer a.Release()
	if a.Format() != FormatACT60 {
		t.Skip("not ACT60")
	}
	original := a.CloneForBatch()
	defer original.Release()
	index := int32(-1)
	for i := int32(1); i < a.FileCount(); i++ {
		if strings.HasSuffix(a.Path(i), ".equ") {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("equipment missing")
	}
	path := a.Path(index)
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	text += "\r\n// 修改保存往返验证\r\n"
	if err := a.SetText(index, text); err != nil {
		t.Fatal(err)
	}
	removed := a.Path(0)
	if _, err := a.RemoveFiles([]int32{0}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("etc/pvfine_act_roundtrip.txt", "修改保存测试", TypeUnicode); err != nil {
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
	if b.FileCount() != original.FileCount() {
		t.Fatal("count mismatch")
	}
	if _, ok := b.Find(removed); ok {
		t.Fatal("removed entry returned")
	}
	if got, err := b.Text(mustFind(b, path)); err != nil || got != text {
		t.Fatal("edited text mismatch", err)
	}
	if got, err := b.Text(mustFind(b, "etc/pvfine_act_roundtrip.txt")); err != nil || got != "修改保存测试" {
		t.Fatal("added text mismatch", err)
	}
	// Verify ALL untouched native payloads, including ANSI bytes that may not
	// round trip through GBK, and all binary ANI resources.
	for i := int32(1); i < original.FileCount(); i++ {
		if i == index {
			continue
		}
		j, ok := b.Find(original.Path(i))
		if !ok {
			t.Fatal("untouched path missing")
		}
		want, err := original.act60.payload(original.data, i)
		if err != nil {
			t.Fatal(err)
		}
		got, err := b.act60.payload(b.data, j)
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("untouched native payload changed at %d: %v", i, err)
		}
	}
	t.Logf("validated edit/add/delete and %d untouched payloads", original.FileCount()-2)
}

func TestRealACT60AmbiguousShopEditRoundTrip(t *testing.T) {
	a := openReal(t)
	defer a.Release()
	if a.Format() != FormatACT60 {
		t.Skip("not ACT60")
	}
	index, ok := a.Find("itemshop/magic2.shp")
	if !ok {
		t.Skip("magic2.shp missing")
	}
	original, err := a.act60.payload(a.data, a.items[index].chunk)
	if err != nil {
		t.Fatal(err)
	}
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := a.encodeNativeText(text); err != nil || !bytes.Equal(back, original) {
		t.Fatal("native interpretation does not round trip", err)
	}
	text += "\r\n// native encoding regression\r\n"
	if err := a.SetText(index, text); err != nil {
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
	index, ok = b.Find("itemshop/magic2.shp")
	if !ok {
		t.Fatal("shop missing after save")
	}
	if got, err := b.Text(index); err != nil || got != text {
		t.Fatal("edited shop did not round trip", err)
	}
}
