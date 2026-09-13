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

func TestGojaRuntimeCreateFilesInNewDirectories(t *testing.T) {
	archive := scriptTestArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	// Neither brand/ nor copy/ exists in the fixture; both calls must
	// introduce the missing directory chain instead of failing.
	const created = pvf.createFile("brand/new/deep/added.equ", pvf.types.script, "[price]\n500");
	if (created.path !== "brand/new/deep/added.equ") throw new Error("nested create lost its path");
	const found = pvf.find("brand/new/deep/added.equ");
	if (!found || !found.text().includes("500")) throw new Error("nested create not readable");

	const copied = pvf.copyFile("equipment/a.equ", "copy/nested/dir/dup.equ");
	if (copied.path !== "copy/nested/dir/dup.equ") throw new Error("nested copy lost its path");
	if (!pvf.find("copy/nested/dir/dup.equ").text().includes("100")) throw new Error("nested copy content mismatch");
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
	for _, path := range []string{"brand/new/deep/added.equ", "copy/nested/dir/dup.equ"} {
		if kinds[path] != pvf.ChangeKindCreated {
			t.Fatalf("kind for %s = %q changes=%#v", path, kinds[path], changes)
		}
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

// scriptListArchive builds an archive containing an equipment .lst plus the
// files its entries point at.
func scriptListArchive(t *testing.T) *pvf.Archive {
	t.Helper()
	built := pvf.New()
	if _, err := built.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/a.equ` 1009 `character/b.equ`",
		pvf.TypeScript,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"equipment/character/a.equ",
		"equipment/character/b.equ",
		"equipment/character/c.equ",
	} {
		if _, err := built.AddFileText(path, "[name]\n`x`", pvf.TypeScript); err != nil {
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
	return archive
}

func TestGojaRuntimeListReadAndWrite(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");
	const entries = lst.get();
	if (Object.keys(entries).length !== 2) throw new Error("unexpected entry count");
	if (entries["1008"] !== "character/a.equ") throw new Error("wrong entry: " + entries["1008"]);

	// Update an existing id in place.
	lst.set("1008", "character/c.equ");
	// Append a new id.
	lst.set("1010", "character/a.equ");
	// mset handles several ids at once.
	lst.mset({ "1009": "character/c.equ", "1011": "character/b.equ" });

	const after = lst.get();
	if (after["1008"] !== "character/c.equ") throw new Error("update failed");
	if (after["1009"] !== "character/c.equ") throw new Error("mset update failed");
	if (after["1010"] !== "character/a.equ") throw new Error("append failed");
	if (after["1011"] !== "character/b.equ") throw new Error("mset append failed");
	if (Object.keys(after).length !== 4) throw new Error("wrong final count");

	// getId resolves the reverse direction.
	if (lst.getId("character/c.equ") !== "1008") throw new Error("getId mismatch");
	if (lst.getId("equipment/character/b.equ") !== "1011") throw new Error("getId did not accept an archive path");
	if (lst.getId("character/missing.equ") !== null) throw new Error("getId should return null when unregistered");

	// unset reports whether anything was removed.
	if (lst.unset("1011") !== true) throw new Error("unset reported no removal");
	if (lst.unset("1011") !== false) throw new Error("second unset should be a no-op");
	if (lst.get()["1011"] !== undefined) throw new Error("entry survived unset");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}

	changes, err := tx.Changes()
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes = %#v err=%v", changes, err)
	}
	if changes[0].Kind != pvf.ChangeKindChanged || changes[0].Path != "equipment/equipment.lst" {
		t.Fatalf("change = %#v", changes[0])
	}
	if archive.ModifiedCount() != 0 {
		t.Fatalf("runtime changed the live archive: %d", archive.ModifiedCount())
	}
	// Staging keeps the edits isolated from the live list.
	liveIndex, _ := archive.Find("equipment/equipment.lst")
	livePairs, err := archive.ListPairs(liveIndex)
	if err != nil || len(livePairs) != 2 {
		t.Fatalf("live pairs changed: %#v err=%v", livePairs, err)
	}
}

func TestGojaRuntimeListIdCollapsedOnSet(t *testing.T) {
	built := pvf.New()
	if _, err := built.AddFileText(
		"equipment/equipment.lst",
		"1008 `character/a.equ` 1009 `character/b.equ` 1008 `character/c.equ`",
		pvf.TypeScript,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"equipment/character/a.equ", "equipment/character/b.equ", "equipment/character/c.equ"} {
		if _, err := built.AddFileText(path, "[name]\n`x`", pvf.TypeScript); err != nil {
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
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");
	lst.set("1008", "character/b.equ");
	const entries = lst.get();
	if (Object.keys(entries).length !== 2) throw new Error("duplicate id was not collapsed");
	// unset removes every remaining record for the id.
	if (lst.unset("1008") !== true) throw new Error("unset failed");
	if (lst.get()["1008"] !== undefined) throw new Error("entry survived unset");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
}

func TestGojaRuntimeListRejectsMissingTargetAndList(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");

	// A missing target file must be refused instead of registering a dangling entry.
	let rejected = false;
	try { lst.set("2000", "character/does-not-exist.equ"); } catch (error) {
		if (!String(error).includes("不存在")) throw error;
		rejected = true;
	}
	if (!rejected) throw new Error("set accepted a missing target");

	// Lookup does not require the file to exist, so the reverse query stays available.
	if (lst.getId("character/does-not-exist.equ") !== null) throw new Error("getId should be null");

	// The failed write must not have changed anything.
	if (Object.keys(lst.get()).length !== 2) throw new Error("failed set mutated the list");

	// An unknown list file is an error.
	let missingList = false;
	try { pvf.lst("equipment/nope.lst"); } catch { missingList = true; }
	if (!missingList) throw new Error("unknown list file was accepted");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if changes, changeErr := tx.Changes(); changeErr != nil || len(changes) != 0 {
		t.Fatalf("rejected list writes staged a change = %#v err=%v", changes, changeErr)
	}
}

func TestGojaRuntimeListAbsolutePathRebased(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	// The archive path resolves to the same list-relative entry, so it is
	// accepted and stored in the file's native relative form.
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");
	lst.set("2000", "equipment/character/a.equ");
	const stored = lst.getId("character/a.equ");
	if (stored !== "2000" && stored !== "1008") throw new Error("absolute path was not rebased: " + stored);
	if (lst.get()["2000"] !== "character/a.equ") throw new Error("stored value is not list-relative");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
}

func TestGojaRuntimeListRejectsInvalidMSetArgument(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");
	let rejected = false;
	try { lst.mset("not-an-object"); } catch { rejected = true; }
	if (!rejected) throw new Error("mset accepted a non-object");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
	if changes, changeErr := tx.Changes(); changeErr != nil || len(changes) != 0 {
		t.Fatalf("rejected mset staged a change = %#v err=%v", changes, changeErr)
	}
}

func TestGojaRuntimeListRollbackLeavesLiveArchiveUntouched(t *testing.T) {
	archive := scriptListArchive(t)
	index, _ := archive.Find("equipment/equipment.lst")
	before, err := archive.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	pvf.lst("equipment/equipment.lst").set("1008", "character/c.equ");
	throw new Error("abort");
`, host)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunStatusFailed {
		t.Fatalf("run result = %#v", result)
	}
	after, err := archive.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed script mutated the live list")
	}
}

func TestGojaRuntimeListForEachIteratesInFileOrder(t *testing.T) {
	// Ids are deliberately out of numeric order so the test can prove the
	// callback follows the .lst file order rather than sorted keys.
	built := pvf.New()
	if _, err := built.AddFileText(
		"equipment/equipment.lst",
		"3000 `character/a.equ` 1008 `character/b.equ` 2000 `character/c.equ`",
		pvf.TypeScript,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"equipment/character/a.equ", "equipment/character/b.equ", "equipment/character/c.equ"} {
		if _, err := built.AddFileText(path, "[name]\n`x`", pvf.TypeScript); err != nil {
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
	result, err := NewGojaRuntime().Run(context.Background(), `
	const seen = [];
	pvf.lst("equipment/equipment.lst").forEach((id, path) => {
		seen.push(id + "=" + path);
	});
	const expected = ["3000=character/a.equ", "1008=character/b.equ", "2000=character/c.equ"];
	if (JSON.stringify(seen) !== JSON.stringify(expected)) {
		throw new Error("forEach order = " + JSON.stringify(seen));
	}
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
}

func TestGojaRuntimeListForEachValidationAndMutation(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");

	// A missing or non-callable argument is an error.
	let rejected = false;
	try { lst.forEach("not-a-function"); } catch { rejected = true; }
	if (!rejected) throw new Error("forEach accepted a non-function");

	// The callback may edit the list it is iterating; the snapshot stays stable.
	const touched = [];
	lst.forEach((id) => { touched.push(id); lst.unset(id); });
	if (touched.length !== 2) throw new Error("forEach did not visit every entry");
	if (Object.keys(lst.get()).length !== 0) throw new Error("entries survived unset");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
	}
}

func TestGojaRuntimeListAcceptsNumericIDs(t *testing.T) {
	archive := scriptListArchive(t)
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	// .lst ids are integers in the token stream, so a JS number must be
	// accepted and normalised to the same string key.
	result, err := NewGojaRuntime().Run(context.Background(), `
	const lst = pvf.lst("equipment/equipment.lst");
	lst.set(2000, "character/c.equ");
	if (lst.get()["2000"] !== "character/c.equ") throw new Error("numeric id was not normalised");
	if (lst.getId("character/c.equ") !== "2000") throw new Error("getId mismatch");
	if (lst.unset(2000) !== true) throw new Error("unset rejected a numeric id");
	if (lst.get()["2000"] !== undefined) throw new Error("numeric-id entry survived unset");
	// Existing integer ids can also be addressed as numbers.
	lst.set(1008, "character/c.equ");
	if (lst.get()["1008"] !== "character/c.equ") throw new Error("existing numeric id was not updated");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("run result = %#v error=%#v err=%v", result, result.Error, err)
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
