package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

type exportLogCapture struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (l *exportLogCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.Write(p)
}

func (l *exportLogCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.String()
}

func TestExportMetadataReusesSnapshotAndInvalidatesOnChanges(t *testing.T) {
	c, exports := exportFixture(t)
	r := modRequest("selection")
	r.Scopes, r.IncludeDependencies = []string{"stackable"}, true
	first, err := exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	original := exports.plan
	r.Name, r.Version = "改名后的MOD", "2.3-beta"
	next, err := exports.Prepare(r)
	if err != nil || next.ID == first.ID {
		t.Fatalf("metadata update = %#v, %v", next, err)
	}
	if &exports.plan.pkg.Entries[0].Data[0] != &original.pkg.Entries[0].Data[0] || exports.plan.created != original.created {
		t.Fatal("metadata update decoded content or extended snapshot lifetime")
	}
	exports.Release(first.ID)
	if exports.plan == nil {
		t.Fatal("releasing old preview removed newer metadata task")
	}
	i, _ := c.archive.Find("stackable/demo.stk")
	if err := NewEditorService(c).SetText(i, "[price]\n333"); err != nil {
		t.Fatal(err)
	}
	latest, err := exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	if &exports.plan.pkg.Entries[0].Data[0] == &original.pkg.Entries[0].Data[0] {
		t.Fatal("reused stale content after a mutation")
	}
	root, err := exports.executeTo(latest.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := readExportFile(t, root, "pvf/stackable/demo.stk"); !strings.Contains(got, "333") {
		t.Fatalf("stale script = %q", got)
	}
	if manifest := readExportFile(t, root, "pack.json"); !strings.Contains(manifest, `"version": "2.3-beta"`) {
		t.Fatalf("custom version missing: %s", manifest)
	}
}

func TestExportStringCommentsAndMalformedRows(t *testing.T) {
	capture := &exportLogCapture{}
	oldOutput := log.Writer()
	log.SetOutput(capture)
	t.Cleanup(func() { log.SetOutput(oldOutput) })
	content := exportContent{
		path: "string/itemshop.uv.str", dataType: pvf.TypeUnicode,
		data: []byte("\ufeff\t// 注释>不应成为条目\r\n \t\r\nname>中文>名称\r\nbad row\r\nempty>\r\n>empty key\r\nname>更新名称\r\nspace>  保留空格  \r\n\t // 更多注释"),
	}
	diagnostics := &exportDiagnostics{}
	pairs, err := parseExportPairsReported(content, modpkg.MergeStrings, diagnostics)
	if err != nil || diagnostics.skipped != 2 || len(pairs) != 3 {
		t.Fatalf("pairs = %#v, skipped = %d, err = %v", pairs, diagnostics.skipped, err)
	}
	values := pairMap(pairs)
	if values["name"] != "更新名称" || values["empty"] != "" || values["space"] != "  保留空格  " {
		t.Fatalf("lost string content: %#v", values)
	}
	if output := capture.String(); !strings.Contains(output, "[pvfine:export] skipped: 无法解析文字表 string/itemshop.uv.str 第 4 行") || !strings.Contains(output, "第 6 行") || strings.Contains(output, "第 1 行") {
		t.Fatalf("missing malformed row logs or treated comment as malformed: %s", output)
	}
}

func TestExportDependenciesSkipMalformedContentWithoutAborting(t *testing.T) {
	c, exports := exportFixture(t)
	editor := NewEditorService(c)
	i, _ := c.archive.Find("string/demo.str")
	if err := editor.SetText(i, "\t// 注释\nbad row\nname>可用名称\nempty>"); err != nil {
		t.Fatal(err)
	}
	script, _ := c.archive.Find("stackable/demo.stk")
	if err := editor.SetText(script, "[name]\n{8=`<1::name>`}\n[desc]\n{10=`<1::missing>`}"); err != nil {
		t.Fatal(err)
	}
	r := modRequest("selection")
	r.Scopes, r.IncludeDependencies = []string{"stackable"}, true
	preview, err := exports.Prepare(r)
	if err != nil || preview.DependencyCount != 3 || len(preview.Warnings) != 1 {
		t.Fatalf("best effort preview = %#v, %v", preview, err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := readExportFile(t, root, "merge/string/demo.str"); got != "name>可用名称\n" {
		t.Fatalf("bad rows or unrelated keys exported: %q", got)
	}
	// An unreadable table is skipped once and unresolved references warn.
	c.mu.Lock()
	err = c.archive.SetDataType(i, pvf.TypeScript)
	c.batchRevision++
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	preview, err = exports.Prepare(r)
	if err != nil || preview.FileCount != 2 || len(preview.Warnings) != 1 {
		t.Fatalf("unreadable dependency should not abort: %#v, %v", preview, err)
	}
	// Parseable but format-incompatible registration IDs also skip, rather
	// than aborting the whole task at the final writer validation.
	list, _ := c.archive.Find("list/stackable.lst")
	if err := editor.SetText(list, "-1 `stackable/demo.stk`"); err != nil {
		t.Fatal(err)
	}
	preview, err = exports.Prepare(r)
	if err != nil || preview.FileCount != 1 || len(preview.Warnings) != 1 {
		t.Fatalf("unsupported dependency should not abort: %#v, %v", preview, err)
	}
}

func TestExportModTablesRequireChangeBaseline(t *testing.T) {
	c, exports := exportFixture(t)
	for _, dependencies := range []bool{false, true} {
		r := modRequest("selection")
		r.Scopes, r.IncludeDependencies = []string{"stackable", "list", "string"}, dependencies
		preview, err := exports.Prepare(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range exports.plan.pkg.Entries {
			if entry.Operation != modpkg.ReplaceFile && !dependencies {
				t.Fatalf("full table exported without baseline: %#v", entry)
			}
			if entry.Path == "string/demo.str" && len(entry.Pairs) != 1 {
				t.Fatalf("full dependency table exported: %#v", entry)
			}
		}
		if len(preview.Warnings) != 1 {
			t.Fatalf("skipped tables not reported: %#v", preview)
		}
	}
	// A table-only selection cannot produce an applicable mod.
	r := modRequest("selection")
	r.Scopes = []string{"list", "string"}
	if _, err := exports.Prepare(r); err == nil || !strings.Contains(err.Error(), "无改动基线") {
		t.Fatalf("baseline-less table-only export accepted: %v", err)
	}
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	script, _ := c.archive.Find("stackable/demo.stk")
	if err := NewEditorService(c).SetText(script, "[price]\n200"); err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	_, changes, err := exports.exportSourceLocked(modRequest("working"))
	c.mu.RUnlock()
	if err != nil || len(changes) != 1 || changes[0].before != nil {
		t.Fatalf("ordinary script unnecessarily loaded baseline: %#v, %v", changes, err)
	}
}

func exportFixture(t *testing.T) (*core, *ExportService) {
	t.Helper()
	a := pvf.New()
	for p, text := range map[string]string{
		"stackable/demo.stk": "[name]\n{8=`<1::name>`}\n[price]\n100",
		"list/stackable.lst": "1 `stackable/demo.stk`",
		"list/n_string.lst":  "1 `string/demo.str`",
	} {
		if _, err := a.AddFileText(p, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.AddFileText("string/demo.str", "name>测试物品\nempty>\nkeep>保留", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "test.pvf")
	if err := a.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c, NewExportService(c)
}

func modRequest(source string) ExportRequest {
	return ExportRequest{Source: source, Mode: "mod", Format: "110USextend", Name: "测试MOD"}
}

func readExportFile(t *testing.T, root, p string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestExportMemoryWithoutVersionControl(t *testing.T) {
	c, exports := exportFixture(t)
	if _, err := exports.Prepare(ExportRequest{Source: "memory", Mode: "direct"}); err == nil {
		t.Fatal("unchanged memory accepted")
	}
	i, _ := c.archive.Find("string/demo.str")
	text := "name>内存中文\nempty>\nkeep>保留"
	if err := NewEditorService(c).SetText(i, text); err != nil {
		t.Fatal(err)
	}
	func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for n := 0; n < 501; n++ {
			if _, err := c.archive.AddFileText(fmt.Sprintf("new/%d.stk", n), "[price]\n123", pvf.TypeScript); err != nil {
				t.Fatal(err)
			}
		}
	}()
	for _, mode := range []string{"direct", "mod"} {
		request := modRequest("memory")
		request.Mode = mode
		preview, err := exports.Prepare(request)
		expectedCount := 502
		if mode == "mod" {
			expectedCount = 501
		}
		if err != nil || preview.FileCount != expectedCount {
			t.Fatalf("%s preview = %#v, err = %v", mode, preview, err)
		}
		if c.versionRepo != nil {
			t.Fatal("memory export initialized version control")
		}
		expectedTreeCount := 502
		if mode == "mod" {
			if len(preview.Warnings) != 1 {
				t.Fatalf("missing skipped table warning: %#v", preview.Warnings)
			}
		}
		if len(preview.Files) != expectedTreeCount {
			t.Fatalf("tree truncated: %d", len(preview.Files))
		}
		root, err := exports.executeTo(preview.ID, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if mode == "direct" {
			if got := readExportFile(t, root, "string/demo.str"); strings.TrimSpace(got) != text {
				t.Fatalf("memory text = %q", got)
			}
		} else if _, err := os.Stat(filepath.Join(root, "merge", "string", "demo.str")); !os.IsNotExist(err) {
			t.Fatalf("table without baseline exported: %v", err)
		}
		unchanged := "stackable/demo.stk"
		if mode == "mod" {
			unchanged = "pvf/" + unchanged
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(unchanged))); !os.IsNotExist(err) {
			t.Fatalf("unchanged file exported: %v", err)
		}
	}
}

func TestExportSelectionExclusions(t *testing.T) {
	c, exports := exportFixture(t)
	c.mu.Lock()
	_, err := c.archive.AddFileText("notes.txt", "保留的备注", pvf.TypeUnicode)
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"direct", "mod"} {
		r := modRequest("selection")
		r.Mode, r.Scopes = mode, []string{"stackable", "string", "notes.txt"}
		preview, err := exports.Prepare(r)
		if err != nil {
			t.Fatal(err)
		}
		prefix := ""
		if mode == "mod" {
			prefix = "pvf/"
		}
		excluded := prefix + "stackable/demo.stk"
		for _, invalid := range []string{"../outside", "unknown.stk", "pack.json"} {
			if _, err := exports.executeSelectedTo(preview.ID, t.TempDir(), []string{invalid}); err == nil {
				t.Fatalf("invalid exclusion accepted: %s", invalid)
			}
		}
		all := []string{}
		for _, file := range preview.Files {
			if !file.Required {
				all = append(all, file.Path)
			}
		}
		emptyDir := t.TempDir()
		if _, err := exports.executeSelectedTo(preview.ID, emptyDir, all); err == nil {
			t.Fatal("empty export accepted")
		}
		if entries, err := os.ReadDir(emptyDir); err != nil || len(entries) != 0 {
			t.Fatalf("empty selection wrote files: %v", err)
		}
		root, err := exports.executeSelectedTo(preview.ID, t.TempDir(), []string{excluded, excluded})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(excluded))); !os.IsNotExist(err) {
			t.Fatalf("excluded file exported: %v", err)
		}
		if mode == "mod" {
			manifest := readExportFile(t, root, "pack.json")
			if strings.Contains(manifest, "stackable/demo.stk") {
				t.Fatalf("metadata contains excluded file: %s", manifest)
			}
			readExportFile(t, root, "pvf/notes.txt")
		} else {
			readExportFile(t, root, "string/demo.str")
		}
	}
}

func TestExportExcludedDependenciesAndStaleSelection(t *testing.T) {
	c, exports := exportFixture(t)
	r := modRequest("selection")
	r.Scopes, r.IncludeDependencies = []string{"stackable"}, true
	preview, err := exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount := 0
	for _, file := range preview.Files {
		dependencyCount += file.DependencyCount
	}
	if dependencyCount != preview.DependencyCount {
		t.Fatalf("file dependency counts disagree: %d / %d", dependencyCount, preview.DependencyCount)
	}
	root, err := exports.executeSelectedTo(preview.ID, t.TempDir(), []string{"merge/string/demo.str"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "merge", "string", "demo.str")); !os.IsNotExist(err) {
		t.Fatalf("excluded dependency re-added: %v", err)
	}
	readExportFile(t, root, "merge/list/n_string.lst")
	preview, err = exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	i, _ := c.archive.Find("stackable/demo.stk")
	if err := NewEditorService(c).SetText(i, "[price]\n222"); err != nil {
		t.Fatal(err)
	}
	if _, err := exports.executeSelectedTo(preview.ID, t.TempDir(), []string{"merge/string/demo.str"}); !errors.Is(err, ErrExportStale) {
		t.Fatalf("stale selection accepted: %v", err)
	}
}

func TestExportSelectionDirectAndModDependencies(t *testing.T) {
	c, exports := exportFixture(t)
	direct := ExportRequest{Source: "selection", Mode: "direct", Scopes: []string{"stackable", "stackable/demo.stk"}}
	preview, err := exports.Prepare(direct)
	if err != nil || preview.FileCount != 1 {
		t.Fatalf("preview = %#v, error %v", preview, err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	i, _ := c.archive.Find("stackable/demo.stk")
	want, _ := c.archive.Text(i)
	if text := readExportFile(t, root, "stackable/demo.stk"); text != want {
		t.Fatalf("direct text = %q", text)
	}
	r := modRequest("selection")
	r.Scopes, r.IncludeDependencies = []string{"stackable"}, true
	preview, err = exports.Prepare(r)
	if err != nil || preview.FileCount != 4 || preview.DependencyCount != 3 {
		t.Fatalf("dependency preview = %#v, error %v", preview, err)
	}
	root, err = exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if text := readExportFile(t, root, "merge/string/demo.str"); text != "name>测试物品\n" {
		t.Fatalf("dependency table = %q", text)
	}
	if text := readExportFile(t, root, "merge/list/stackable.lst"); text != "1\t`stackable/demo.stk`\n" {
		t.Fatalf("registration = %q", text)
	}
	// Selected tables without a baseline are skipped; dependency completion
	// still emits only the rows matching the exported script.
	r.Scopes = []string{"stackable/demo.stk", "string/demo.str", "list"}
	preview, err = exports.Prepare(r)
	if err != nil || preview.FileCount != 4 || preview.DependencyCount != 3 || len(preview.Warnings) != 1 {
		t.Fatalf("explicit preview = %#v, error %v", preview, err)
	}
}

func TestExportWorkingAndCommitUseLogicalDifferences(t *testing.T) {
	c, exports := exportFixture(t)
	versions, editor := NewVersionService(c), NewEditorService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	i, _ := c.archive.Find("string/demo.str")
	if err := editor.SetText(i, "name>修改后的物品\nempty>\nnew>新值"); err != nil {
		t.Fatal(err)
	}
	r := modRequest("working")
	preview, err := exports.Prepare(r)
	if err != nil || preview.FileCount != 1 || preview.SkippedDeletes != 1 || len(preview.Warnings) != 1 {
		t.Fatalf("working preview = %#v, error %v", preview, err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if text := readExportFile(t, root, "merge/string/demo.str"); text != "name>修改后的物品\nnew>新值\n" {
		t.Fatalf("delta = %q", text)
	}
	commit, err := versions.Commit("修改文字表")
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.SetText(i, "name>当前工作区不同内容"); err != nil {
		t.Fatal(err)
	}
	r.Source, r.CommitID = "commit", commit.ID
	preview, err = exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	root, err = exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if text := readExportFile(t, root, "merge/string/demo.str"); strings.Contains(text, "当前工作区") || !strings.Contains(text, "修改后的物品") {
		t.Fatalf("history leaked current content: %q", text)
	}
}

func TestExportDeletedFileDirectPreservesBeforeAndModSkips(t *testing.T) {
	c, exports := exportFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	i, _ := c.archive.Find("stackable/demo.stk")
	old, _ := c.archive.CanonicalText(i)
	if _, err := NewArchiveService(c).DeleteFiles([]int32{i}); err != nil {
		t.Fatal(err)
	}
	direct := ExportRequest{Source: "working", Mode: "direct"}
	preview, err := exports.Prepare(direct)
	if err != nil || preview.FileCount != 1 {
		t.Fatalf("deleted direct preview = %#v, error %v", preview, err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if text := readExportFile(t, root, "stackable/demo.stk"); text != old {
		t.Fatalf("deleted before = %q", text)
	}
	if _, err := exports.Prepare(modRequest("working")); err == nil {
		t.Fatal("delete-only mod accepted")
	}
	commit, err := versions.Commit("删除文件")
	if err != nil {
		t.Fatal(err)
	}
	direct.Source, direct.CommitID = "commit", commit.ID
	preview, err = exports.Prepare(direct)
	if err != nil {
		t.Fatal(err)
	}
	root, err = exports.executeTo(preview.ID, t.TempDir())
	if err != nil || readExportFile(t, root, "stackable/demo.stk") != old {
		t.Fatalf("commit deleted export: %v", err)
	}
}

func TestExportWorkingIncludesMoreThan500Changes(t *testing.T) {
	c, exports := exportFixture(t)
	if _, err := NewVersionService(c).Initialize(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	paths := make([]string, 510)
	for i := range paths {
		paths[i] = fmt.Sprintf("etc/new%d.etc", i)
		if _, err := c.archive.AddFileText(paths[i], "[value]\n1", pvf.TypeScript); err != nil {
			c.mu.Unlock()
			t.Fatal(err)
		}
	}
	after, err := pvfversion.ContentSnapshotFromArchive(c.archive, paths)
	if err == nil {
		err = c.recordVersionMutationLocked("bulk add", pvfversion.ContentSnapshot{}, after)
	}
	c.batchRevision++
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := exports.Prepare(ExportRequest{Source: "working", Mode: "direct"})
	if err != nil || preview.FileCount != 510 {
		t.Fatalf("all changes preview = %#v, error %v", preview, err)
	}
}

func TestExportRejectsStaleReleasedAndExpiredPlans(t *testing.T) {
	c, exports := exportFixture(t)
	request := ExportRequest{Source: "selection", Mode: "direct", Scopes: []string{"stackable"}}
	for _, reason := range []string{"mutation", "generation", "session", "release", "expiry", "head"} {
		preview, err := exports.Prepare(request)
		if err != nil {
			t.Fatal(err)
		}
		switch reason {
		case "mutation":
			c.batchRevision++
		case "generation":
			c.archiveGeneration++
		case "session":
			c.versionLoadID++
		case "release":
			exports.Release(preview.ID)
		case "expiry":
			exports.plan.created = time.Now().Add(-11 * time.Minute)
		case "head":
			c.versionHead.ID = "changed"
		}
		dir := t.TempDir()
		if _, err := exports.executeTo(preview.ID, dir); !errors.Is(err, ErrExportStale) {
			t.Fatalf("%s stale error = %v", reason, err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("stale plan wrote files")
		}
	}
}

func TestExportHistoricalDependenciesNeverUseCurrentWorktree(t *testing.T) {
	c, exports := exportFixture(t)
	versions, editor := NewVersionService(c), NewEditorService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	script, _ := c.archive.Find("stackable/demo.stk")
	if err := editor.SetText(script, "[name]\n{8=`<1::name>`}\n[price]\n200"); err != nil {
		t.Fatal(err)
	}
	commit, err := versions.Commit("修改价格")
	if err != nil {
		t.Fatal(err)
	}
	table, _ := c.archive.Find("string/demo.str")
	if err := editor.SetText(table, "name>不应导出的当前文字"); err != nil {
		t.Fatal(err)
	}
	r := modRequest("commit")
	r.CommitID, r.IncludeDependencies = commit.ID, true
	preview, err := exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if text := readExportFile(t, root, "merge/string/demo.str"); text != "name>测试物品\n" {
		t.Fatalf("historical dependency = %q", text)
	}
	// Broken references are logged and skipped, without blocking the export.
	if err := editor.SetText(script, "[name]\n{8=`<99::missing>`}"); err != nil {
		t.Fatal(err)
	}
	r = modRequest("selection")
	r.Scopes, r.IncludeDependencies = []string{"stackable"}, true
	preview, err = exports.Prepare(r)
	if err != nil || len(preview.Warnings) != 1 {
		t.Fatalf("missing reference should warn without aborting: %#v, %v", preview, err)
	}
	r.IncludeDependencies = false
	if _, err := exports.Prepare(r); err != nil {
		t.Fatalf("strict export should not resolve dependencies: %v", err)
	}
}

type failingModWriter struct{ modpkg.Extend110Writer }

func (failingModWriter) Write(root string, pkg modpkg.Package) error {
	if err := os.WriteFile(filepath.Join(root, "partial.txt"), []byte("partial"), 0o644); err != nil {
		return err
	}
	return errors.New("injected write failure")
}

func TestModPublishRejectsExistingAndCleansFailure(t *testing.T) {
	pkg := modpkg.Package{Metadata: modpkg.Metadata{Name: "test"}, Entries: []modpkg.Entry{
		{Path: "a.etc", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: 1, Data: []byte("1")},
	}}
	parent := t.TempDir()
	if _, err := writeModDirectory(parent, pkg, failingModWriter{}); err == nil {
		t.Fatal("injected failure not reported")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 0 {
		t.Fatalf("failed export left entries: %#v", entries)
	}
	root, err := writeModDirectory(parent, pkg, modpkg.Extend110Writer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeModDirectory(parent, pkg, modpkg.Extend110Writer{}); err == nil {
		t.Fatal("existing package overwritten")
	}
	if got := readExportFile(t, root, "pvf/a.etc"); got != "1" {
		t.Fatalf("existing content changed: %q", got)
	}
}

func TestExportListDeltaAndEmptyString(t *testing.T) {
	before := exportContent{path: "list/test.lst", dataType: pvf.TypeScript, data: []byte("1 `a.stk`\n2 `b.stk`")}
	after := exportContent{path: before.path, dataType: pvf.TypeScript, data: []byte("1 `changed.stk`\n3 `new.stk`")}
	entry, removed, err := exportModEntry(after, &before)
	if err != nil || removed != 1 || len(entry.Pairs) != 2 {
		t.Fatalf("list delta = %#v, removed %d, error %v", entry, removed, err)
	}
	before = exportContent{path: "string/test.str", dataType: pvf.TypeUnicode, data: []byte("key>value>with separator")}
	after = exportContent{path: before.path, dataType: pvf.TypeUnicode, data: []byte("key>")}
	entry, removed, err = exportModEntry(after, &before)
	if err != nil || removed != 0 || len(entry.Pairs) != 1 || entry.Pairs[0].Value != "" {
		t.Fatalf("empty string delta = %#v, error %v", entry, err)
	}
	entry, _, err = exportModEntry(exportContent{path: "notes.txt", dataType: pvf.TypeUnicode, data: []byte("\ufeff中文")}, nil)
	if err != nil || string(entry.Data) != "中文" {
		t.Fatalf("text BOM was not removed: %#v, error %v", entry, err)
	}
}

func TestExportRealArchive(t *testing.T) {
	c := testArchive(t) // Opt-in only through PVF_TESTFILE.
	t.Cleanup(c.closeArchive)
	exports := NewExportService(c)
	c.mu.RLock()
	selected := ""
	for i := int32(0); i < c.archive.FileCount(); i++ {
		p := c.archive.Path(i)
		if c.archive.File(i).DataType == pvf.TypeScript && !strings.EqualFold(filepath.Ext(p), ".lst") && modpkg.ValidatePath(p) == nil {
			selected = p
			break
		}
	}
	c.mu.RUnlock()
	if selected == "" {
		t.Skip("archive has no compatible script")
	}
	r := modRequest("selection")
	r.Scopes = []string{selected}
	preview, err := exports.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	root, err := exports.executeTo(preview.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got := readExportFile(t, root, "pvf/"+selected)
	c.mu.RLock()
	i, _ := c.archive.Find(selected)
	want, err := c.archive.Text(i)
	c.mu.RUnlock()
	if err != nil || got != want {
		t.Fatal("mod text differs from archive rendering")
	}
}
