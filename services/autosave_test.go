package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

const autosaveScriptPath = "test/example.equ"
const autosaveKeepPath = "test/keep.equ"

// newAutosaveFixture writes a synthetic source archive and settings that point
// the backup slot at a nested temporary directory (so the directory creation
// path is covered too).
func newAutosaveFixture(t *testing.T) (sourcePath string, settings *SettingsService, cachePath string) {
	t.Helper()
	built := pvf.New()
	if _, err := built.AddFileText(autosaveScriptPath, "[name]\n`before`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := built.AddFileText(autosaveKeepPath, "[name]\n`keep`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := built.SaveTo(&encoded); err != nil {
		t.Fatal(err)
	}
	sourcePath = filepath.Join(t.TempDir(), "Script.pvf")
	if err := os.WriteFile(sourcePath, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	settings = newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	appSettings := DefaultAppSettings()
	appSettings.AutosaveEnabled = true
	cachePath = filepath.Join(t.TempDir(), "backup", "autosave.pvf")
	appSettings.AutosavePath = cachePath
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}
	return sourcePath, settings, cachePath
}

func openAutosaveCore(t *testing.T, sourcePath string) *core {
	t.Helper()
	core := NewCore()
	t.Cleanup(core.closeArchive)
	if _, err := core.openArchive(sourcePath); err != nil {
		t.Fatal(err)
	}
	return core
}

func fixtureIndex(t *testing.T, c *core, path string) int32 {
	t.Helper()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.archive == nil {
		t.Fatal("no archive loaded")
	}
	index, ok := c.archive.Find(path)
	if !ok {
		t.Fatalf("path %q missing from the archive", path)
	}
	return index
}

func fixtureInfo(t *testing.T, c *core) ArchiveInfo {
	t.Helper()
	info, ok := c.archiveInfo()
	if !ok {
		t.Fatal("no archive loaded")
	}
	return info
}

func readFixtureBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAutosaveSnapshotKeepsWorkspaceDirty(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)
	editor := NewEditorService(core)

	if err := editor.SetText(fixtureIndex(t, core, autosaveScriptPath), "[name]\n`after`"); err != nil {
		t.Fatal(err)
	}
	original := readFixtureBytes(t, sourcePath)

	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}

	if info := fixtureInfo(t, core); info.ModifiedCount == 0 {
		t.Fatal("workspace lost its unsaved state after the snapshot")
	}
	if !bytes.Equal(original, readFixtureBytes(t, sourcePath)) {
		t.Fatal("snapshot wrote to the source file")
	}

	backup, err := pvf.Open(cachePath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	index, ok := backup.Find(autosaveScriptPath)
	if !ok {
		t.Fatal("backup lost the edited file")
	}
	text, err := backup.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "after") {
		t.Fatalf("backup text = %q", text)
	}

	meta, ok := readAutosaveMeta(cachePath)
	if !ok {
		t.Fatal("backup metadata missing")
	}
	if meta.SourcePath != sourcePath {
		t.Fatalf("metadata source = %q, want %q", meta.SourcePath, sourcePath)
	}
	if len(meta.ModifiedPaths) != 1 || meta.ModifiedPaths[0] != autosaveScriptPath {
		t.Fatalf("metadata modified paths = %v", meta.ModifiedPaths)
	}
	if len(meta.AddedPaths) != 0 {
		t.Fatalf("metadata added paths = %v, want none", meta.AddedPaths)
	}
	if meta.SourceSize == 0 || meta.SourceModified == 0 || meta.CachedAt == 0 {
		t.Fatalf("metadata source identity = %#v", meta)
	}
}

func TestAutosaveSkipsCleanWorkspace(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)

	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("clean workspace wrote a backup: %v", err)
	}
}

func TestAutosaveRestoreReplaysInterruptedSession(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)

	// First session: edit one file, add one, delete one, then snapshot. The
	// process is "killed" by dropping the core without saving.
	first := openAutosaveCore(t, sourcePath)
	firstService := NewAutosaveService(first, settings)
	if err := NewEditorService(first).SetText(fixtureIndex(t, first, autosaveScriptPath), "[name]\n`recovered`"); err != nil {
		t.Fatal(err)
	}
	archives := NewArchiveService(first)
	added, err := archives.CreateFile("custom/new.stk", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(first).SetText(added.FileIndex, "[name]\n`新增文件`"); err != nil {
		t.Fatal(err)
	}
	if _, err := archives.DeleteFiles([]int32{fixtureIndex(t, first, autosaveKeepPath)}); err != nil {
		t.Fatal(err)
	}
	if err := firstService.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	first.closeArchive()

	// Second session: nothing is open yet, exactly like a fresh start.
	second := NewCore()
	t.Cleanup(second.closeArchive)
	recovery := NewAutosaveService(second, settings)
	pending, err := recovery.PendingRecovery()
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil {
		t.Fatal("no pending recovery reported")
	}
	if pending.SourcePath != sourcePath || !pending.SourceExists {
		t.Fatalf("pending = %#v", pending)
	}
	if pending.SourceChanged {
		t.Fatal("source reported as changed right after the snapshot")
	}
	if pending.PendingFiles != 2 {
		t.Fatalf("pending files = %d, want 2", pending.PendingFiles)
	}

	info, err := recovery.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if info.Path != sourcePath {
		t.Fatalf("restored path = %q, want %q", info.Path, sourcePath)
	}
	if info.ModifiedCount == 0 {
		t.Fatal("restored workspace is reported as saved")
	}

	meta, _ := NewEditorService(second).GetFile(fixtureIndex(t, second, autosaveScriptPath))
	if !strings.Contains(meta.Text, "recovered") || !meta.Modified {
		t.Fatalf("restored file = %#v", meta)
	}
	if _, ok := second.archive.Find("custom/new.stk"); !ok {
		t.Fatal("restored workspace lost the added file")
	}
	if _, ok := second.archive.Find(autosaveKeepPath); ok {
		t.Fatal("restored workspace kept a file deleted before the snapshot")
	}

	// The backup survives a successful restore: only a save clears it.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("backup should be kept after restore: %v", err)
	}

	// Saving writes the recovered edits back to the source and drops the slot.
	if _, err := NewEditorService(second).Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := pvf.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	savedIndex, ok := saved.Find(autosaveScriptPath)
	if !ok {
		t.Fatal("saved archive lost the edited file")
	}
	text, err := saved.Text(savedIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "recovered") {
		t.Fatalf("saved text = %q", text)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("backup survived the save: %v", err)
	}
	if _, err := os.Stat(cachePath + ".json"); !os.IsNotExist(err) {
		t.Fatalf("backup metadata survived the save: %v", err)
	}
}

// The recovery has to land on an attached version session, otherwise the
// panel would keep reporting a clean workspace while the overlay holds edits.
func TestAutosaveRestoreFeedsVersionSession(t *testing.T) {
	sourcePath := writeSearchFixture(t, "autosave-version.pvf")
	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	appSettings := DefaultAppSettings()
	appSettings.AutosaveEnabled = true
	cachePath := filepath.Join(t.TempDir(), "autosave.pvf")
	appSettings.AutosavePath = cachePath
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}

	first := openAutosaveCore(t, sourcePath)
	versions := NewVersionService(first)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	index := fixtureIndex(t, first, "equipment/character/common/amulet/1008.equ")
	if err := NewEditorService(first).SetText(index, "[name]\n`缓存恢复的项链`\n[grade]\n9"); err != nil {
		t.Fatal(err)
	}
	if err := NewAutosaveService(first, settings).CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	first.closeArchive()

	second := NewCore()
	t.Cleanup(second.closeArchive)
	recovery := NewAutosaveService(second, settings)
	if _, err := recovery.Restore(); err != nil {
		t.Fatal(err)
	}
	status := NewVersionService(second).Status()
	if !status.Enabled || status.Loading {
		t.Fatalf("version status after restore = %#v", status)
	}
	if status.ChangedFiles != 1 || !status.NeedsSave {
		t.Fatalf("version status lost the recovered edit: %#v", status)
	}
	if status.PendingChangeSets != 1 {
		t.Fatalf("recovery should be undoable once: %#v", status)
	}
}

// A commit without saving the PVF leaves HEAD ahead of the file on disk. The
// recovery opens that file (the session materializes HEAD into it) and then has
// to keep the backed-up edit, both in the overlay and in the version state.
func TestAutosaveRestoreReplaysCommittedUnsavedEdit(t *testing.T) {
	sourcePath := writeSearchFixture(t, "autosave-committed.pvf")
	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	appSettings := DefaultAppSettings()
	appSettings.AutosaveEnabled = true
	cachePath := filepath.Join(t.TempDir(), "autosave.pvf")
	appSettings.AutosavePath = cachePath
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}
	const target = "equipment/character/common/amulet/1008.equ"
	const edited = "[name]\n`缓存恢复的项链`\n[grade]\n9"

	first := openAutosaveCore(t, sourcePath)
	versions := NewVersionService(first)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(first).SetText(fixtureIndex(t, first, target), edited); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.Commit("编辑项链"); err != nil {
		t.Fatal(err)
	}
	// The PVF itself is still the old revision on disk while HEAD already holds
	// the edit, exactly what a crash before saving leaves behind.
	if err := NewAutosaveService(first, settings).CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	first.closeArchive()

	second := NewCore()
	t.Cleanup(second.closeArchive)
	recovery := NewAutosaveService(second, settings)
	if _, err := recovery.Restore(); err != nil {
		t.Fatal(err)
	}
	meta, err := NewEditorService(second).GetFile(fixtureIndex(t, second, target))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.Text, "缓存恢复的项链") || !meta.Modified {
		t.Fatalf("restored file = %#v", meta)
	}
}

func TestAutosaveRestoreRefusesDirtyWorkspace(t *testing.T) {
	sourcePath, settings, _ := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)
	if err := NewEditorService(core).SetText(fixtureIndex(t, core, autosaveScriptPath), "[name]\n`still editing`"); err != nil {
		t.Fatal(err)
	}
	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(); err == nil {
		t.Fatal("expected restore to refuse a workspace with unsaved edits")
	}
	if info := fixtureInfo(t, core); info.ModifiedCount == 0 {
		t.Fatal("workspace lost its edits to a refused restore")
	}
}

func TestAutosaveRestoreKeepsBackupWhenSourceMissing(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	first := openAutosaveCore(t, sourcePath)
	snapshotter := NewAutosaveService(first, settings)
	if err := NewEditorService(first).SetText(fixtureIndex(t, first, autosaveScriptPath), "[name]\n`gone`"); err != nil {
		t.Fatal(err)
	}
	if err := snapshotter.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	first.closeArchive()
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}

	fresh := NewCore()
	t.Cleanup(fresh.closeArchive)
	recovery := NewAutosaveService(fresh, settings)
	pending, err := recovery.PendingRecovery()
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.SourceExists {
		t.Fatalf("pending = %#v, want a backup with a missing source", pending)
	}
	if _, err := recovery.Restore(); err == nil {
		t.Fatal("expected restore to fail without the source file")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("backup should be kept for a later retry: %v", err)
	}
}

func TestAutosaveDiscardRemovesSlot(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)
	if err := NewEditorService(core).SetText(fixtureIndex(t, core, autosaveScriptPath), "[name]\n`discarded`"); err != nil {
		t.Fatal(err)
	}
	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}

	removed, err := service.Discard()
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("discard reported nothing removed")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("backup survived the discard: %v", err)
	}
	if _, err := os.Stat(cachePath + ".json"); !os.IsNotExist(err) {
		t.Fatalf("metadata survived the discard: %v", err)
	}
	if service.Status().Exists {
		t.Fatal("status still reports a backup")
	}
}

func TestAutosaveDropForSourceIgnoresOtherArchives(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)
	if err := NewEditorService(core).SetText(fixtureIndex(t, core, autosaveScriptPath), "[name]\n`kept`"); err != nil {
		t.Fatal(err)
	}
	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}

	service.DropForSource(filepath.Join(t.TempDir(), "Other.pvf"))
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("backup of another archive was dropped: %v", err)
	}

	service.DropForSource(sourcePath)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("backup survived the source drop: %v", err)
	}
}

func TestAutosaveStatusReportsSlot(t *testing.T) {
	sourcePath, settings, cachePath := newAutosaveFixture(t)
	core := openAutosaveCore(t, sourcePath)
	service := NewAutosaveService(core, settings)

	status := service.Status()
	if !status.Enabled {
		t.Fatal("status lost the enabled flag")
	}
	if status.Path != cachePath {
		t.Fatalf("status path = %q, want %q", status.Path, cachePath)
	}
	if status.Exists {
		t.Fatal("status reports a backup before the first snapshot")
	}
	if status.DefaultPath == "" {
		t.Fatal("status is missing the default backup path")
	}

	if err := NewEditorService(core).SetText(fixtureIndex(t, core, autosaveScriptPath), "[name]\n`status`"); err != nil {
		t.Fatal(err)
	}
	if err := service.CreateSnapshot(); err != nil {
		t.Fatal(err)
	}
	status = service.Status()
	if !status.Exists || status.CachedAt == 0 || status.SizeBytes == 0 {
		t.Fatalf("status after snapshot = %#v", status)
	}
	if status.SourcePath != sourcePath {
		t.Fatalf("status source = %q, want %q", status.SourcePath, sourcePath)
	}
	if status.LastError != "" {
		t.Fatalf("status error = %q, want none", status.LastError)
	}
}

func TestAutosaveSnapshotRequiresArchive(t *testing.T) {
	_, settings, cachePath := newAutosaveFixture(t)
	core := NewCore()
	t.Cleanup(core.closeArchive)
	service := NewAutosaveService(core, settings)

	if err := service.CreateSnapshot(); err == nil {
		t.Fatal("expected an error without an open archive")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("backup written without an archive: %v", err)
	}
}
