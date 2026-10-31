package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"

	"pvfine/internal/pvf"
)

func TestDecodeImportText(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		codec      encoding.Encoding
	}{
		{"UTF-8", "[name]\r\n`测试装备`\r\n", nil},
		{"GBK", "[name]\r\n`测试装备`\r\n[desc]\r\n`这是一件可以增加力量和攻击速度的装备，装备后可以提升角色的攻击力和防御力。`\r\n", simplifiedchinese.GBK},
		{"GBK", "[name]\n`测试装备`", simplifiedchinese.GBK},
		{"Big5", "[name]\r\n`測試裝備`\r\n[desc]\r\n`這是一件可以增加力量和攻擊速度的裝備，裝備後可以提升角色的攻擊力和防禦力。`\r\n", traditionalchinese.Big5},
		{"EUC-KR", "[name]\n`테스트 장비`\n[desc]\n`이 아이템은 캐릭터의 공격력과 방어력을 증가시키고 공격 속도를 높여 줍니다.`", korean.EUCKR},
		{"UTF-16LE", "[name]\n`测试装备`", textunicode.UTF16(textunicode.LittleEndian, textunicode.UseBOM)},
		{"UTF-16BE", "[name]\n`測試裝備`", textunicode.UTF16(textunicode.BigEndian, textunicode.UseBOM)},
		{"UTF-16LE", "[name]\n`测试装备`", textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM)},
		{"UTF-16BE", "[name]\n`測試裝備`", textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM)},
		{"UTF-32LE", "[name]\n`测试装备`", utf32.UTF32(utf32.LittleEndian, utf32.UseBOM)},
		{"UTF-32BE", "[name]\n`测试装备`", utf32.UTF32(utf32.BigEndian, utf32.UseBOM)},
	} {
		t.Run(tc.name+tc.text[:6], func(t *testing.T) {
			data := []byte(tc.text)
			if tc.codec != nil {
				var err error
				data, err = tc.codec.NewEncoder().Bytes(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			got, name, err := decodeImportText(data)
			if err != nil || got != tc.text || name != tc.name {
				t.Fatalf("encoding=%s, text=%q, err=%v; want encoding=%s text=%q", name, got, err, tc.name, tc.text)
			}
		})
	}
	for _, data := range [][]byte{append([]byte{0xef, 0xbb, 0xbf}, 0xff), {0xff, 0xfe, 0xfd}, {0xff, 0xfe, 0, 0xd8}, {0, 1, 2}, []byte("abc\x00def")} {
		if _, _, err := decodeImportText(data); err == nil {
			t.Fatalf("accepted invalid/binary input %x", data)
		}
	}
	if got, name, err := decodeImportText(append([]byte{0xef, 0xbb, 0xbf}, []byte("测试")...)); err != nil || name != "UTF-8" || got != "测试" {
		t.Fatalf("UTF-8 BOM: %q %s %v", got, name, err)
	}
}

func TestImportEncodingPreviewAndApply(t *testing.T) {
	c := NewCore()
	if err := c.setArchive(pvf.New()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	source := "[name]\n`测试装备`"
	data, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.equ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewArchiveService(c)
	preview, err := svc.PreviewImport([]string{path}, "", ImportModeText)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ConvertedCount != 1 || preview.Entries[0].SourceEncoding != "GBK" || c.archive.FileCount() != 0 {
		t.Fatalf("unexpected preview %+v", preview)
	}
	if _, err := svc.ImportFiles([]string{path}, "", ImportModeText); err != nil {
		t.Fatal(err)
	}
	i, ok := c.archive.Find("test.equ")
	if !ok {
		t.Fatal("missing file")
	}
	if got, err := c.archive.Text(i); err != nil || !strings.Contains(got, "`测试装备`") {
		t.Fatalf("text=%q err=%v", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("source changed")
	}
	rawPreview, err := svc.PreviewImport([]string{path}, "", ImportModeRaw)
	if err != nil || rawPreview.ConvertedCount != 0 || rawPreview.Entries[0].SourceEncoding != "" {
		t.Fatalf("raw preview=%+v err=%v", rawPreview, err)
	}
	for _, read := range []func([]importFile, string) error{readImportFiles, readMCPImportFiles} {
		files := []importFile{{sourcePath: path, targetPath: "test.equ"}}
		if err := read(files, ImportModeText); err != nil || files[0].text != source {
			t.Fatalf("shared decoding: %+v %v", files[0], err)
		}
		if err := read(files, ImportModeRaw); err != nil || !bytes.Equal(files[0].data, data) {
			t.Fatalf("raw import: %v", err)
		}
	}
}
