package services

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pvfine/internal/pvf"
	scriptengine "pvfine/internal/script"
)

type blockingScriptRuntime struct {
	started chan struct{}
	once    sync.Once
}

func (r *blockingScriptRuntime) Compile(string) scriptengine.CompileResult {
	return scriptengine.CompileResult{Valid: true}
}

func (r *blockingScriptRuntime) Run(ctx context.Context, _ string, _ *scriptengine.BatchAPI) (scriptengine.RunResult, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return scriptengine.RunResult{
		Status: scriptengine.RunStatusCancelled,
		Error: &scriptengine.Diagnostic{
			Kind:    scriptengine.ErrorKindCancelled,
			Message: ctx.Err().Error(),
		},
	}, nil
}

func newScriptTestService(t *testing.T) (*core, *ScriptService, int32, int32) {
	t.Helper()
	c := NewCore()
	a := pvf.New()
	first, err := a.AddFileText("equipment/first.equ", "[price]\n1\n[name]\n`first`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.AddFileText("equipment/second.equ", "[price]\n20\n[name]\n`second`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	svc := newScriptService(c, scriptengine.NewGojaRuntime(), t.TempDir())
	t.Cleanup(c.closeArchive)
	return c, svc, first, second
}

func TestScriptServiceRunPreviewAndApplySelected(t *testing.T) {
	c, svc, first, second := newScriptTestService(t)
	liveFirstBefore, err := c.archive.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	liveSecondBefore, err := c.archive.Text(second)
	if err != nil {
		t.Fatal(err)
	}
	request := ScriptRunRequest{
		Name: "raise-prices",
		Source: `for (const file of pvf.glob("equipment/**/*.equ")) {
  const document = file.parse();
  const price = document.section("price");
  if (price && price.get() < 10) {
    price.set(10);
    file.write(document);
  }
}`,
	}
	result, err := svc.Run(nil, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted || result.PlanID == "" || result.ModifiedFiles != 1 {
		t.Fatalf("run result = %#v", result)
	}

	beforeFirst, err := c.archive.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	if beforeFirst != liveFirstBefore {
		t.Fatalf("preview changed live archive: before=%q after=%q", liveFirstBefore, beforeFirst)
	}
	beforeSecond, err := c.archive.Text(second)
	if err != nil {
		t.Fatal(err)
	}

	page, err := svc.PreviewPage(result.PlanID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.ModifiedFiles != 1 || len(page.Rows) != 1 || page.Rows[0].FileIndex != first {
		t.Fatalf("preview page = %#v", page)
	}
	if page.Rows[0].ChangeKey == "" || page.Rows[0].Status != ScriptFileChanged {
		t.Fatalf("preview row identity = %#v", page.Rows[0])
	}
	if len(page.Rows[0].Diff) == 0 {
		t.Fatal("preview diff is empty")
	}

	apply, err := svc.Apply(result.PlanID, []string{page.Rows[0].ChangeKey})
	if err != nil {
		t.Fatal(err)
	}
	if apply.AppliedFiles != 1 || len(apply.FileIndexes) != 1 || apply.FileIndexes[0] != first {
		t.Fatalf("apply result = %#v", apply)
	}
	afterFirst, err := c.archive.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	if afterFirst == beforeFirst {
		t.Fatal("selected script change was not applied")
	}
	afterSecond, err := c.archive.Text(second)
	if err != nil {
		t.Fatal(err)
	}
	if afterSecond != beforeSecond || beforeSecond != liveSecondBefore {
		t.Fatalf("unselected file changed: before=%q after=%q", beforeSecond, afterSecond)
	}
	if _, err := svc.PreviewPage(result.PlanID, 0, 10); !errors.Is(err, ErrScriptPlanStale) {
		t.Fatalf("applied plan error = %v, want ErrScriptPlanStale", err)
	}
}

func TestScriptServiceFailureDoesNotMutateLiveArchive(t *testing.T) {
	c, svc, first, _ := newScriptTestService(t)
	before, err := c.archive.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(nil, ScriptRunRequest{Source: `
const file = pvf.find("equipment/first.equ");
file.setText("[price]\n999");
throw new Error("abort");
`})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunFailed || result.PlanID != "" || result.Error == nil {
		t.Fatalf("failure result = %#v", result)
	}
	after, err := c.archive.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("failed script mutated live archive: before=%q after=%q", before, after)
	}
}

func TestScriptServicePreviewAndApplyStructuralChanges(t *testing.T) {
	c, svc, _, second := newScriptTestService(t)
	beforeSecond, err := c.archive.Text(second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(nil, ScriptRunRequest{Name: "restructure", Source: `
pvf.createFile("equipment/added.equ", pvf.types.script, "[price]\n7");
pvf.deleteFile("equipment/second.equ");
`})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted || result.PlanID == "" || result.ModifiedFiles != 2 {
		t.Fatalf("run result = %#v", result)
	}
	if _, ok := c.archive.Find("equipment/added.equ"); ok {
		t.Fatal("preview created a live entry")
	}
	if _, ok := c.archive.Find("equipment/second.equ"); !ok {
		t.Fatal("preview deleted a live entry")
	}

	page, err := svc.PreviewPage(result.PlanID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	statusByKey := make(map[string]string, len(page.Rows))
	for _, row := range page.Rows {
		statusByKey[row.ChangeKey] = row.Status
	}
	if statusByKey["equipment/added.equ"] != ScriptFileAdded {
		t.Fatalf("added row status = %q rows=%#v", statusByKey["equipment/added.equ"], page.Rows)
	}
	if statusByKey["equipment/second.equ"] != ScriptFileDeleted {
		t.Fatalf("deleted row status = %q rows=%#v", statusByKey["equipment/second.equ"], page.Rows)
	}

	// Applying only the creation must leave the deletion staged, not applied.
	apply, err := svc.Apply(result.PlanID, []string{"equipment/added.equ"})
	if err != nil {
		t.Fatal(err)
	}
	if apply.AppliedFiles != 1 {
		t.Fatalf("apply result = %#v", apply)
	}
	added, ok := c.archive.Find("equipment/added.equ")
	if !ok {
		t.Fatal("selected creation was not applied")
	}
	if text, textErr := c.archive.Text(added); textErr != nil || !strings.Contains(text, "7") {
		t.Fatalf("created text = %q err=%v", text, textErr)
	}
	if _, ok := c.archive.Find("equipment/second.equ"); !ok {
		t.Fatal("unselected deletion was applied")
	}
	if text, textErr := c.archive.Text(second); textErr != nil || text != beforeSecond {
		t.Fatalf("unselected file changed: %q err=%v", text, textErr)
	}
}

func TestScriptServiceListEditFlowsThroughPreviewAndApply(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/a.equ`",
		pvf.TypeScript,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"equipment/character/a.equ", "equipment/character/b.equ"} {
		if _, err := a.AddFileText(path, "[name]\n`x`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	svc := newScriptService(c, scriptengine.NewGojaRuntime(), t.TempDir())

	listIndex, _ := c.archive.Find("equipment/equipment.lst")
	before, err := c.archive.RawBytes(listIndex)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Run(nil, ScriptRunRequest{Name: "register", Source: `
const lst = pvf.lst("equipment/equipment.lst");
lst.set("1009", "character/b.equ");
`})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScriptRunCompleted || result.PlanID == "" || result.ModifiedFiles != 1 {
		t.Fatalf("run result = %#v", result)
	}
	// The preview must not touch the live list.
	afterRun, err := c.archive.RawBytes(listIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterRun) {
		t.Fatal("preview mutated the live list")
	}

	page, err := svc.PreviewPage(result.PlanID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Path != "equipment/equipment.lst" {
		t.Fatalf("preview rows = %#v", page.Rows)
	}
	// A list edit is an ordinary payload change, not a structural one.
	if page.Rows[0].Status != ScriptFileChanged {
		t.Fatalf("row status = %q", page.Rows[0].Status)
	}

	apply, err := svc.Apply(result.PlanID, []string{page.Rows[0].ChangeKey})
	if err != nil {
		t.Fatal(err)
	}
	if apply.Structural {
		t.Fatal("a list edit should not be reported as structural")
	}
	applied, ok := c.archive.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("list file disappeared")
	}
	pairs, err := c.archive.ListPairs(applied)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[1].ID != "1009" || pairs[1].Path != "character/b.equ" {
		t.Fatalf("applied pairs = %#v", pairs)
	}
}

func TestScriptServiceApplyDeleteRemovesEntryAndRebuildsTree(t *testing.T) {
	c, svc, first, second := newScriptTestService(t)
	result, err := svc.Run(nil, ScriptRunRequest{Source: `
pvf.deleteFile("equipment/second.equ");
pvf.find("equipment/first.equ").setText("[price]\n42\n[name]\n` + "`first`" + `");
`})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID == "" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := svc.Apply(result.PlanID, []string{"equipment/second.equ", "equipment/first.equ"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.archive.Find("equipment/second.equ"); ok {
		t.Fatal("deleted entry still resolves in the live archive")
	}
	// The payload edit must land on the surviving entry even though the
	// deletion renumbered it.
	remaining, ok := c.archive.Find("equipment/first.equ")
	if !ok {
		t.Fatal("surviving entry disappeared")
	}
	if text, textErr := c.archive.Text(remaining); textErr != nil || !strings.Contains(text, "42") {
		t.Fatalf("surviving text = %q err=%v", text, textErr)
	}
	if c.editorText[remaining] == "" {
		t.Fatal("editor text cache was not refreshed for the surviving entry")
	}
	// The tree index must reflect the removal.
	if _, stillThere := c.dirChildren["equipment"]; stillThere {
		for _, node := range c.dirChildren["equipment"] {
			if node.FileIndex == second {
				t.Fatal("removed entry is still present in the tree index")
			}
		}
	}
	_ = first
}

func TestScriptPlanInvalidatedByEditorEdit(t *testing.T) {
	c, svc, first, _ := newScriptTestService(t)
	result, err := svc.Run(nil, ScriptRunRequest{Source: `
const file = pvf.find("equipment/first.equ");
file.setText("[price]\n10");
`})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID == "" {
		t.Fatalf("result = %#v", result)
	}
	if _, _, err := c.setText(first, "[price]\n2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewPage(result.PlanID, 0, 10); !errors.Is(err, ErrScriptPlanStale) {
		t.Fatalf("stale preview error = %v, want ErrScriptPlanStale", err)
	}
}

func TestScriptServiceCancelsOnArchiveCloseAndRejectsConcurrentRun(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText("equipment/first.equ", "[price]\n1", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	runtime := &blockingScriptRuntime{started: make(chan struct{})}
	svc := newScriptService(c, runtime, t.TempDir())
	done := make(chan ScriptRunResult, 1)
	errDone := make(chan error, 1)
	go func() {
		result, err := svc.Run(context.Background(), ScriptRunRequest{Source: "void 0"})
		done <- result
		errDone <- err
	}()
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("script runtime did not start")
	}
	if _, err := svc.Run(context.Background(), ScriptRunRequest{Source: "void 0"}); !errors.Is(err, ErrScriptBusy) {
		t.Fatalf("concurrent run error = %v, want ErrScriptBusy", err)
	}
	c.closeArchive()
	select {
	case result := <-done:
		if result.Status != ScriptRunCancelled {
			t.Fatalf("cancelled result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("script did not stop after archive close")
	}
	if err := <-errDone; err != nil {
		t.Fatal(err)
	}
}

func TestScriptDirectoryValidationAndAtomicSave(t *testing.T) {
	directory := t.TempDir()
	svc := newScriptService(NewCore(), scriptengine.NewGojaRuntime(), directory)
	if err := svc.SaveScript("z.pvf.js", "z"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveScript("a", "a"); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.pvf.js")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkCreated := os.Symlink(external, filepath.Join(directory, "link.pvf.js")) == nil
	if _, err := svc.LoadScript("../escape.pvf.js"); err == nil {
		t.Fatal("path traversal was accepted")
	}
	if _, err := svc.LoadScript("nested/name.pvf.js"); err == nil {
		t.Fatal("nested script path was accepted")
	}
	if linkCreated {
		if _, err := svc.LoadScript("link.pvf.js"); err == nil {
			t.Fatal("script symlink was accepted")
		}
	}
	files, err := svc.ListScripts()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "a.pvf.js" || files[1].Name != "z.pvf.js" {
		t.Fatalf("scripts = %#v", files)
	}
	source, err := svc.LoadScript("a.pvf.js")
	if err != nil || source != "a" {
		t.Fatalf("loaded source = %q, err = %v", source, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("temporary save file left behind: %s", entry.Name())
		}
	}
}
