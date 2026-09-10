package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	if len(page.Rows[0].Diff) == 0 {
		t.Fatal("preview diff is empty")
	}

	apply, err := svc.Apply(result.PlanID, []int32{first})
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
