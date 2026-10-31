package services

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
)

func importModFixture(t *testing.T) (*core, *ModImportService, string) {
	t.Helper()
	c, _ := exportFixture(t)
	service := NewModImportService(c)
	dir := t.TempDir()
	pkg := modpkg.Package{Metadata: modpkg.Metadata{Name: "测试导入", Version: "2.0"}, Entries: []modpkg.Entry{
		{Path: "stackable/demo.stk", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: pvf.TypeScript, Data: []byte("[name]\n`导入名称`\n[price]\n200")},
		{Path: "stackable/new.stk", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: pvf.TypeScript, Data: []byte("[price]\n300")},
		{Path: "list/stackable.lst", Operation: modpkg.MergeList, Pairs: []modpkg.Pair{{Key: "2", Value: "stackable/new.stk"}}},
		{Path: "string/demo.str", Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "name", Value: " 新值 > "}, {Key: "added", Value: ""}}},
	}}
	if err := (modpkg.Extend110Writer{}).Write(dir, pkg); err != nil {
		t.Fatal(err)
	}
	return c, service, dir
}

func modText(t *testing.T, c *core, p string) string {
	t.Helper()
	i, ok := c.archive.Find(p)
	if !ok {
		t.Fatalf("missing %s", p)
	}
	text, err := c.archive.Text(i)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestModImportFrozenPreviewAndMerge(t *testing.T) {
	c, s, dir := importModFixture(t)
	before := modText(t, c, "stackable/demo.stk")
	preview, err := s.Prepare(dir, "110USextend")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release(preview.ID)
	if preview.ImportedCount != 1 || preview.OverwrittenCount != 3 || len(preview.Files) != 4 {
		t.Fatalf("preview = %#v", preview)
	}
	if got := modText(t, c, "stackable/demo.stk"); got != before {
		t.Fatal("preview changed archive")
	}
	for _, file := range preview.Files {
		if file.Path == "string/demo.str" && (len(file.Pairs) != 2 || !file.Pairs[0].Overwrite || file.Pairs[1].Overwrite || file.Pairs[0].Previous != "测试物品") {
			t.Fatalf("pairs = %#v", file.Pairs)
		}
	}
	// Confirmation uses the frozen content even if the original package disappears.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 1 || result.OverwrittenCount != 3 || len(result.ChangedPaths) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if got := modText(t, c, "stackable/demo.stk"); !strings.Contains(got, "导入名称") {
		t.Fatalf("script = %s", got)
	}
	if got := modText(t, c, "string/demo.str"); !strings.Contains(got, "name> 新值 > ") || !strings.Contains(got, "keep>保留") || !strings.Contains(got, "empty>") || !strings.Contains(got, "added>") {
		t.Fatalf("strings = %q", got)
	}
	list, err := pvf.ParseListText(modText(t, c, "list/stackable.lst"))
	if err != nil || len(list) != 2 || list[0].ID != "1" || list[1].ID != "2" {
		t.Fatalf("list = %#v, %v", list, err)
	}
	if _, err := s.Execute(preview.ID); err == nil {
		t.Fatal("plan reused")
	}
}

func TestModImportRejectsStalePreview(t *testing.T) {
	for _, reason := range []string{"revision", "edit", "archive", "version", "expired", "release"} {
		t.Run(reason, func(t *testing.T) {
			c, s, dir := importModFixture(t)
			preview, err := s.Prepare(dir, "110USextend")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Release(preview.ID)
			before := modText(t, c, "stackable/demo.stk")
			switch reason {
			case "revision":
				c.batchRevision++
			case "edit":
				index, _ := c.archive.Find("stackable/demo.stk")
				if err := NewEditorService(c).SetText(index, "[price]\n999"); err != nil {
					t.Fatal(err)
				}
				before = modText(t, c, "stackable/demo.stk")
			case "archive":
				c.archiveGeneration++
			case "version":
				c.versionLoadID++
			case "expired":
				s.plan.created = time.Now().Add(-11 * time.Minute)
			case "release":
				s.Release(preview.ID)
			}
			if _, err := s.Execute(preview.ID); err == nil {
				t.Fatal("stale preview accepted")
			}
			if modText(t, c, "stackable/demo.stk") != before {
				t.Fatal("stale import changed archive")
			}
		})
	}
}

func TestModImportInvalidPayloadLeavesArchiveUnchanged(t *testing.T) {
	c, s, dir := importModFixture(t)
	before := modText(t, c, "stackable/demo.stk")
	if err := os.WriteFile(filepath.Join(dir, "merge/string/demo.str"), []byte("invalid row"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prepare(dir, "110USextend"); err == nil {
		t.Fatal("invalid table accepted")
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("invalid import changed archive")
	}
	if _, ok := c.archive.Find("stackable/new.stk"); ok {
		t.Fatal("partial import installed")
	}
}

func TestModMergeTables(t *testing.T) {
	text, rows, err := mergeModTable(" // 保留注释\r\na>原值\r\nb> 保留空格 ", modpkg.Entry{Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "a", Value: ""}, {Key: "c", Value: "new>text"}}})
	if err != nil || text != " // 保留注释\r\na>\r\nb> 保留空格 \nc>new>text\n" || len(rows) != 2 {
		t.Fatalf("merge = %q, %#v, %v", text, rows, err)
	}
	text, rows, err = mergeModTable("1 `old`\n2 `keep`", modpkg.Entry{Operation: modpkg.MergeList, Pairs: []modpkg.Pair{{Key: "1", Value: "new"}, {Key: "3", Value: "added"}}})
	if err != nil || text != "1\t`new`\n2\t`keep`\n3\t`added`\n" || len(rows) != 2 || !rows[0].Overwrite {
		t.Fatalf("merge = %q, %#v, %v", text, rows, err)
	}
}

func TestModImportVersionUndo(t *testing.T) {
	c, s, dir := importModFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	before := modText(t, c, "stackable/demo.stk")
	preview, err := s.Prepare(dir, "110USextend")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(preview.ID); err != nil {
		t.Fatal(err)
	}
	status := versions.Status()
	if status.PendingChangeSets != 1 || status.ChangedFiles != 4 {
		t.Fatalf("status = %#v", status)
	}
	if _, err := versions.Undo(); err != nil {
		t.Fatal(err)
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("undo did not restore replaced file")
	}
	if _, ok := c.archive.Find("stackable/new.stk"); ok {
		t.Fatal("undo did not remove new file")
	}
	if got := modText(t, c, "string/demo.str"); strings.Contains(got, "added>") {
		t.Fatal("undo did not restore merged table")
	}
}

func TestModImportStageFailureRollsBack(t *testing.T) {
	c, s, dir := importModFixture(t)
	preview, err := s.Prepare(dir, "110USextend")
	if err != nil {
		t.Fatal(err)
	}
	before := modText(t, c, "stackable/demo.stk")
	// Inject a failure after at least one valid staged write. The live archive
	// must remain intact even if staging fails after preview has succeeded.
	s.plan.files = append(s.plan.files, importFile{targetPath: "fail/invalid.stk", dataType: 999, text: "invalid"})
	if _, err := s.Execute(preview.ID); err == nil {
		t.Fatal("invalid staged type accepted")
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("failed transaction changed archive")
	}
	if _, ok := c.archive.Find("stackable/new.stk"); ok {
		t.Fatal("failed transaction installed partial new file")
	}
}

func TestModImportCreatesMissingTables(t *testing.T) {
	c, s, dir := importModFixture(t)
	if err := os.MkdirAll(filepath.Join(dir, "merge/new"), 0700); err != nil {
		t.Fatal(err)
	}
	for p, text := range map[string]string{"list.lst": "1 `stackable/new.stk`\n", "strings.str": "name>\n"} {
		if err := os.WriteFile(filepath.Join(dir, "merge/new", p), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := s.Prepare(dir, "110USextend")
	if err != nil {
		t.Fatal(err)
	}
	if preview.ImportedCount != 3 {
		t.Fatalf("preview = %#v", preview)
	}
	if _, err := s.Execute(preview.ID); err != nil {
		t.Fatal(err)
	}
	if got := modText(t, c, "new/strings.str"); got != "name>\n" {
		t.Fatalf("new strings = %q", got)
	}
	if pairs, err := pvf.ParseListText(modText(t, c, "new/list.lst")); err != nil || len(pairs) != 1 {
		t.Fatalf("new list = %#v, %v", pairs, err)
	}
}

func TestModMergeExistingBOM(t *testing.T) {
	for _, base := range []string{"\ufeffname>old\nkeep>value\n", "\ufeff"} {
		text, rows, err := mergeModTable(base, modpkg.Entry{Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "name", Value: "new"}}})
		if err != nil || len(rows) != 1 || !strings.HasPrefix(text, "\ufeffname>new\n") || strings.Count(text, "name>") != 1 {
			t.Fatalf("BOM merge = %q, %#v, %v", text, rows, err)
		}
	}
}

// Both test readers can recognize the same directory; only the selected reader
// is allowed to run, so adding formats cannot introduce ambiguous detection.
type selectedModReader struct {
	detected       bool
	pkg            modpkg.Package
	detects, reads int
}

func (r *selectedModReader) Detect(fs.FS) (bool, error)         { r.detects++; return r.detected, nil }
func (r *selectedModReader) Read(fs.FS) (modpkg.Package, error) { r.reads++; return r.pkg, nil }

func TestModImportSelectedFormat(t *testing.T) {
	c, s, dir := importModFixture(t)
	selected := &selectedModReader{detected: true, pkg: modpkg.Package{
		Metadata: modpkg.Metadata{Name: "另一种格式"},
		Entries:  []modpkg.Entry{{Path: "string/alternate.str", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: pvf.TypeUnicode, Data: []byte("name>alternate")}},
	}}
	other := &selectedModReader{detected: true}
	for _, format := range []modpkg.Format{
		{Descriptor: modpkg.Descriptor{ID: "alternate", Name: "另一种格式"}, Reader: selected},
		{Descriptor: modpkg.Descriptor{ID: "other", Name: "其他格式"}, Reader: other},
		{Descriptor: modpkg.Descriptor{ID: "write-only", Name: "仅导出格式"}, Writer: modpkg.Extend110Writer{}},
	} {
		if err := s.registry.Register(format); err != nil {
			t.Fatal(err)
		}
	}
	formats := s.Formats()
	if len(formats) != 3 {
		t.Fatalf("readable formats = %#v", formats)
	}
	for _, format := range formats {
		if !format.CanRead || format.ID == "write-only" {
			t.Fatalf("unreadable option: %#v", format)
		}
	}
	preview, err := s.Prepare(dir, "alternate")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Format != "alternate" || preview.Name != "另一种格式" || selected.detects != 1 || selected.reads != 1 || other.detects != 0 || other.reads != 0 {
		t.Fatalf("wrong format dispatch: preview=%#v selected=%#v other=%#v", preview, selected, other)
	}
	if _, err := s.Execute(preview.ID); err != nil {
		t.Fatal(err)
	}
	if text := modText(t, c, "string/alternate.str"); text != "name>alternate" {
		t.Fatalf("alternate text = %q", text)
	}
}

func TestModImportRejectsInvalidFormatSelection(t *testing.T) {
	c, s, dir := importModFixture(t)
	mismatch := &selectedModReader{}
	for _, format := range []modpkg.Format{
		{Descriptor: modpkg.Descriptor{ID: "mismatch", Name: "不匹配格式"}, Reader: mismatch},
		{Descriptor: modpkg.Descriptor{ID: "write-only", Name: "仅导出格式"}, Writer: modpkg.Extend110Writer{}},
	} {
		if err := s.registry.Register(format); err != nil {
			t.Fatal(err)
		}
	}
	before := modText(t, c, "stackable/demo.stk")
	for _, id := range []string{"", "unknown", "write-only", "mismatch"} {
		if _, err := s.Prepare(dir, id); err == nil {
			t.Fatalf("invalid format %q accepted", id)
		}
	}
	if mismatch.detects != 1 || mismatch.reads != 0 || s.plan != nil {
		t.Fatalf("mismatch was read or prepared: %#v", mismatch)
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("invalid format selection changed archive")
	}
}
