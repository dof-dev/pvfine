package script

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func runFileSetScript(t *testing.T, host *BatchAPI, source string) RunResult {
	t.Helper()
	result, err := NewGojaRuntime().Run(context.Background(), source, host)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusCompleted || result.Error != nil {
		t.Fatalf("run result = %#v error=%#v", result, result.Error)
	}
	return result
}

func TestFileSetStageChangesDedupeAndPreserveMetadata(t *testing.T) {
	stage := NewFileSetStage([]FileSet{
		{ID: "set-1", Name: "默认文件集", Entries: []FileSetEntry{
			{Path: "equipment/a.equ", Name: "a", IDs: []string{"1008"}, Size: 12, DataType: 1},
		}},
	})

	// Rewriting a set keeps surviving entries and drops the removed one.
	if _, err := stage.Replace("默认文件集", []string{"equipment\\a.equ", "/equipment/a.equ/", "equipment/b.equ", "equipment/b.equ"}); err != nil {
		t.Fatal(err)
	}
	fileSet, ok := stage.Lookup("默认文件集")
	if !ok {
		t.Fatal("file set missing after replace")
	}
	wantPaths := []string{"equipment/a.equ", "equipment/b.equ"}
	gotPaths := []string{fileSet.Entries[0].Path, fileSet.Entries[1].Path}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if fileSet.Entries[0].IDs[0] != "1008" || fileSet.Entries[0].Size != 12 {
		t.Fatalf("metadata not preserved: %#v", fileSet.Entries[0])
	}
	if fileSet.Entries[1].Name != "b.equ" {
		t.Fatalf("new entry name = %q", fileSet.Entries[1].Name)
	}

	changes := stage.Changes()
	if len(changes) != 1 || changes[0].Created {
		t.Fatalf("changes = %#v", changes)
	}
	if len(changes[0].Entries) != 2 {
		t.Fatalf("change entries = %#v", changes[0].Entries)
	}
}

func TestFileSetStageOmitsUnchangedAndRejectsDuplicateCreate(t *testing.T) {
	stage := NewFileSetStage([]FileSet{{ID: "set-1", Name: "默认文件集", Entries: []FileSetEntry{{Path: "a.equ"}}}})

	// Replacing with an equivalent list (only spelling differs) stages nothing.
	if _, err := stage.Replace("默认文件集", []string{"/a.equ"}); err != nil {
		t.Fatal(err)
	}
	if changes := stage.Changes(); len(changes) != 0 {
		t.Fatalf("unchanged rewrite staged changes = %#v", changes)
	}

	if _, err := stage.Create("默认文件集", nil); err == nil {
		t.Fatal("creating a duplicate name should fail")
	}
	if _, err := stage.Replace("不存在", nil); err == nil {
		t.Fatal("replacing a missing set should fail")
	}
	if _, err := stage.Create("  ", nil); err == nil {
		t.Fatal("blank name should fail")
	}

	created, err := stage.Create("新建文件集", []string{"b.equ", "b.equ", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Entries) != 1 {
		t.Fatalf("created entries = %#v", created.Entries)
	}
	changes := stage.Changes()
	if len(changes) != 1 || !changes[0].Created || changes[0].Name != "新建文件集" {
		t.Fatalf("create changes = %#v", changes)
	}
}

func TestGojaRuntimeFileSetReadAndWrite(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	host.SetFileSetStage(NewFileSetStage([]FileSet{
		{ID: "set-1", Name: "默认文件集", Entries: []FileSetEntry{
			{Path: "equipment/a.equ", Name: "a", Size: 10, DataType: 1},
			{Path: "equipment/b.equ", Name: "b", Size: 20, DataType: 1},
		}},
	}))

	runFileSetScript(t, host, `
		const set = pvf.fileset("默认文件集");
		if (set === null) throw new Error("set not found");
		if (set.name !== "默认文件集") throw new Error("wrong name");
		const paths = set.getAll();
		if (paths.length !== 2 || paths[0] !== "equipment/a.equ") throw new Error("wrong paths: " + JSON.stringify(paths));

		const kept = set.setAll(["equipment/b.equ", "equipment/c.equ"]);
		if (kept !== 2) throw new Error("setAll returned " + kept);
		if (set.getAll().join("|") !== "equipment/b.equ|equipment/c.equ") throw new Error("setAll not applied");

		const created = pvf.createFileset("新建文件集", ["equipment/a.equ"]);
		if (created.name !== "新建文件集") throw new Error("wrong created name");
		if (pvf.fileset("新建文件集").getAll().length !== 1) throw new Error("created set not readable");

		const empty = pvf.createFileset("空文件集");
		if (empty.getAll().length !== 0) throw new Error("empty set should have no paths");

		if (pvf.fileset("不存在") !== null) throw new Error("unknown set should be null");
	`)

	changes := host.FileSetStage().Changes()
	if len(changes) != 3 {
		t.Fatalf("changes = %#v", changes)
	}
	byName := make(map[string]FileSetChange, len(changes))
	for _, change := range changes {
		byName[change.Name] = change
	}
	edited, ok := byName["默认文件集"]
	if !ok || edited.Created || len(edited.Entries) != 2 {
		t.Fatalf("edited change = %#v", edited)
	}
	// The surviving entry keeps its stored metadata across setAll.
	if edited.Entries[0].Name != "b" || edited.Entries[0].Size != 20 {
		t.Fatalf("surviving metadata lost: %#v", edited.Entries[0])
	}
	if created, ok := byName["新建文件集"]; !ok || !created.Created {
		t.Fatalf("created change = %#v", created)
	}
	// The archive is untouched: a file set edit is not an archive mutation.
	if archive.ModifiedCount() != 0 {
		t.Fatalf("file set API changed the archive: %d", archive.ModifiedCount())
	}
	if staged, err := tx.Changes(); err != nil || len(staged) != 0 {
		t.Fatalf("file set API staged archive changes = %#v err=%v", staged, err)
	}
}

func TestGojaRuntimeFileSetErrorsAreReported(t *testing.T) {
	archive := scriptTestArchive(t)

	cases := []struct {
		name    string
		source  string
		message string
	}{
		{"blank name", `pvf.fileset("  ");`, "文件集名称不能为空"},
		{"duplicate create", `pvf.createFileset("a"); pvf.createFileset("a");`, "文件集已存在"},
		{"setAll needs array", `pvf.createFileset("a").setAll("b.equ");`, "路径列表必须是数组"},
		{"setAll rejects non-string", `pvf.createFileset("a").setAll([1]);`, "路径列表第 1 项必须是字符串"},
		{"blank create name", `pvf.createFileset("");`, "文件集名称不能为空"},
	}
	for _, testCase := range cases {
		// Each case needs its own stage: a create in one case would otherwise
		// make the next case fail on a duplicate name instead of its own error.
		host := NewBatchAPI(context.Background(), NewTransaction(archive), nil, nil)
		host.SetFileSetStage(NewFileSetStage(nil))
		result, err := NewGojaRuntime().Run(context.Background(), testCase.source, host)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", testCase.name, err)
		}
		if result.Status != RunStatusFailed || result.Error == nil {
			t.Fatalf("%s: result = %#v", testCase.name, result)
		}
		if !strings.Contains(result.Error.Message, testCase.message) {
			t.Fatalf("%s: message = %q, want contains %q", testCase.name, result.Error.Message, testCase.message)
		}
	}
}

func TestGojaRuntimeFileSetUnavailableWithoutStage(t *testing.T) {
	archive := scriptTestArchive(t)
	host := NewBatchAPI(context.Background(), NewTransaction(archive), nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `pvf.fileset("默认文件集");`, host)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusFailed || result.Error == nil {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Error.Message, "不支持文件集") {
		t.Fatalf("message = %q", result.Error.Message)
	}
}
