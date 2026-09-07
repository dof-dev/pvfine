package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func openForTest(path string) (*pvf.Archive, error) {
	return pvf.Open(path)
}

func mustReopen(t *testing.T, path string) *pvf.Archive {
	t.Helper()
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testArchive(t *testing.T) *core {
	t.Helper()
	src := os.Getenv("PVF_TESTFILE")
	if src == "" {
		t.Skip("PVF_TESTFILE not set; skipping service integration test")
	}
	// 硬链接到临时目录,编辑/保存测试不会触碰源文件
	link := filepath.Join(t.TempDir(), "Script.pvf")
	if err := os.Link(src, link); err != nil {
		// 跨盘等情况下退回复制
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	a, err := openForTest(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c
}

func TestArchiveServiceTreeAndSearch(t *testing.T) {
	c := testArchive(t)
	svc := NewArchiveService(c)

	if svc.Info().FileCount != 1008171 {
		t.Fatalf("file count = %d", svc.Info().FileCount)
	}
	root, err := svc.ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) == 0 || !root[0].IsDir {
		t.Fatalf("root children invalid: %d entries", len(root))
	}

	// 展开一个已知目录
	monster, err := svc.ListChildren("monster")
	if err != nil || len(monster) == 0 {
		t.Fatalf("monster dir: %v, %d entries", err, len(monster))
	}

	// 分页搜索
	res, err := svc.Search("100300001", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || !strings.HasSuffix(res.Hits[0].Path, "100300001.equ") {
		t.Fatalf("search result unexpected: %d hits", len(res.Hits))
	}
}

func TestEditorServiceEditAndSave(t *testing.T) {
	c := testArchive(t)
	archiveSvc := NewArchiveService(c)
	editorSvc := NewEditorService(c)

	// 通过搜索定位目标文件(与前端行为一致)
	res, err := archiveSvc.Search("equipment/character/common/amulet/100300001.equ", 0, 5)
	if err != nil || len(res.Hits) == 0 {
		t.Fatalf("locate target: %v", err)
	}
	idx := res.Hits[0].FileIndex

	meta, err := editorSvc.GetFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Editable || !strings.Contains(meta.Text, "[name]") {
		t.Fatalf("unexpected meta: editable=%v len=%d", meta.Editable, len(meta.Text))
	}

	if err := editorSvc.SetText(idx, "[name]\n`测试项链`\n"); err != nil {
		t.Fatal(err)
	}
	if c.archive.ModifiedCount() != 1 {
		t.Fatalf("modified count = %d", c.archive.ModifiedCount())
	}

	info, err := editorSvc.Save()
	if err != nil {
		t.Fatal(err)
	}
	if info.ModifiedCount != 0 {
		t.Fatalf("modified after save = %d", info.ModifiedCount)
	}

	// 重新打开验证落盘
	if err := c.setArchive(mustReopen(t, c.archive.SourcePath())); err != nil {
		t.Fatal(err)
	}
	meta2, err := editorSvc.GetFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta2.Text, "测试项链") {
		t.Fatalf("edit not persisted: %.80s", meta2.Text)
	}
}
