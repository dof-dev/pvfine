package pvf

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func packedTestArchive(t *testing.T) *Archive {
	t.Helper()
	source := New()
	source.AddFileText("equip/a.equ", scriptText, TypeScript)
	source.AddFileText("equip/b.equ", scriptText, TypeScript)
	var buf bytes.Buffer
	if err := source.SaveTo(&buf); err != nil {
		t.Fatalf("save: %v", err)
	}
	parsed, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return parsed
}

func editKinds(edits []FileMutation) map[string]MutationKind {
	result := make(map[string]MutationKind, len(edits))
	for _, edit := range edits {
		result[edit.Path] = edit.Kind
	}
	return result
}

func TestPendingEditsReportsOverlayAndAddedEntries(t *testing.T) {
	a := packedTestArchive(t)
	if edits := a.PendingEdits(); len(edits) != 0 {
		t.Fatalf("clean archive reported %d edits: %v", len(edits), edits)
	}

	index, ok := a.Find("equip/a.equ")
	if !ok {
		t.Fatal("equip/a.equ missing")
	}
	if err := a.SetText(index, "[name]\n`改过的名字`"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("custom/new.stk", "[name]\n`新文件`", TypeScript); err != nil {
		t.Fatal(err)
	}

	kinds := editKinds(a.PendingEdits())
	if len(kinds) != 2 {
		t.Fatalf("edits = %v, want exactly two", kinds)
	}
	if kinds["equip/a.equ"] != MutationModified {
		t.Errorf("equip/a.equ kind = %q, want %q", kinds["equip/a.equ"], MutationModified)
	}
	if kinds["custom/new.stk"] != MutationAdded {
		t.Errorf("custom/new.stk kind = %q, want %q", kinds["custom/new.stk"], MutationAdded)
	}
	if _, ok := kinds["equip/b.equ"]; ok {
		t.Error("untouched entry reported as edited")
	}

	// Reverting the payload drops the entry from the reported set again.
	a.Revert(index)
	rest := editKinds(a.PendingEdits())
	if len(rest) != 1 || rest["custom/new.stk"] != MutationAdded {
		t.Fatalf("after revert edits = %v, want only the added entry", rest)
	}
}

// Saving a clone is what the timed backup does; the live archive must keep its
// unsaved overlay and never be touched by the write.
func TestCloneSaveAsKeepsLiveEdits(t *testing.T) {
	a := packedTestArchive(t)
	index, _ := a.Find("equip/a.equ")
	if err := a.SetText(index, "[name]\n`备份中的改动`"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("custom/new.stk", "[name]\n`新文件`", TypeScript); err != nil {
		t.Fatal(err)
	}
	before := editKinds(a.PendingEdits())

	dir := t.TempDir()
	backupPath := filepath.Join(dir, "autosave.pvf")
	clone := a.CloneForBatch()
	if err := clone.SaveAs(backupPath); err != nil {
		t.Fatalf("clone save: %v", err)
	}

	if !a.Modified() || a.ModifiedCount() == 0 {
		t.Fatal("live archive lost its unsaved state after the clone write")
	}
	after := editKinds(a.PendingEdits())
	if len(after) != len(before) {
		t.Fatalf("live edits changed: before=%v after=%v", before, after)
	}
	for path, kind := range before {
		if after[path] != kind {
			t.Fatalf("live edit %q kind = %q, want %q", path, after[path], kind)
		}
	}
	if text, err := a.Text(index); err != nil || !strings.Contains(text, "备份中的改动") {
		t.Fatalf("live text = %q err = %v", text, err)
	}

	backup, err := Open(backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	backupIndex, ok := backup.Find("equip/a.equ")
	if !ok {
		t.Fatal("backup lost equip/a.equ")
	}
	text, err := backup.Text(backupIndex)
	if err != nil || !strings.Contains(text, "备份中的改动") {
		t.Fatalf("backup text = %q err = %v", text, err)
	}
	if _, ok := backup.Find("custom/new.stk"); !ok {
		t.Fatal("backup lost the added entry")
	}
}

// A backup cache written outside the client folder has no page key table next
// to it, so opening it has to borrow "sk.dat" from the source directory.
func TestOpenWithSidecarsBorrowsPaged110Keys(t *testing.T) {
	archive, sourcePath := openPaged110Fixture(t)
	cachePath := filepath.Join(t.TempDir(), "autosave.pvf")
	if err := archive.CloneForBatch().SaveAs(cachePath); err != nil {
		t.Fatalf("clone save: %v", err)
	}
	if _, err := Open(cachePath); err == nil {
		t.Fatal("cache opened without its page keys")
	}
	backup, err := OpenWithSidecars(cachePath, filepath.Dir(sourcePath))
	if err != nil {
		t.Fatalf("open with borrowed page keys: %v", err)
	}
	if !backup.IsPaged110() {
		t.Fatal("backup is not recognised as Paged110")
	}
	if backup.FileCount() != archive.FileCount() {
		t.Fatalf("file count = %d, want %d", backup.FileCount(), archive.FileCount())
	}
	if edits := backup.PendingEdits(); len(edits) != 0 {
		t.Errorf("freshly written backup reported %d pending edits", len(edits))
	}
}

func TestOpenWithSidecarsMatchesOpen(t *testing.T) {
	source := New()
	source.AddFileText("equip/a.equ", scriptText, TypeScript)
	source.AddFile("bin/data.bin", []byte{1, 2, 3}, TypeScript)
	source.AddFile("str/korean.str", utf16le("한국스크립트"), TypeUnicode)

	dir := t.TempDir()
	path := filepath.Join(dir, "Script.pvf")
	if err := source.SaveAs(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	adjacent, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	borrowed, err := OpenWithSidecars(path, t.TempDir())
	if err != nil {
		t.Fatalf("open with sidecars: %v", err)
	}
	if adjacent.FileCount() != borrowed.FileCount() {
		t.Fatalf("file count = %d, want %d", borrowed.FileCount(), adjacent.FileCount())
	}
	if borrowed.SourcePath() != path {
		t.Errorf("source path = %q, want %q", borrowed.SourcePath(), path)
	}
	paths := func(a *Archive) []string {
		result := make([]string, 0, a.FileCount())
		for i := int32(0); i < a.FileCount(); i++ {
			result = append(result, a.Path(i))
		}
		sort.Strings(result)
		return result
	}
	want, got := paths(adjacent), paths(borrowed)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source file vanished: %v", err)
	}
	if edits := borrowed.PendingEdits(); len(edits) != 0 {
		t.Errorf("freshly opened archive reported %d pending edits", len(edits))
	}
}
