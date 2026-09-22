package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

// Real-archive coverage for the backup slot: a large archive (with real script
// payloads and string pools) has to snapshot without touching the source file
// and replay back into a fresh session.
func TestAutosaveRealArchiveRecovery(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	const target = "equipment/character/common/amulet/100300001.equ"
	const probePath = "zz_test/autosave-probe.stk"
	const replacementScript = "[name]\n`测试物品`\n[grade]\n74\n[rarity]\n3\n"
	// Present in the sample revisions but not part of the assertion: it only
	// exercises the deleted-entry replay when it is there.
	const deletedPath = "aicharacter/aicharacter.kor.str"

	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	appSettings := DefaultAppSettings()
	appSettings.AutosaveEnabled = true
	cachePath := filepath.Join(t.TempDir(), "backup", "autosave.pvf")
	appSettings.AutosavePath = cachePath
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}

	first := NewCore()
	t.Cleanup(first.closeArchive)
	if _, err := first.openArchive(filename); err != nil {
		t.Fatal(err)
	}
	index, ok := first.archive.Find(target)
	if !ok {
		t.Skip("样例缺少目标文件")
	}
	if err := NewEditorService(first).SetText(index, replacementScript); err != nil {
		t.Fatal(err)
	}
	added, err := NewArchiveService(first).CreateFile(probePath, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(first).SetText(added.FileIndex, "[name]\n`缓存探针`"); err != nil {
		t.Fatal(err)
	}
	deleted := ""
	if other, ok := first.archive.Find(deletedPath); ok {
		if _, err := NewArchiveService(first).DeleteFiles([]int32{other}); err != nil {
			t.Fatal(err)
		}
		deleted = deletedPath
	}

	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAutosaveService(first, settings).CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	if info := first.archive.Info(); info.ModifiedCount == 0 {
		t.Fatal("snapshot cleared the unsaved state of the live archive")
	}
	after, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("snapshot wrote to the real archive")
	}
	first.closeArchive()

	second := NewCore()
	t.Cleanup(second.closeArchive)
	recovery := NewAutosaveService(second, settings)
	pending, err := recovery.PendingRecovery()
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || !pending.SourceExists || pending.SourcePath != filename {
		t.Fatalf("pending recovery = %#v", pending)
	}
	if pending.PendingFiles < 2 {
		t.Fatalf("pending files = %d, want the edited and the added entry", pending.PendingFiles)
	}
	if _, err := recovery.Restore(); err != nil {
		t.Fatal(err)
	}

	restored, ok := second.archive.Find(target)
	if !ok {
		t.Fatal("restored archive lost the edited file")
	}
	meta, err := NewEditorService(second).GetFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.Text, "测试物品") || !meta.Modified {
		t.Fatalf("restored file = %#v", meta)
	}
	if _, ok := second.archive.Find(probePath); !ok {
		t.Fatal("restored archive lost the added file")
	}
	if deleted != "" {
		if _, ok := second.archive.Find(deleted); ok {
			t.Fatalf("restored archive kept the deleted file %q", deleted)
		}
	}

	if _, err := recovery.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("backup survived the discard: %v", err)
	}
}
