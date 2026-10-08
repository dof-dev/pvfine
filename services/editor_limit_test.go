package services

import (
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestEditorFileSizeLimit(t *testing.T) {
	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	a := pvf.New()
	boundaryText := strings.Repeat("a", (1<<20)/2)
	boundary := mustAddText(t, a, "test/boundary.str", boundaryText, pvf.TypeUnicode)
	largeText := boundaryText + "a"
	large := mustAddText(t, a, "test/large.str", largeText, pvf.TypeUnicode)
	c := NewCore(settings)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	editor := NewEditorService(c, settings)
	getters := []struct {
		name string
		get  func(int32) (*FileMeta, error)
	}{{"basic", editor.GetFileBasic}, {"full", editor.GetFile}}
	check := func(index int32, editable bool, text string) {
		t.Helper()
		for _, getter := range getters {
			meta, err := getter.get(index)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Editable != editable {
				t.Fatalf("%s editable = %t, want %t", getter.name, meta.Editable, editable)
			}
			if editable && meta.Text != text {
				t.Fatalf("%s text mismatch", getter.name)
			}
			if !editable && !strings.Contains(meta.Text, "1048576 字节") {
				t.Fatalf("%s missing configured limit in hint", getter.name)
			}
		}
	}
	// Without a saved setting the original 8 MB default still applies.
	check(large, true, largeText)
	configured := DefaultAppSettings()
	configured.MaxEditableSizeMB = 1
	if err := settings.SaveSettings(configured); err != nil {
		t.Fatal(err)
	}
	check(boundary, true, boundaryText)
	check(large, false, "")
	// Existing services pick up a changed setting without being recreated.
	configured.MaxEditableSizeMB = 2
	if err := settings.SaveSettings(configured); err != nil {
		t.Fatal(err)
	}
	check(large, true, largeText)
}

func TestEditableByteLimitFallback(t *testing.T) {
	services := []*SettingsService{nil, {initErr: ErrNoArchive}}
	for _, service := range services {
		if got := editableByteLimit(service); got != 8<<20 {
			t.Fatalf("fallback = %d, want 8 MB", got)
		}
	}
}
