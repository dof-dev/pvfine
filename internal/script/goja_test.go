package script

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func scriptTestArchive(t *testing.T) *pvf.Archive {
	t.Helper()
	built := pvf.New()
	if _, err := built.AddFileText("equipment/a.equ", "[price]\n100\n[name]\n`a`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := built.AddFileText("equipment/b.equ", "[price]\n200\n[name]\n`b`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	archive, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestGojaRuntimeStructuredScript(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
		const files = pvf.glob("equipment/**/*.equ");
		if (files.length !== 2) throw new Error("unexpected file count");
		for (let i = 0; i < files.length; i++) {
			const file = files[i];
			const doc = file.parse();
			const price = doc.section("price");
			if (!price) throw new Error("missing price");
			if (price.getValue(0).type !== "integer") throw new Error("wrong type");
			price.set(price.get(0) + 50);
			file.write(doc);
		}
		if (pvf.modifiedCount !== 2) throw new Error("wrong modified count");
	`, host)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusCompleted || result.Error != nil {
		t.Fatalf("run result = %#v error=%#v", result, result.Error)
	}
	changes, err := tx.Changes()
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes = %#v err=%v", changes, err)
	}
	for _, change := range changes {
		if change.Kind != pvf.ChangeKindChanged {
			t.Fatalf("change kind = %q", change.Kind)
		}
		if !strings.Contains(change.AfterText, "150") && !strings.Contains(change.AfterText, "250") {
			t.Fatalf("staged text = %q", change.AfterText)
		}
	}
	if archive.ModifiedCount() != 0 {
		t.Fatalf("runtime changed live archive: %d", archive.ModifiedCount())
	}
}

func TestGojaRuntimeParseWarningsAreNonFatalAndLogged(t *testing.T) {
	archive := pvf.New()
	if _, err := archive.AddFileText("equipment/malformed.equ", "[parent]\n1\n[/orphan]\n2\n[/parent]", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	var logs []LogEntry
	host := NewBatchAPI(context.Background(), tx, func(entry LogEntry) {
		logs = append(logs, entry)
	}, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
const file = pvf.find("equipment/malformed.equ");
const document = file.parse();
const warnings = document.warnings();
if (warnings.length !== 1 || warnings[0].code !== "orphan-close") throw new Error("warning was not exposed");
if (warnings[0].tokenIndex !== 2 || warnings[0].path[0] !== "parent") throw new Error("warning location was not exposed");
file.write(document);
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("warning result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if len(logs) != 1 || logs[0].Level != LogLevelWarn || !strings.Contains(logs[0].Message, "orphan-close") {
		t.Fatalf("warning logs = %#v", logs)
	}
	if changes, changedErr := tx.Changes(); changedErr != nil || len(changes) != 0 {
		t.Fatalf("warning-only write changed entries = %#v err=%v", changes, changedErr)
	}
}

func TestGojaRuntimeLogsAndProgress(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	var logs []LogEntry
	var progress []Progress
	host := NewBatchAPI(context.Background(), tx, func(entry LogEntry) {
		logs = append(logs, entry)
	}, func(value Progress) {
		progress = append(progress, value)
	})
	result, err := NewGojaRuntime().Run(context.Background(), `
pvf.log("hello", 3);
console.warn("careful");
pvf.progress(1, 2, "scan");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("log result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if len(logs) != 2 || logs[0].Level != LogLevelInfo || logs[0].Message != "hello 3" || logs[1].Level != LogLevelWarn {
		t.Fatalf("logs = %#v", logs)
	}
	if len(progress) != 1 || progress[0].Done != 1 || progress[0].Total != 2 || progress[0].Message != "scan" {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestGojaCompileDiagnostics(t *testing.T) {
	result := NewGojaRuntime().Compile("const =")
	if result.Valid || len(result.Diagnostics) == 0 {
		t.Fatalf("compile result = %#v", result)
	}
	if result.Diagnostics[0].Kind != ErrorKindCompile || result.Diagnostics[0].Line <= 0 {
		t.Fatalf("compile diagnostic = %#v", result.Diagnostics[0])
	}
}

func TestGojaRuntimeDuplicateSelectionAndExactValue(t *testing.T) {
	built := pvf.New()
	if _, err := built.AddFileText("equipment/item.equ", "[price]\n100\n[price]\n200", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	archive, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
		const file = pvf.find("EQUIPMENT/ITEM.EQU");
		const doc = file.parse();
		const prices = doc.sections("price");
		if (prices.length !== 2 || prices[1].occurrence !== 1) throw new Error("bad occurrences");
		prices[1].set({ type: "float", value: 3.5 });
		file.write(doc);
	`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	changes, err := tx.Changes()
	if err != nil || len(changes) != 1 {
		t.Fatalf("staged duplicate changes = %#v err=%v", changes, err)
	}
	if text := changes[0].AfterText; !strings.Contains(text, "3.5") || strings.Contains(text, "200") {
		t.Fatalf("staged duplicate text = %q", text)
	}
}

func TestGojaRuntimeUnicodeTextAndCrossFileDocumentGuard(t *testing.T) {
	built := pvf.New()
	if _, err := built.AddFileText("text/name.str", "初始", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}
	if _, err := built.AddFileText("equipment/item.equ", "[price]\n1", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	archive, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
const unicode = pvf.find("text/name.str");
if (unicode.type !== pvf.types.unicode || unicode.text() !== "初始") throw new Error("unicode read failed");
unicode.setText("更新");
if (unicode.text() !== "更新") throw new Error("unicode write failed");
let parseRejected = false;
try { unicode.parse(); } catch { parseRejected = true; }
if (!parseRejected) throw new Error("unicode parse was accepted");
const first = pvf.find("equipment/item.equ");
const document = first.parse();
try { unicode.write(document); throw new Error("cross-file document was accepted"); } catch (error) {
  if (!String(error).includes("不属于当前文件")) throw error;
}
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("unicode result = %#v error=%#v err=%v", result, result.Error, err)
	}
	changes, changeErr := tx.Changes()
	if changeErr != nil || len(changes) != 1 || changes[0].Path != "text/name.str" || changes[0].AfterText != "更新" {
		t.Fatalf("unicode staged change = %#v err=%v", changes, changeErr)
	}
	if archive.ModifiedCount() != 0 {
		t.Fatal("unicode runtime changed live archive")
	}
}

func TestGojaRuntimeRollbackAndSecurity(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
		if (typeof require !== "undefined" || typeof process !== "undefined" || typeof fetch !== "undefined") {
			throw new Error("host capability leaked");
		}
		pvf.find("equipment/a.equ").setText("changed");
		throw new Error("rollback");
	`, host)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusFailed || result.Error == nil || !strings.Contains(result.Error.Message, "rollback") {
		t.Fatalf("failure result = %#v error=%#v", result, result.Error)
	}
	if archive.ModifiedCount() != 0 {
		t.Fatalf("failed script changed live archive: %d", archive.ModifiedCount())
	}
	// The service owns rollback by dropping the transaction; the staged change
	// is intentionally observable here so the test proves it never crossed the
	// transaction boundary.
	if changes, changedErr := tx.Changes(); changedErr != nil || len(changes) != 1 {
		t.Fatalf("staged rollback candidate = %#v err=%v", changes, changedErr)
	}
}

func TestGojaRuntimeInterruptsOnContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := NewGojaRuntime().Run(ctx, `for (;;) {}`, NewBatchAPI(ctx, NewTransaction(scriptTestArchive(t)), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusCancelled || result.Error == nil || result.Error.Kind != ErrorKindTimeout {
		t.Fatalf("interrupt result = %#v", result)
	}
}

func TestPathPatternMatch(t *testing.T) {
	for _, test := range []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "equipment/**/*.equ", value: "equipment/a.equ", want: true},
		{pattern: "equipment/**/*.equ", value: "equipment/character/a.equ", want: true},
		{pattern: "equipment/*.equ", value: "equipment/character/a.equ", want: false},
		{pattern: "EQUIPMENT/**/A.EQU", value: "equipment/character/a.equ", want: true},
	} {
		if got := PathPatternMatch(test.pattern, test.value); got != test.want {
			t.Errorf("PathPatternMatch(%q, %q) = %v, want %v", test.pattern, test.value, got, test.want)
		}
	}
}

func TestGojaRuntimeCreateCopyDeleteFiles(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const created = pvf.createFile("equipment/new.equ", pvf.types.script, "[price]\n500");
	if (!created || created.path !== "equipment/new.equ") throw new Error("create did not return a handle");
	if (created.type !== pvf.types.script) throw new Error("wrong created type");
	const found = pvf.find("equipment/new.equ");
	if (!found || !found.text().includes("500")) throw new Error("created file not found");

	const copied = pvf.copyFile("equipment/a.equ", "equipment/copy.equ");
	if (!copied || copied.type !== pvf.types.script) throw new Error("copy lost its data type");
	if (!pvf.find("equipment/copy.equ").text().includes("100")) throw new Error("copy content mismatch");

	let rejected = false;
	try { pvf.copyFile("equipment/a.equ", "equipment/b.equ"); } catch { rejected = true; }
	if (!rejected) throw new Error("copy overwrote without the overwrite flag");

	pvf.copyFile("equipment/a.equ", "equipment/b.equ", true);
	if (!pvf.find("equipment/b.equ").text().includes("100")) throw new Error("overwrite did not apply");

	if (pvf.deleteFile("equipment/b.equ") !== true) throw new Error("delete reported no removal");
	if (pvf.find("equipment/b.equ") !== null) throw new Error("deleted file still resolves");
	if (pvf.deleteFile("equipment/b.equ") !== false) throw new Error("second delete should be a no-op");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}

	changes, err := tx.Changes()
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]string, len(changes))
	for _, change := range changes {
		kinds[change.Normalized] = change.Kind
	}
	if kinds["equipment/new.equ"] != pvf.ChangeKindCreated {
		t.Fatalf("new file kind = %q changes=%#v", kinds["equipment/new.equ"], changes)
	}
	if kinds["equipment/copy.equ"] != pvf.ChangeKindCreated {
		t.Fatalf("copy kind = %q changes=%#v", kinds["equipment/copy.equ"], changes)
	}
	// b.equ was overwritten first and then deleted, so the final intent is a
	// removal; the overwrite itself was asserted inside the script.
	if kinds["equipment/b.equ"] != pvf.ChangeKindDeleted {
		t.Fatalf("deleted file kind = %q changes=%#v", kinds["equipment/b.equ"], changes)
	}
	if archive.ModifiedCount() != 0 {
		t.Fatalf("runtime changed live archive: %d", archive.ModifiedCount())
	}
}

func TestGojaRuntimeDeleteThenCreateSamePath(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	pvf.deleteFile("equipment/a.equ");
	const recreated = pvf.createFile("equipment/a.equ", pvf.types.script, "[price]\n999");
	if (recreated.text().includes("100")) throw new Error("recreated file kept old content");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	changes, err := tx.Changes()
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes = %#v err=%v", changes, err)
	}
	if changes[0].Kind != pvf.ChangeKindCreated || !strings.Contains(changes[0].AfterText, "999") {
		t.Fatalf("recreated change = %#v", changes[0])
	}
}

func TestGojaRuntimeCreateThenDeleteLeavesNoChange(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	pvf.createFile("equipment/temporary.equ", pvf.types.script, "[price]\n1");
	if (pvf.deleteFile("equipment/temporary.equ") !== true) throw new Error("delete failed");
	if (pvf.modifiedCount !== 0) throw new Error("create+delete should net to zero, got " + pvf.modifiedCount);
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if changes, changeErr := tx.Changes(); changeErr != nil || len(changes) != 0 {
		t.Fatalf("create+delete staged a change = %#v err=%v", changes, changeErr)
	}
	// The live archive must not gain or lose an entry on commit.
	before := archive.FileCount()
	if _, err := tx.Commit(map[string]struct{}{"equipment/temporary.equ": {}}); err != nil {
		t.Fatal(err)
	}
	if archive.FileCount() != before {
		t.Fatalf("file count = %d want %d", archive.FileCount(), before)
	}
}

func TestGojaRuntimeStaleHandleAfterDelete(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const stale = pvf.find("equipment/a.equ");
	pvf.deleteFile("equipment/b.equ");
	let rejected = false;
	try { stale.setText("should not land"); } catch (error) {
		if (!String(error).includes("句柄已失效")) throw error;
		rejected = true;
	}
	if (!rejected) throw new Error("stale handle was accepted after a delete");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	changes, err := tx.Changes()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Normalized == "equipment/a.equ" {
			t.Fatalf("stale handle mutated an unrelated entry: %#v", change)
		}
	}
}

func TestGojaRuntimeMultipleDeletesUsePathIdentity(t *testing.T) {
	built := pvf.New()
	for _, path := range []string{"equipment/a.equ", "equipment/b.equ", "equipment/c.equ"} {
		if _, err := built.AddFileText(path, "[price]\n1", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	archive, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	// Delete from the middle outward so every removal renumbers later entries;
	// path-addressed staging must still remove exactly the requested paths.
	result, err := NewGojaRuntime().Run(context.Background(), `
	pvf.deleteFile("equipment/a.equ");
	pvf.deleteFile("equipment/c.equ");
	if (pvf.find("equipment/a.equ") !== null || pvf.find("equipment/c.equ") !== null) {
		throw new Error("deleted paths still resolve");
	}
	const survivor = pvf.find("equipment/b.equ");
	if (!survivor) throw new Error("survivor disappeared");
	survivor.setText("[price]\n77");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if _, err := tx.Commit(map[string]struct{}{
		"equipment/a.equ": {},
		"equipment/c.equ": {},
		"equipment/b.equ": {},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := archive.Find("equipment/a.equ"); ok {
		t.Fatal("a.equ survived commit")
	}
	if _, ok := archive.Find("equipment/c.equ"); ok {
		t.Fatal("c.equ survived commit")
	}
	survivor, ok := archive.Find("equipment/b.equ")
	if !ok {
		t.Fatal("b.equ was removed by mistake")
	}
	if text, textErr := archive.Text(survivor); textErr != nil || !strings.Contains(text, "77") {
		t.Fatalf("survivor text = %q err=%v", text, textErr)
	}
}

func TestGojaRuntimeCreateRejectsInvalidPaths(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	for (const path of ["", "  ", "../escape.equ", "dir/../escape.equ", "equipment/a.equ"]) {
		let rejected = false;
		try { pvf.createFile(path, pvf.types.script); } catch { rejected = true; }
		if (!rejected) throw new Error("create accepted " + JSON.stringify(path));
	}
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if changes, changeErr := tx.Changes(); changeErr != nil || len(changes) != 0 {
		t.Fatalf("invalid create staged a change = %#v err=%v", changes, changeErr)
	}
}
