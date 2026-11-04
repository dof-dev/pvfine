package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestArchiveServiceImportFilesTextAndDirectoryMapping(t *testing.T) {
	sourceDir := t.TempDir()
	nestedDir := filepath.Join(sourceDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "name.str"), []byte("导入文本"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "item.equ"), []byte("[name]\n`导入脚本`"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewCore()
	a := pvf.New()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	service := NewArchiveService(c)
	preview, err := service.PreviewImport(
		[]string{sourceDir, nestedDir},
		"equipment",
		ImportModeText,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalFiles != 2 || preview.ImportedCount != 2 || preview.OverwrittenCount != 0 {
		t.Fatalf("import preview = %#v", preview)
	}
	if c.archive.FileCount() != 0 {
		t.Fatalf("preview modified archive: file count = %d", c.archive.FileCount())
	}

	result, err := service.ImportFiles(
		[]string{sourceDir, nestedDir},
		"equipment",
		ImportModeText,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 2 || result.OverwrittenCount != 0 {
		t.Fatalf("import result = %#v", result)
	}

	directoryName := filepath.Base(sourceDir)
	strPath := filepath.ToSlash(filepath.Join("equipment", directoryName, "name.str"))
	equPath := filepath.ToSlash(filepath.Join("equipment", directoryName, "nested", "item.equ"))
	strIndex, ok := c.archive.Find(strPath)
	if !ok {
		t.Fatalf("missing imported string file %q", strPath)
	}
	equIndex, ok := c.archive.Find(equPath)
	if !ok {
		t.Fatalf("missing imported script file %q", equPath)
	}
	if c.archive.File(strIndex).DataType != pvf.TypeUnicode {
		t.Fatalf("string data type = %d, want %d", c.archive.File(strIndex).DataType, pvf.TypeUnicode)
	}
	if c.archive.File(equIndex).DataType != pvf.TypeScript {
		t.Fatalf("script data type = %d, want %d", c.archive.File(equIndex).DataType, pvf.TypeScript)
	}
	children, err := service.ListChildren(filepath.ToSlash(filepath.Join("equipment", directoryName)))
	if err != nil {
		t.Fatal(err)
	}
	foundNewKind := false
	for _, child := range children {
		if child.Path == strPath {
			foundNewKind = true
			if child.ChangeKind != ChangeKindAdded {
				t.Fatalf("new file change kind = %q, want %q", child.ChangeKind, ChangeKindAdded)
			}
		}
	}
	if !foundNewKind {
		t.Fatalf("new file tree node missing: %q", strPath)
	}
	if text, err := c.archive.Text(strIndex); err != nil || text != "导入文本" {
		t.Fatalf("string text = %q, err = %v", text, err)
	}
	if text, err := c.archive.Text(equIndex); err != nil || !strings.Contains(text, "导入脚本") {
		t.Fatalf("script text = %q, err = %v", text, err)
	}

	var packed bytes.Buffer
	if err := c.archive.SaveTo(&packed); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := pvf.Parse(packed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.FileCount() != 2 {
		t.Fatalf("round-trip file count = %d, want 2", roundTrip.FileCount())
	}
	roundTripStringIndex, ok := roundTrip.Find(strPath)
	if !ok {
		t.Fatalf("round-trip string file missing: %q", strPath)
	}
	if text, err := roundTrip.Text(roundTripStringIndex); err != nil || text != "导入文本" {
		t.Fatalf("round-trip string text = %q, err = %v", text, err)
	}
}

func TestArchiveServiceImportFilesRawOverwritesAndUpdatesType(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "same.str")
	raw := []byte{0x4b, 0x00, 0x8b, 0x65}
	if err := os.WriteFile(sourcePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText("same.str", "old", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "base.pvf")
	if err := a.SaveAs(archivePath); err != nil {
		t.Fatal(err)
	}
	loaded, err := pvf.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(loaded); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	result, err := NewArchiveService(c).ImportFiles([]string{sourcePath}, "", ImportModeRaw)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 0 || result.OverwrittenCount != 1 || len(result.ChangedPaths) != 1 {
		t.Fatalf("import result = %#v", result)
	}
	index, ok := c.archive.Find("same.str")
	if !ok {
		t.Fatal("overwritten file missing")
	}
	if c.archive.File(index).DataType != pvf.TypeUnicode {
		t.Fatalf("overwritten data type = %d, want %d", c.archive.File(index).DataType, pvf.TypeUnicode)
	}
	got, err := c.archive.RawBytes(index)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("overwritten raw = %v, err = %v", got, err)
	}
	children, err := NewArchiveService(c).ListChildren("")
	if err != nil {
		t.Fatal(err)
	}
	foundModifiedKind := false
	for _, child := range children {
		if child.Path == "same.str" {
			foundModifiedKind = true
			if child.ChangeKind != ChangeKindModified {
				t.Fatalf("overwritten file change kind = %q, want %q", child.ChangeKind, ChangeKindModified)
			}
		}
	}
	if !foundModifiedKind {
		t.Fatal("overwritten file tree node missing")
	}
}

func TestArchiveServiceImportFilesRollsBackOnInvalidText(t *testing.T) {
	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "valid.equ")
	invalidPath := filepath.Join(sourceDir, "invalid.equ")
	if err := os.WriteFile(validPath, []byte("[name]\n`valid`"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, []byte{0xff, 0xfe, 0xfd}, 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText("existing.equ", "[name]\n`existing`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	beforeCount := c.archive.FileCount()
	beforeModified := c.archive.ModifiedCount()
	_, err := NewArchiveService(c).ImportFiles(
		[]string{validPath, invalidPath},
		"",
		ImportModeText,
	)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid text error = %v", err)
	}
	if c.archive.FileCount() != beforeCount || c.archive.ModifiedCount() != beforeModified {
		t.Fatalf("archive changed after failed import: count=%d modified=%d", c.archive.FileCount(), c.archive.ModifiedCount())
	}
	if _, ok := c.archive.Find("valid.equ"); ok {
		t.Fatal("valid file was partially imported")
	}
}

func TestCollectImportFilesRejectsTargetCollision(t *testing.T) {
	left := filepath.Join(t.TempDir(), "same")
	right := filepath.Join(t.TempDir(), "same")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatal(err)
	}
	leftFile := filepath.Join(left, "item.equ")
	rightFile := filepath.Join(right, "item.equ")
	if err := os.WriteFile(leftFile, []byte("left"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rightFile, []byte("right"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := collectImportFiles([]string{left, right}, "")
	if err == nil || !strings.Contains(err.Error(), "批次内归档路径冲突") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestImportNutSourceKeepsSourceText(t *testing.T) {
	source := "// 繁體註解\r\nfunction test() { return [1, 2]; }\r\n"
	path := filepath.Join(t.TempDir(), "common.NUT")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []importFile{{sourcePath: path, targetPath: "sqr/common.NUT"}}
	if err := readImportFiles(files, ImportModeText, ""); err != nil {
		t.Fatal(err)
	}
	a := pvf.New()
	if _, err := applyPreparedImport(a, files, ImportModeText); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := pvf.Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	index, ok := b.Find("sqr/common.nut")
	if !ok {
		t.Fatal("imported NUT missing")
	}
	if b.File(index).DataType != pvf.TypeUnicode {
		t.Fatal("NUT imported as PVF tokens")
	}
	if text, err := b.Text(index); err != nil || text != source {
		t.Fatalf("NUT source changed: %v", err)
	}
}

func TestImportSelectedFilesPreservesMappingAndSkipsUnchecked(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{"keep.str": "不要覆盖", "nested/add.str": "选中的文件"} {
		if err := os.WriteFile(filepath.Join(source, path), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	a := pvf.New()
	root := "target/" + filepath.Base(source)
	if _, err := a.AddFileText(root+"/keep.str", "原内容", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	svc := NewArchiveService(c)
	preview, err := svc.PreviewImport([]string{source}, "target", ImportModeText)
	if err != nil || preview.TotalFiles != 2 {
		t.Fatal("preview failed", err)
	}
	// Even a source changed to invalid text after preview must be ignored if
	// unchecked; source roots must not flatten when selecting nested files.
	if err := os.WriteFile(filepath.Join(source, "keep.str"), []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	selected := root + "/nested/add.str"
	result, err := svc.ImportSelectedFiles([]string{source}, "target", ImportModeText, []string{selected, selected})
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 1 || result.OverwrittenCount != 0 || c.archive.FileCount() != 2 {
		t.Fatalf("incorrect selection result: %+v", result)
	}
	for path, expected := range map[string]string{selected: "选中的文件", root + "/keep.str": "原内容"} {
		index, ok := c.archive.Find(path)
		if !ok {
			t.Fatal("missing mapped file", path)
		}
		if text, err := c.archive.Text(index); err != nil || text != expected {
			t.Fatal("wrong selected content", err)
		}
	}
	before := c.archive
	for _, selection := range [][]string{nil, {}, {root + "/missing.str"}} {
		if _, err := svc.ImportSelectedFiles([]string{source}, "target", ImportModeText, selection); err == nil {
			t.Fatal("accepted empty or stale selection")
		}
		if c.archive != before {
			t.Fatal("invalid selection changed archive")
		}
	}
}

func TestImportPreviewIncludesEverySelectableFile(t *testing.T) {
	source := t.TempDir()
	for i := 0; i < 501; i++ {
		if err := os.WriteFile(filepath.Join(source, fmt.Sprintf("%03d.str", i)), []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	if err := c.setArchive(pvf.New()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	preview, err := NewArchiveService(c).PreviewImport([]string{source}, "", ImportModeText)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalFiles != 501 || len(preview.Entries) != 501 || preview.EntriesTruncated {
		t.Fatal("preview hid selectable files")
	}
}
