package services

import (
	"path/filepath"
	"testing"

	"pvfine/internal/pvf"
)

func TestCollectExportSelectionsExpandsDirectories(t *testing.T) {
	a := pvf.New()
	first, err := a.AddFileText("dir/first.txt", "第一份", pvf.TypeUnicode)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.AddFileText("dir/second.txt", "第二份", pvf.TypeUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("other/third.txt", "第三份", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}

	_, sortedPaths, err := buildIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	selections := collectExportSelections(a, sortedPaths, []string{"dir", "dir/first.txt"})
	if len(selections) != 2 {
		t.Fatalf("selections = %d, want 2", len(selections))
	}
	if selections[0].index != first || selections[1].index != second {
		t.Fatalf("selections = %#v", selections)
	}
	if got, err := a.Text(selections[0].index); err != nil || got != "第一份" {
		t.Fatalf("rendered text = %q, err = %v", got, err)
	}
}

func TestSafeExportPath(t *testing.T) {
	base := t.TempDir()
	got, err := safeExportPath(base, "dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "dir", "file.txt"); got != want {
		t.Fatalf("safe path = %q, want %q", got, want)
	}
	if _, err := safeExportPath(base, "../outside.txt"); err == nil {
		t.Fatal("safeExportPath accepted traversal")
	}
}
