package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariantArchiveService drives the UI-facing services against the archive
// supplied via PVF_TESTFILE. This variant uses non-standard per-section LCG
// seeds, so it exercises the parser's seed recovery through the real open,
// tree, search and read paths rather than the parser alone.
func TestVariantArchiveService(t *testing.T) {
	src := os.Getenv("PVF_TESTFILE")
	if src == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	// Hard-link into a temp dir so nothing touches the source archive.
	link := filepath.Join(t.TempDir(), "Script.pvf")
	if err := os.Link(src, link); err != nil {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := NewCore()
	t.Cleanup(c.closeArchive)
	svc := NewArchiveService(c)
	info, err := svc.Open(link)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if info.FileCount <= 0 {
		t.Fatalf("file count = %d", info.FileCount)
	}
	t.Logf("opened: %d files, %d groups", info.FileCount, info.GroupCount)

	root, err := svc.ListChildren("")
	if err != nil || len(root) == 0 {
		t.Fatalf("root children: %v, %d", err, len(root))
	}
	if monster, err := svc.ListChildren("monster"); err != nil || len(monster) == 0 {
		t.Fatalf("monster dir: %v, %d", err, len(monster))
	}

	c.startSearchIndex()
	waitForSearchIndex(t, c)
	res, err := svc.Search("100300001", 0, 10)
	if err != nil || len(res.Hits) == 0 {
		t.Fatalf("search: %v, %d hits", err, len(res.Hits))
	}
	hit := res.Hits[0]
	if !strings.HasSuffix(hit.Path, "100300001.equ") {
		t.Fatalf("unexpected search hit: %s", hit.Path)
	}

	// The editor path must decompile the retrieved script, which only works
	// when the string pools and body chunks decoded with the recovered seeds.
	meta, err := NewEditorService(c).GetFile(hit.FileIndex)
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if meta.Path == "" || !strings.Contains(meta.Text, "[name]") {
		t.Fatalf("script not decompiled: path=%q text=%.80q", meta.Path, meta.Text)
	}
	t.Logf("script: %s -> %.60q", meta.Path, meta.Text)
}
