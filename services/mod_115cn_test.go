package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
)

func importCN115Fixture(t *testing.T) (*core, *ModImportService, string) {
	t.Helper()
	c, _ := exportFixture(t)
	root := t.TempDir()
	pkg := modpkg.Package{
		Metadata: modpkg.Metadata{ID: "service-fixture", Name: "组件测试", Version: "1.0.0"},
		Entries: []modpkg.Entry{
			{Path: "stackable/demo.stk", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: pvf.TypeScript, Data: []byte("[price]\n200")},
			{Path: "string/demo.str", Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "name", Value: "新名称"}}},
			{Path: "stackable/new.stk", Operation: modpkg.AddFile, Encoding: modpkg.Text, DataType: pvf.TypeScript, Data: []byte("[price]\n300")},
			{Path: "list/stackable.lst", Operation: modpkg.MergeList, Pairs: []modpkg.Pair{{Key: "2", Value: "stackable/new.stk"}}},
		},
	}
	if err := (modpkg.CN115Writer{}).Write(root, pkg); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "mod.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["components"] = []modpkg.Component{
		{ID: "base", Title: "基础", DefaultSelected: true},
		{ID: "optional", Title: "扩展", Requires: []string{"base"}},
	}
	ops := m["pvf"].([]any)
	for _, op := range ops[2:] {
		op.(map[string]any)["component"] = "optional"
	}
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return c, NewModImportService(c), root
}

func TestCN115ImportFrozenComponents(t *testing.T) {
	c, service, root := importCN115Fixture(t)
	before := modText(t, c, "stackable/demo.stk")
	preview, err := service.Prepare(root, modpkg.CN115Format)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Files) != 2 || len(preview.Components) != 2 || len(preview.SelectedComponents) != 1 {
		t.Fatalf("default preview: %+v", preview)
	}
	created := service.plan.created
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SelectComponents(preview.ID, []string{"optional"}); err == nil {
		t.Fatal("dependency ignored")
	}
	selected, err := service.SelectComponents(preview.ID, []string{"base", "optional"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID == preview.ID || selected.ImportedCount != 1 || selected.OverwrittenCount != 3 || service.plan.created != created {
		t.Fatalf("selected preview: %+v", selected)
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("preview changed archive")
	}
	if _, err := service.Execute(preview.ID); err == nil {
		t.Fatal("old component preview accepted")
	}
	result, err := service.Execute(selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 1 || !strings.Contains(modText(t, c, "stackable/demo.stk"), "200") || !strings.Contains(modText(t, c, "list/stackable.lst"), "stackable/new.stk") || !strings.Contains(modText(t, c, "string/demo.str"), "keep>保留") {
		t.Fatalf("import failed: %+v", result)
	}
}

func TestCN115ImportEmptyAndStaleSelections(t *testing.T) {
	c, service, root := importCN115Fixture(t)
	preview, err := service.Prepare(root, modpkg.CN115Format)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := service.SelectComponents(preview.ID, []string{})
	if err != nil || len(empty.Files) != 0 {
		t.Fatal("empty selection failed", err)
	}
	before := modText(t, c, "stackable/demo.stk")
	if _, err := service.Execute(empty.ID); err == nil {
		t.Fatal("empty selection installed")
	}
	if modText(t, c, "stackable/demo.stk") != before {
		t.Fatal("empty selection changed archive")
	}
	preview, err = service.Prepare(root, modpkg.CN115Format)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Release(preview.ID)
	c.batchRevision++
	if _, err := service.SelectComponents(preview.ID, []string{"base", "optional"}); err == nil {
		t.Fatal("stale component preview rebuilt")
	}
}

func TestCN115ImportAddCollisionIsAtomic(t *testing.T) {
	c, service, root := importCN115Fixture(t)
	before := modText(t, c, "stackable/demo.stk")
	if _, err := c.archive.AddFileText("stackable/new.stk", "[price]\n999", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	preview, err := service.Prepare(root, modpkg.CN115Format)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Release(preview.ID)
	if _, err := service.SelectComponents(preview.ID, []string{"base", "optional"}); err == nil || !strings.Contains(err.Error(), "add") {
		t.Fatal("add overwrote an existing target", err)
	}
	if modText(t, c, "stackable/demo.stk") != before || !strings.Contains(modText(t, c, "stackable/new.stk"), "999") {
		t.Fatal("failed preflight changed archive")
	}
}

func TestCN115ExportZIPAndSelection(t *testing.T) {
	_, exports := exportFixture(t)
	request := modRequest("selection")
	request.Format = modpkg.CN115Format
	request.ModID = "stable-unique-id"
	request.Version = "1.2.3"
	request.Scopes = []string{"stackable", "string"}
	request.IncludeDependencies = true
	preview, err := exports.Prepare(request)
	if err != nil {
		t.Fatal(err)
	}
	request.ModID = "changed-unique-id"
	preview, err = exports.Prepare(request)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	output, err := exports.executeSelectedTo(preview.ID, parent, []string{"pvf/string/demo.str"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(output) != ".zip" {
		t.Fatal("export is not a ZIP", output)
	}
	root, closer, err := modpkg.OpenSource(output, true)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (modpkg.CN115Reader{}).Read(root)
	closer.Close()
	if err != nil || pkg.Metadata.ID != request.ModID || pkg.Metadata.Version != request.Version {
		t.Fatal("ZIP metadata", pkg.Metadata, err)
	}
	for _, e := range pkg.Entries {
		if e.Path == "string/demo.str" {
			t.Fatal("excluded payload reappeared")
		}
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = exports.Prepare(request)
	if err != nil {
		t.Fatal(err)
	}
	defer exports.Release(preview.ID)
	if _, err := exports.executeTo(preview.ID, parent); err == nil {
		t.Fatal("existing MOD ZIP overwritten")
	}
	after, err := os.ReadFile(output)
	if err != nil || string(after) != string(data) {
		t.Fatal("existing ZIP changed", err)
	}
}

func TestCN115RealMODPreflight(t *testing.T) {
	modFile, pvfFile := os.Getenv("PVF_MOD_TESTFILE"), os.Getenv("PVF_MOD_ARCHIVE")
	if modFile == "" || pvfFile == "" {
		t.Skip("PVF_MOD_TESTFILE and PVF_MOD_ARCHIVE required")
	}
	a, err := pvf.Open(pvfFile)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	c := newCore()
	c.archive = a
	service := NewModImportService(c)
	beforeCount, beforeModified := a.FileCount(), a.ModifiedCount()
	preview, err := service.Prepare(modFile, modpkg.CN115Format)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Release(preview.ID)
	if len(preview.Files) == 0 {
		t.Fatal("no PVF content")
	}
	if a.FileCount() != beforeCount || a.ModifiedCount() != beforeModified {
		t.Fatal("preflight changed the source archive")
	}
	t.Logf("MOD %s %s: files=%d added=%d replaced=%d", preview.Name, preview.Version, len(preview.Files), preview.ImportedCount, preview.OverwrittenCount)
}

func TestCN115MCPExportZIP(t *testing.T) {
	s := newMCPFixture(t)
	a := mcpCurrent(s)
	a.Mode, a.Source, a.Format = "mod", "selection", modpkg.CN115Format
	a.Scopes, a.Name, a.ModID = []string{"equipment"}, "MCP-115CN", "mcp-unique-mod"
	prepared := mcpCall(t, s, "export_prepare", a)
	requireMCPCompleted(t, prepared)
	id := prepared.Result.(map[string]any)["exportPlanId"].(string)
	page, err := s.exportPreview(mcpArgs{ExportPlanID: id, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	exec := mcpArgs{ExportPlanID: id, OutputParent: t.TempDir()}
	for _, row := range page.(map[string]any)["rows"].([]map[string]any) {
		if !row["required"].(bool) {
			exec.SelectedEntryKeys = append(exec.SelectedEntryKeys, row["entryKey"].(string))
		}
	}
	done := mcpCall(t, s, "export_execute", exec)
	requireMCPCompleted(t, done)
	output := done.Result.(map[string]any)["outputPath"].(string)
	if filepath.Base(output) != "MCP-115CN.zip" {
		t.Fatal("unexpected output", output)
	}
	root, closer, err := modpkg.OpenSource(output, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	pkg, err := (modpkg.CN115Reader{}).Read(root)
	if err != nil || pkg.Metadata.ID != a.ModID {
		t.Fatal("MCP dropped stable MOD ID", pkg.Metadata, err)
	}
	for _, tool := range s.tools() {
		if tool.Name == "pvf_export_prepare" {
			properties := tool.Schema["properties"].(map[string]any)
			if properties["modId"] == nil {
				t.Fatal("modId missing from MCP schema")
			}
		}
	}
}

func TestModImportUnicodeTableTerminators(t *testing.T) {
	entry := modpkg.Entry{Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "name", Value: "new"}, {Key: "added", Value: "value"}}}
	base := "\ufeff// preserved\r\nname>old\r\nkeep>same\r\n\x00\x00"
	text, rows, err := mergeModTable(base, entry)
	if err != nil || text != "\ufeff// preserved\r\nname>new\r\nkeep>same\r\nadded>value\n\x00\x00" || len(rows) != 2 || rows[0].Previous != "old" {
		t.Fatalf("terminators changed: %q %+v %v", text, rows, err)
	}
	for _, invalid := range []string{"name>old\nbroken\n\x00\x00", "name>old\n\x00\x00\n"} {
		if _, _, err := mergeModTable(invalid, entry); err == nil {
			t.Fatal("unrecognized malformed row accepted")
		}
	}
}
