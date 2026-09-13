package services

import (
	"path/filepath"
	"testing"

	"pvfine/internal/pvf"
	scriptengine "pvfine/internal/script"
)

// newFileSetScriptService wires a script service whose file sets persist to a
// temp file, returning the service pair so tests can inspect the document.
func newFileSetScriptService(t *testing.T) (*core, *ScriptService, *FileSetService) {
	t.Helper()
	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText("equipment/first.equ", "[price]\n1", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("equipment/second.equ", "[price]\n20", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	fileSets := newFileSetService(filepath.Join(t.TempDir(), "file-sets.json"))
	svc := newScriptService(c, scriptengine.NewGojaRuntime(), t.TempDir(), fileSets)
	t.Cleanup(c.closeArchive)
	return c, svc, fileSets
}

func TestScriptServiceFileSetRunPreviewsWithoutPersisting(t *testing.T) {
	_, svc, fileSets := newFileSetScriptService(t)

	// The built-in default set always exists, so it is readable without a prior
	// save and is written through setAll rather than createFileset.
	result, err := svc.Run(nil, ScriptRunRequest{
		Name: "build-set",
		Source: `
			const set = pvf.fileset("默认文件集");
			if (!set) throw new Error("default set should always exist");
			if (set.getAll().length !== 0) throw new Error("default set should start empty");
			set.setAll(["equipment/first.equ", "equipment/second.equ"]);
			set.setAll(["equipment/first.equ"]);
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted {
		t.Fatalf("run result = %#v", result)
	}
	if result.PlanID == "" || result.FileSetChanges != 1 {
		t.Fatalf("plan/file set counts = %#v", result)
	}
	// Running must not touch user config: the change is staged until apply.
	document, err := fileSets.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.FileSets) != 0 {
		t.Fatalf("run persisted file sets = %#v", document.FileSets)
	}

	page, err := svc.PreviewPage(result.PlanID, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.FileSetRows) != 1 || page.FileSetSelectKey == "" {
		t.Fatalf("preview page = %#v", page)
	}
	row := page.FileSetRows[0]
	// The default set exists in the baseline, so the change is an edit, not a
	// create, and the diff shows the surviving path only.
	if row.Name != "默认文件集" || row.Status != ScriptFileSetChanged || row.Count != 1 {
		t.Fatalf("file set row = %#v", row)
	}
	if len(row.Added) != 1 || row.Added[0] != "equipment/first.equ" {
		t.Fatalf("file set added = %#v", row.Added)
	}

	keys, err := svc.SelectableChangeKeys(result.PlanID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != page.FileSetSelectKey {
		t.Fatalf("selectable keys = %#v", keys)
	}

	applied, err := svc.Apply(result.PlanID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if applied.AppliedFileSets != 1 || applied.AppliedFiles != 0 {
		t.Fatalf("apply result = %#v", applied)
	}
	document, err = fileSets.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.FileSets) != 1 {
		t.Fatalf("persisted file sets = %#v", document.FileSets)
	}
	persisted := document.FileSets[0]
	// The id must be the reserved one, otherwise the sidebar shows the set twice.
	if persisted.Name != "默认文件集" || persisted.ID != defaultFileSetID {
		t.Fatalf("persisted set = %#v", persisted)
	}
	if len(persisted.Entries) != 1 || persisted.Entries[0].Path != "equipment/first.equ" {
		t.Fatalf("persisted entries = %#v", persisted.Entries)
	}
}

func TestScriptServiceFileSetDefaultSetIsReadableAndReserved(t *testing.T) {
	_, svc, _ := newFileSetScriptService(t)
	result, err := svc.Run(nil, ScriptRunRequest{
		Source: `
			if (pvf.fileset("默认文件集") === null) throw new Error("default set missing");
			// The default set already exists, so creating it again must fail
			// rather than silently producing a second set with the same name.
			let failed = false;
			try { pvf.createFileset("默认文件集"); } catch (error) { failed = true; }
			if (!failed) throw new Error("duplicate default set was allowed");
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted {
		t.Fatalf("run result = %#v error=%#v", result, result.Error)
	}
	if result.FileSetChanges != 0 {
		t.Fatalf("file set changes = %d, want 0", result.FileSetChanges)
	}
}

func TestScriptServiceFileSetPersistedDefaultKeepsItsName(t *testing.T) {
	_, svc, fileSets := newFileSetScriptService(t)
	// The reserved "default" slot is the sidebar's built-in set. When it was
	// saved under a custom name, that name is what both the sidebar and a script
	// see — including the fact that "默认文件集" no longer exists.
	if err := fileSets.SaveFileSets(FileSetDocument{
		Version:     fileSetDocumentVersion,
		ActiveSetID: defaultFileSetID,
		FileSets: []StoredFileSet{
			{ID: defaultFileSetID, Name: "我的常用", Entries: []StoredFileSetEntry{
				{Path: "equipment/first.equ", Name: "first", Size: 10, DataType: 1},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Run(nil, ScriptRunRequest{
		Source: `
			// 磁盘上的名字就是脚本看到的名字。
			const named = pvf.fileset("我的常用");
			if (!named) throw new Error("persisted default not readable by its own name");
			if (named.getAll().join("|") !== "equipment/first.equ") throw new Error("wrong persisted paths: " + named.getAll());
			// 侧边栏不再显示「默认文件集」，脚本侧同样取不到，避免出现 UI 上没有的名字。
			if (pvf.fileset("默认文件集") !== null) throw new Error("phantom default set was exposed");
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted {
		t.Fatalf("run result = %#v error=%#v", result, result.Error)
	}
	if result.FileSetChanges != 0 {
		t.Fatalf("read-only run staged changes = %d", result.FileSetChanges)
	}
}

func TestScriptServiceFileSetRenamePersistsUnderNewName(t *testing.T) {
	_, svc, fileSets := newFileSetScriptService(t)
	// setAll on the built-in default writes the reserved id, so the next reload
	// shows it under the same name the script used.
	result, err := svc.Run(nil, ScriptRunRequest{
		Source: `pvf.fileset("默认文件集").setAll(["equipment/second.equ"]);`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(result.PlanID, []string{fileSetChangeKey}); err != nil {
		t.Fatal(err)
	}
	document, err := fileSets.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.FileSets) != 1 || document.FileSets[0].ID != defaultFileSetID || document.FileSets[0].Name != defaultFileSetName {
		t.Fatalf("persisted default = %#v", document.FileSets)
	}

	// Re-running sees the paths it just wrote, under the same name.
	result, err = svc.Run(nil, ScriptRunRequest{
		Source: `
			const set = pvf.fileset("默认文件集");
			if (set.getAll().join("|") !== "equipment/second.equ") throw new Error("round trip lost paths: " + set.getAll());
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted {
		t.Fatalf("second run = %#v error=%#v", result, result.Error)
	}
	if result.FileSetChanges != 0 {
		t.Fatalf("read-only run staged changes = %d", result.FileSetChanges)
	}
}

func TestScriptServiceFileSetApplyCanBeSkipped(t *testing.T) {
	c, svc, fileSets := newFileSetScriptService(t)

	result, err := svc.Run(nil, ScriptRunRequest{
		Source: `
			pvf.createFileset("脚本集", ["equipment/first.equ"]);
			const file = pvf.find("equipment/first.equ");
			const document = file.parse();
			document.set("price", 99);
			file.write(document);
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.PreviewPage(result.PlanID, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || len(page.FileSetRows) != 1 {
		t.Fatalf("preview page rows=%d fileSets=%d", len(page.Rows), len(page.FileSetRows))
	}

	// Applying only the archive row must leave the file set untouched.
	applied, err := svc.Apply(result.PlanID, []string{page.Rows[0].ChangeKey})
	if err != nil {
		t.Fatal(err)
	}
	if applied.AppliedFileSets != 0 {
		t.Fatalf("skipped apply wrote file sets: %#v", applied)
	}
	if _, ok := c.archive.Find("equipment/first.equ"); !ok {
		t.Fatal("archive change was not applied")
	}
	document, err := fileSets.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.FileSets) != 0 {
		t.Fatalf("file sets persisted despite being unselected = %#v", document.FileSets)
	}
}

func TestScriptServiceFileSetApplyRejectsUnknownKey(t *testing.T) {
	_, svc, _ := newFileSetScriptService(t)
	result, err := svc.Run(nil, ScriptRunRequest{Source: `pvf.createFileset("a");`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(result.PlanID, []string{fileSetChangeKey}); err != nil {
		t.Fatalf("file set apply failed: %v", err)
	}
	// The plan is consumed by apply, so a second apply is stale.
	if _, err := svc.Apply(result.PlanID, []string{fileSetChangeKey}); err == nil {
		t.Fatal("reapplying a consumed plan should fail")
	}
}

func TestScriptServiceFileSetEditsExistingPersistedSet(t *testing.T) {
	_, svc, fileSets := newFileSetScriptService(t)
	if err := fileSets.SaveFileSets(FileSetDocument{
		Version:     fileSetDocumentVersion,
		ActiveSetID: "set-7",
		FileSets: []StoredFileSet{
			{ID: "set-7", Name: "已有文件集", Entries: []StoredFileSetEntry{
				{Path: "equipment/first.equ", Name: "first", IDs: []string{"1008"}, Size: 10, DataType: 1},
				{Path: "equipment/second.equ", Name: "second", IDs: []string{"1009"}, Size: 20, DataType: 1},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Run(nil, ScriptRunRequest{
		Source: `
			const set = pvf.fileset("已有文件集");
			if (!set) throw new Error("persisted set not visible to the script");
			if (set.getAll().length !== 2) throw new Error("wrong starting paths");
			set.setAll(["equipment/second.equ"]);
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted {
		t.Fatalf("run result = %#v", result)
	}
	page, err := svc.PreviewPage(result.PlanID, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.FileSetRows) != 1 {
		t.Fatalf("file set rows = %#v", page.FileSetRows)
	}
	row := page.FileSetRows[0]
	if row.Status != ScriptFileSetChanged {
		t.Fatalf("status = %q", row.Status)
	}
	if len(row.Removed) != 1 || row.Removed[0] != "equipment/first.equ" {
		t.Fatalf("removed = %#v", row.Removed)
	}
	if len(row.Added) != 0 {
		t.Fatalf("added = %#v", row.Added)
	}

	if _, err := svc.Apply(result.PlanID, []string{fileSetChangeKey}); err != nil {
		t.Fatal(err)
	}
	document, err := fileSets.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if document.ActiveSetID != "set-7" {
		t.Fatalf("active set id changed: %q", document.ActiveSetID)
	}
	// The persisted id survives, and the surviving entry keeps its metadata.
	if len(document.FileSets) != 1 || document.FileSets[0].ID != "set-7" {
		t.Fatalf("persisted sets = %#v", document.FileSets)
	}
	entries := document.FileSets[0].Entries
	if len(entries) != 1 || entries[0].IDs[0] != "1009" || entries[0].Size != 20 {
		t.Fatalf("surviving entry metadata = %#v", entries)
	}
}
