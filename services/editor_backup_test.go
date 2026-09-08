package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func newEditorSaveFixture(t *testing.T) (string, *core, int32, []byte) {
	t.Helper()

	built := pvf.New()
	index, err := built.AddFileText("test/example.equ", "[name]\n`before`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := built.SaveTo(&encoded); err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.pvf")
	original := encoded.Bytes()
	if err := os.WriteFile(sourcePath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	archive, err := pvf.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	core := NewCore()
	if err := core.setArchive(archive); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.closeArchive)
	return sourcePath, core, index, append([]byte(nil), original...)
}

func TestEditorServiceSaveBacksUpSourceByDefault(t *testing.T) {
	sourcePath, core, index, original := newEditorSaveFixture(t)
	editor := NewEditorService(core)
	if err := editor.SetText(index, "[name]\n`after`\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Save(); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(sourcePath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("source backup does not contain the original archive")
	}
	saved, err := pvf.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	savedIndex, ok := saved.Find("test/example.equ")
	if !ok {
		t.Fatal("saved file is missing")
	}
	text, err := saved.Text(savedIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "after") {
		t.Fatalf("saved text = %q", text)
	}
}

func TestEditorServiceSaveCanDisableSourceBackup(t *testing.T) {
	sourcePath, core, index, _ := newEditorSaveFixture(t)
	settingsService := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	settings := DefaultAppSettings()
	settings.BackupSourceOnSave = false
	if err := settingsService.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	editor := NewEditorService(core, settingsService)
	if err := editor.SetText(index, "[name]\n`after`\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourcePath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("source backup exists or stat failed: %v", err)
	}
}
