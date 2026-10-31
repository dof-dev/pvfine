package services

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pvfine/internal/mcpserver"
	"strings"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func newMCPFixture(t *testing.T) *MCPService {
	t.Helper()
	c, script, _, _ := newScriptTestService(t)
	s := newMCPService(c, NewArchiveService(c), NewEditorService(c), script, NewExportService(c), filepath.Join(t.TempDir(), "mcp.json"))
	s.workspace()
	t.Cleanup(func() { _ = s.stop() })
	return s
}
func mcpCurrent(s *MCPService) mcpArgs {
	s.workspace()
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return mcpArgs{WorkspaceID: s.c.mcpWorkspaceID, ExpectedRevision: s.c.batchRevision, ExpectedWorkspaceEpoch: s.c.mcpEpoch, Limit: 50}
}
func mcpCall(t *testing.T, s *MCPService, name string, a mcpArgs) MCPTask {
	t.Helper()
	if a.RequestID == "" {
		a.RequestID = mcpID()
	}
	raw, _ := json.Marshal(a)
	value, err := s.call(context.Background(), name, raw, a)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(map[string]any)["taskId"].(string)
	return waitMCPTask(t, s, id)
}
func waitMCPTask(t *testing.T, s *MCPService, id string) MCPTask {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		value, err := s.task(id)
		if err != nil {
			t.Fatal(err)
		}
		task := value.(MCPTask)
		if task.Status != "running" {
			return task
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("MCP task timed out")
	return MCPTask{}
}
func requireMCPCompleted(t *testing.T, task MCPTask) {
	t.Helper()
	if task.Status != "completed" {
		t.Fatalf("task %s: %s", task.Kind, task.Error)
	}
}
func TestMCPScriptPreviewApplyAndWorkspaceConflict(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.Source = "const f=pvf.find('equipment/first.equ');const d=f.parse();d.set('price',7);f.write(d);"
	task := mcpCall(t, s, "script_run", a)
	requireMCPCompleted(t, task)
	r := task.Result.(ScriptRunResult)
	if r.PlanID == "" {
		t.Fatal("missing plan")
	}
	p, err := s.script.PreviewPage(r.PlanID, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.script.Run(context.Background(), ScriptRunRequest{Source: "pvf.log('other');"}); err == nil || !strings.Contains(err.Error(), "PLAN_PENDING") {
		t.Fatal("UI run overwrote AI preview")
	}
	apply := mcpCurrent(s)
	apply.PlanID = r.PlanID
	apply.ChangeKeys = []string{p.Rows[0].ChangeKey}
	done := mcpCall(t, s, "script_apply", apply)
	requireMCPCompleted(t, done)
	s.c.mu.Lock()
	index, _ := s.c.archive.Find("equipment/first.equ")
	text, _ := s.c.archive.Text(index)
	s.c.mu.Unlock()
	if !strings.Contains(text, "7") {
		t.Fatal("change missing")
	}
	if s.check(a) == nil {
		t.Fatal("old revision accepted")
	}
	query := mcpCurrent(s)
	query.Source = "pvf.log(pvf.find('equipment/first.equ').path);"
	q := mcpCall(t, s, "script_run", query)
	requireMCPCompleted(t, q)
	if q.Result.(ScriptRunResult).PlanID != "" {
		t.Fatal("query retained empty plan")
	}
}
func TestMCPImportFrozenSelectionAndOverwrite(t *testing.T) {
	s := newMCPFixture(t)
	root := t.TempDir()
	file := filepath.Join(root, "first.equ")
	if err := os.WriteFile(file, []byte("[price]\n99"), 0600); err != nil {
		t.Fatal(err)
	}
	a := mcpCurrent(s)
	a.SourcePaths = []string{file}
	a.TargetDir = "equipment"
	a.Mode = "text"
	prep := mcpCall(t, s, "import_prepare", a)
	requireMCPCompleted(t, prep)
	id := prep.Result.(map[string]any)["importPlanId"].(string)
	if err := os.WriteFile(file, []byte("[price]\n1000"), 0600); err != nil {
		t.Fatal(err)
	}
	a = mcpCurrent(s)
	a.ImportPlanID = id
	a.SelectedKeys = []string{"equipment/first.equ"}
	rejected := mcpCall(t, s, "import_apply", a)
	if !strings.Contains(rejected.Error, "OVERWRITE_REQUIRED") {
		t.Fatal("implicit overwrite accepted")
	}
	a.OverwriteKeys = a.SelectedKeys
	done := mcpCall(t, s, "import_apply", a)
	requireMCPCompleted(t, done)
	s.c.mu.Lock()
	i, _ := s.c.archive.Find("equipment/first.equ")
	text, _ := s.c.archive.Text(i)
	s.c.mu.Unlock()
	if strings.Contains(text, "1000") || !strings.Contains(text, "99") {
		t.Fatal("import did not use frozen bytes")
	}
}
func TestMCPExportDirectAndMod(t *testing.T) {
	for _, mode := range []string{"direct", "mod"} {
		t.Run(mode, func(t *testing.T) {
			s := newMCPFixture(t)
			a := mcpCurrent(s)
			a.Mode = mode
			a.Source = "selection"
			a.Scopes = []string{"equipment"}
			a.Format = "110USextend"
			a.Name = "AI-test"
			a.Version = "1.0"
			prep := mcpCall(t, s, "export_prepare", a)
			requireMCPCompleted(t, prep)
			id := prep.Result.(map[string]any)["exportPlanId"].(string)
			if _, err := s.export.Prepare(ExportRequest{Mode: "direct", Source: "selection", Scopes: []string{"equipment"}}); err == nil {
				t.Fatal("UI replaced AI export")
			}
			page, err := s.exportPreview(mcpArgs{ExportPlanID: id, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			exec := mcpArgs{ExportPlanID: id, OutputParent: t.TempDir(), OutputName: "files"}
			for _, row := range page.(map[string]any)["rows"].([]map[string]any) {
				if !row["required"].(bool) {
					exec.SelectedEntryKeys = append(exec.SelectedEntryKeys, row["entryKey"].(string))
				}
			}
			done := mcpCall(t, s, "export_execute", exec)
			requireMCPCompleted(t, done)
			out := done.Result.(map[string]any)["outputPath"].(string)
			target := filepath.Join(out, "equipment", "first.equ")
			if mode == "mod" {
				target = filepath.Join(out, "pack.json")
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatal(err)
			}
			if _, err = s.exportPreview(mcpArgs{ExportPlanID: id, Limit: 100}); err == nil {
				t.Fatal("export plan not consumed")
			}
		})
	}
}
func TestMCPSaveCopyPreservesSourceAndDirty(t *testing.T) {
	s := newMCPFixture(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pvf")
	s.c.mu.Lock()
	if err := s.c.archive.SaveAs(source); err != nil {
		t.Fatal(err)
	}
	s.c.mcpSourceHash = s.c.archive.SourceHash()
	s.c.mu.Unlock()
	if err := s.editor.SetText(0, "[price]\n42"); err != nil {
		t.Fatal(err)
	}
	a := mcpCurrent(s)
	a.Path = filepath.Join(dir, "copy.pvf")
	copyTask := mcpCall(t, s, "archive_save_as", a)
	requireMCPCompleted(t, copyTask)
	s.c.mu.RLock()
	same := s.c.archive.SourcePath() == source
	dirty := s.c.archive.Modified()
	s.c.mu.RUnlock()
	if !same || !dirty {
		t.Fatal("copy changed source baseline")
	}
	copied, err := pvf.Open(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Release()
	text, _ := copied.Text(0)
	if !strings.Contains(text, "42") {
		t.Fatal("copy missing edit")
	}
	a = mcpCurrent(s)
	done := mcpCall(t, s, "archive_save", a)
	requireMCPCompleted(t, done)
	s.c.mu.RLock()
	dirty = s.c.archive.Modified()
	s.c.mu.RUnlock()
	if dirty {
		t.Fatal("save left archive dirty")
	}
	if err := os.WriteFile(source, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	conflict := mcpCall(t, s, "archive_save", mcpCurrent(s))
	if !strings.Contains(conflict.Error, "SOURCE_CHANGED") {
		t.Fatal("external change not detected")
	}
}
func TestMCPOpenFailureCloseAndDeduplication(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.Path = filepath.Join(t.TempDir(), "bad.pvf")
	a.DirtyPolicy = "discard"
	before := a.WorkspaceID
	failed := mcpCall(t, s, "archive_open", a)
	if failed.Status != "failed" || mcpCurrent(s).WorkspaceID != before {
		t.Fatal("open failure replaced workspace")
	}
	a = mcpCurrent(s)
	a.DirtyPolicy = "discard"
	a.RequestID = "close-once"
	raw, _ := json.Marshal(a)
	value, err := s.call(context.Background(), "archive_close", raw, a)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(map[string]any)["taskId"].(string)
	requireMCPCompleted(t, waitMCPTask(t, s, id))
	retry, err := s.call(context.Background(), "archive_close", raw, a)
	if err != nil || retry.(map[string]any)["taskId"] != id {
		t.Fatal("close retry not deduplicated")
	}
	a.DirtyPolicy = "save"
	raw, _ = json.Marshal(a)
	if _, err = s.call(context.Background(), "archive_close", raw, a); err == nil {
		t.Fatal("request ID conflict accepted")
	}
	if s.workspace()["open"] != false {
		t.Fatal("close failed")
	}
}
func TestMCPUIPreparationProtectsDraftAndBusy(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(fmt.Sprint(draft), func(t *testing.T) {
			s := newMCPFixture(t)
			s.RegisterWindow("main")
			a := mcpCurrent(s)
			a.Source = "pvf.log('query');"
			a.RequestID = mcpID()
			raw, _ := json.Marshal(a)
			value, err := s.call(context.Background(), "script_run", raw, a)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for s.WindowState()["operationId"] == "" && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			s.Acknowledge("main", s.WindowState()["operationId"], draft, false)
			task := waitMCPTask(t, s, value.(map[string]any)["taskId"].(string))
			if draft {
				if !strings.Contains(task.Error, "UI_DRAFT_PENDING") {
					t.Fatal("draft not protected")
				}
			} else {
				requireMCPCompleted(t, task)
			}
		})
	}
}

func TestMCPCancelBusyAndDiscardFileSetMutation(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.Source = "while(true){}"
	a.RequestID = mcpID()
	raw, _ := json.Marshal(a)
	value, err := s.call(context.Background(), "script_run", raw, a)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(map[string]any)["taskId"].(string)
	deadline := time.Now().Add(time.Second)
	for !s.script.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	other := a
	other.RequestID = mcpID()
	raw2, _ := json.Marshal(other)
	if _, err = s.call(context.Background(), "script_run", raw2, other); err == nil {
		t.Fatal("concurrent task accepted")
	}
	if !s.CancelActiveScript() {
		t.Fatal("UI cancel failed")
	}
	done := waitMCPTask(t, s, id)
	requireMCPCompleted(t, done)
	if done.Result.(ScriptRunResult).Status != "cancelled" {
		t.Fatal("script not cancelled")
	}
	a = mcpCurrent(s)
	a.Source = "pvf.createFileset('ai-set', ['equipment/first.equ']);"
	failed := mcpCall(t, s, "script_run", a)
	if !strings.Contains(failed.Error, "UNSUPPORTED_CHANGE") {
		t.Fatal("file set mutation exposed")
	}
	s.c.mu.RLock()
	pending := s.c.scriptPlan != nil
	s.c.mu.RUnlock()
	if pending {
		t.Fatal("unsupported plan retained")
	}
}
func TestMCPImportProtectedSourceAndPlanConflict(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.SourcePaths = []string{filepath.Join(t.TempDir(), ".env")}
	a.Mode = "text"
	failed := mcpCall(t, s, "import_prepare", a)
	if failed.Status != "failed" {
		t.Fatal("protected source accepted")
	}
}
func TestMCPOpenValidArchiveAndDirtyPolicy(t *testing.T) {
	s := newMCPFixture(t)
	path := filepath.Join(t.TempDir(), "next.pvf")
	next := pvf.New()
	if _, err := next.AddFileText("next.equ", "[price]\n123", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := next.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	a := mcpCurrent(s)
	old := a.WorkspaceID
	a.Path = path
	failed := mcpCall(t, s, "archive_open", a)
	if !strings.Contains(failed.Error, "UNSAVED_CHANGES") {
		t.Fatal("dirty switch accepted")
	}
	a.DirtyPolicy = "discard"
	task := mcpCall(t, s, "archive_open", a)
	requireMCPCompleted(t, task)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.c.mu.RLock()
		loading := s.c.versionLoading
		s.c.mu.RUnlock()
		if !loading {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	current := mcpCurrent(s)
	if current.WorkspaceID == old {
		t.Fatal("workspace identity reused")
	}
	current.Path = "next.equ"
	result, err := s.readFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.(map[string]any)["text"].(string), "123") {
		t.Fatal("opened incorrect archive")
	}
}

func TestMCPActualToolsOverHTTP(t *testing.T) {
	s := newMCPFixture(t)
	server := httptest.NewServer(mcpserver.Handler(s.tools()))
	defer server.Close()
	s.server = server.Config
	configuration, err := s.ConnectionConfig()
	if err != nil {
		t.Fatal(err)
	}
	var connection map[string]map[string]map[string]any
	if json.Unmarshal([]byte(configuration), &connection) != nil || len(connection["mcpServers"]["pvfine"]) != 1 || connection["mcpServers"]["pvfine"]["url"] == nil {
		t.Fatal("connection config must contain only URL")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) < 28 {
		t.Fatal("MCP discovery incomplete")
	}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("tool %s failed", name)
		}
		var value map[string]any
		if err = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	workspace := call("pvf_workspace_info", map[string]any{})
	docs := call("pvf_annotation_search", map[string]any{"pvfVersion": "all", "limit": 1})
	if docs["schemaVersion"] != float64(1) {
		t.Fatal("annotation HTTP schema")
	}
	if len(docs["items"].([]any)) > 0 {
		item := docs["items"].([]any)[0].(map[string]any)
		call("pvf_annotation_get", map[string]any{"documentId": item["documentId"], "rulesRevision": docs["rulesRevision"], "valuesLimit": 1})
	}
	call("pvf_annotation_explain", map[string]any{"workspaceId": workspace["workspaceId"], "expectedRevision": workspace["revision"], "filePath": "equipment/first.equ", "limit": 1})
	result := call("pvf_script_run", map[string]any{"workspaceId": workspace["workspaceId"], "expectedRevision": workspace["revision"], "source": "pvf.log('http query');", "requestId": "http-run-once"})
	id := result["taskId"].(string)
	task := waitMCPTask(t, s, id)
	requireMCPCompleted(t, task)
	result = call("pvf_task_status", map[string]any{"taskId": id})
	if result["status"] != "completed" {
		t.Fatal("MCP task result unavailable")
	}
	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pvf_read_file", Arguments: map[string]any{"path": "equipment/first.equ"}})
	if err != nil || !invalid.IsError {
		t.Fatal("missing workspace revision accepted")
	}
}

func TestMCPExportBinaryAndDestinationConflict(t *testing.T) {
	s := newMCPFixture(t)
	s.c.mu.Lock()
	s.c.archive.AddFile("image/data.bin", []byte{1, 2, 3}, 2)
	if err := s.c.rebuildArchiveIndexesLocked(s.c.archive); err != nil {
		t.Fatal(err)
	}
	s.c.mu.Unlock()
	a := mcpCurrent(s)
	a.Mode = "direct"
	a.Source = "selection"
	a.Scopes = []string{"image"}
	task := mcpCall(t, s, "export_prepare", a)
	requireMCPCompleted(t, task)
	id := task.Result.(map[string]any)["exportPlanId"].(string)
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	a = mcpArgs{ExportPlanID: id, OutputParent: parent, OutputName: "files", SelectedEntryKeys: []string{"image/data.bin"}}
	conflict := mcpCall(t, s, "export_execute", a)
	if !strings.Contains(conflict.Error, "TARGET_EXISTS") {
		t.Fatal("existing output replaced")
	}
	a.OutputName = "new-files"
	done := mcpCall(t, s, "export_execute", a)
	requireMCPCompleted(t, done)
	data, err := os.ReadFile(filepath.Join(parent, "new-files", "image", "data.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string([]byte{1, 2, 3}) {
		t.Fatal("binary bytes lost")
	}
}

func TestMCPAtomicDirectoryPublishNeverReplacesExistingEmptyDirectory(t *testing.T) {
	parent := t.TempDir()
	temp, err := os.MkdirTemp(parent, "staged-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(temp, "data.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "existing")
	if err = os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	if err = publishModDirectory(temp, dest); err == nil {
		t.Fatal("existing empty directory was replaced")
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 0 {
		t.Fatal("destination was changed")
	}
}

func TestMCPScriptLogsAreBoundedAndReportTruncation(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.Source = "for(let i=0;i<100;i++){pvf.log('x'.repeat(20000));}"
	task := mcpCall(t, s, "script_run", a)
	requireMCPCompleted(t, task)
	result := task.Result.(ScriptRunResult)
	size := 0
	warned := false
	for _, line := range result.Logs {
		size += len(line.Message)
		if strings.Contains(line.Message, "省略") {
			warned = true
		}
	}
	if size > 300<<10 || !warned {
		t.Fatal("log limit or truncation notice missing")
	}
}
