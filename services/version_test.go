package services

import (
	"os"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func versionServiceFixture(t *testing.T) (*core, string, int32) {
	t.Helper()
	path := writeSearchFixture(t, "version.pvf")
	c := NewCore()
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	index, ok := a.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("version fixture file missing")
	}
	t.Cleanup(c.closeArchive)
	return c, path, index
}

func TestVersionServiceCommitSaveHistoryCheckout(t *testing.T) {
	c, _, index := versionServiceFixture(t)
	versions := NewVersionService(c)
	initialStatus, err := versions.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	status := *initialStatus
	if !status.Enabled || status.ChangedFiles != 0 || status.NeedsSave {
		t.Fatalf("initial status = %#v", status)
	}

	if err := NewEditorService(c).SetText(index, "[name]\n`修改后的项链`\n[grade]\n2"); err != nil {
		t.Fatal(err)
	}
	status = versions.Status()
	if status.ChangedFiles != 1 || status.PendingChangeSets != 1 || !status.NeedsSave {
		t.Fatalf("edited status = %#v", status)
	}

	first, err := versions.Commit("修改项链属性")
	if err != nil {
		t.Fatal(err)
	}
	status = versions.Status()
	if first.ChangeCount != 1 || status.ChangedFiles != 0 || !status.NeedsSave {
		t.Fatalf("committed status = %#v commit=%#v", status, first)
	}

	if _, err := NewEditorService(c).Save(); err != nil {
		t.Fatal(err)
	}
	status = versions.Status()
	if status.NeedsSave {
		t.Fatalf("saved status = %#v", status)
	}

	history, err := versions.History(0, 10)
	if err != nil || len(history.Commits) != 2 {
		t.Fatalf("history = %#v err=%v", history, err)
	}
	diff, err := versions.Diff(first.ID, "equipment/character/common/amulet/1008.equ")
	if err != nil {
		t.Fatal(err)
	}
	if !diff.TextAvailable || !strings.Contains(diff.BeforeText, "烈火之心项链") || !strings.Contains(diff.AfterText, "修改后的项链") {
		t.Fatalf("diff = %#v", diff)
	}

	if err := NewEditorService(c).SetText(index, "[name]\n`第三次修改`\n[grade]\n3"); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.Undo(); err != nil {
		t.Fatal(err)
	}
	text, err := c.archive.Text(index)
	if err != nil || !strings.Contains(text, "修改后的项链") {
		t.Fatalf("undo text = %q err=%v", text, err)
	}

	rootID := history.Commits[len(history.Commits)-1].ID
	if _, err := versions.Checkout(rootID); err != nil {
		t.Fatal(err)
	}
	rootIndex, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("checked out file missing")
	}
	text, err = c.archive.Text(rootIndex)
	if err != nil || !strings.Contains(text, "烈火之心项链") {
		t.Fatalf("checkout text = %q err=%v", text, err)
	}
	if versions.Status().HeadID != first.ID || versions.Status().ChangedFiles != 1 {
		t.Fatalf("checkout status = %#v", versions.Status())
	}
	if _, err := versions.Discard(); err != nil {
		t.Fatal(err)
	}
	current, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("discarded file missing")
	}
	text, err = c.archive.Text(current)
	if err != nil || !strings.Contains(text, "修改后的项链") {
		t.Fatalf("discard text = %q err=%v", text, err)
	}
}

func TestVersionServiceStatusTracksIncrementalRevert(t *testing.T) {
	c, _, index := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	original, err := c.archive.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(c).SetText(index, "[name]\n`临时修改`"); err != nil {
		t.Fatal(err)
	}
	status := versions.Status()
	if status.ChangedFiles != 1 || !status.NeedsSave {
		t.Fatalf("modified status = %#v", status)
	}
	if err := NewEditorService(c).SetText(index, original); err != nil {
		t.Fatal(err)
	}
	status = versions.Status()
	if status.ChangedFiles != 0 || status.NeedsSave {
		t.Fatalf("reverted status = %#v", status)
	}
}

func TestVersionServiceRemoveDeletesOnlySidecar(t *testing.T) {
	c, path, _ := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	sidecar := path + ".pvfine"
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar was not created: %v", err)
	}
	status, err := versions.Remove()
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.Enabled {
		t.Fatalf("removed status = %#v", status)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar still exists or stat failed: %v", err)
	}
	if c.archive == nil {
		t.Fatal("removing version control closed the archive")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PVF was removed: %v", err)
	}
}

func TestVersionServiceDiscoversRepositoryOnOpen(t *testing.T) {
	c, path, index := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(c).SetText(index, "[name]\n`持久化版本`\n[grade]\n5"); err != nil {
		t.Fatal(err)
	}
	commit, err := versions.Commit("持久化版本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEditorService(c).Save(); err != nil {
		t.Fatal(err)
	}
	c.closeArchive()

	archiveService := NewArchiveService(c)
	if _, err := archiveService.Open(path); err != nil {
		t.Fatal(err)
	}
	status := NewVersionService(c).Status()
	if !status.Enabled || status.HeadID != commit.ID || status.ChangedFiles != 0 || status.NeedsSave {
		t.Fatalf("reopened status = %#v", status)
	}
	openedIndex, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("reopened file missing")
	}
	text, err := c.archive.Text(openedIndex)
	if err != nil || !strings.Contains(text, "持久化版本") {
		t.Fatalf("reopened text = %q err=%v", text, err)
	}
}

func TestVersionServiceRecoversCommittedWorktreeOnOpen(t *testing.T) {
	c, path, index := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(c).SetText(index, "[name]\n`尚未保存但已提交`\n[grade]\n6"); err != nil {
		t.Fatal(err)
	}
	commit, err := versions.Commit("未保存提交")
	if err != nil {
		t.Fatal(err)
	}
	// The packed PVF remains at the base state. Reopening it should recover
	// the committed logical state into memory without rewriting the file.
	c.closeArchive()
	if _, err := NewArchiveService(c).Open(path); err != nil {
		t.Fatal(err)
	}
	status := NewVersionService(c).Status()
	if status.HeadID != commit.ID || status.ChangedFiles != 0 || !status.NeedsSave {
		t.Fatalf("recovered status = %#v", status)
	}
	openedIndex, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("recovered file missing")
	}
	text, err := c.archive.Text(openedIndex)
	if err != nil || !strings.Contains(text, "尚未保存但已提交") {
		t.Fatalf("recovered text = %q err=%v", text, err)
	}
}

func TestVersionServiceRecordsBatchAsOneChangeSet(t *testing.T) {
	c, _, _ := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	a := c.archive
	first, ok := a.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("first batch file missing")
	}
	second, ok := a.Find("stackable/stackable.lst")
	if !ok {
		t.Fatal("second batch file missing")
	}
	page, err := NewBatchService(c).Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{a.Path(first), a.Path(second)},
		Text:  &TextReplaceSpec{Find: "1008", Replacement: "2008"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBatchService(c).Apply(page.PlanID, []int32{first, second}); err != nil {
		t.Fatal(err)
	}
	status := versions.Status()
	if status.ChangedFiles != 2 || status.PendingChangeSets != 1 {
		t.Fatalf("batch status = %#v", status)
	}
	commit, err := versions.Commit("批量替换编号")
	if err != nil {
		t.Fatal(err)
	}
	if commit.ChangeCount != 2 || versions.Status().ChangedFiles != 0 {
		t.Fatalf("batch commit = %#v status=%#v", commit, versions.Status())
	}
}

func TestVersionServiceCheckoutRestoresAddedFile(t *testing.T) {
	c, _, _ := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	node, err := NewArchiveService(c).CreateFile("custom/generated.stk", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewEditorService(c).SetText(node.FileIndex, "[name]\n`生成文件`"); err != nil {
		t.Fatal(err)
	}
	commit, err := versions.Commit("新增生成文件")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEditorService(c).Save(); err != nil {
		t.Fatal(err)
	}
	history, err := versions.History(0, 10)
	if err != nil || len(history.Commits) != 2 {
		t.Fatalf("history = %#v err=%v", history, err)
	}
	rootID := history.Commits[len(history.Commits)-1].ID
	if _, err := versions.Checkout(rootID); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.archive.Find("custom/generated.stk"); ok {
		t.Fatal("added file remained after root checkout")
	}
	if _, err := versions.Checkout(commit.ID); err != nil {
		t.Fatal(err)
	}
	index, ok := c.archive.Find("custom/generated.stk")
	if !ok {
		t.Fatal("added file missing after commit checkout")
	}
	text, err := c.archive.Text(index)
	if err != nil || !strings.Contains(text, "生成文件") {
		t.Fatalf("added file text = %q err=%v", text, err)
	}
}
