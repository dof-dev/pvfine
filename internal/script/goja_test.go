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
	indexes, err := tx.ChangedIndexes()
	if err != nil || len(indexes) != 2 {
		t.Fatalf("changed indexes = %#v err=%v", indexes, err)
	}
	for _, index := range indexes {
		text, textErr := tx.Stage().Text(index)
		if textErr != nil || !strings.Contains(text, "150") && !strings.Contains(text, "250") {
			t.Fatalf("staged text = %q err=%v", text, textErr)
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
	if indexes, changedErr := tx.ChangedIndexes(); changedErr != nil || len(indexes) != 0 {
		t.Fatalf("warning-only write changed indexes = %#v err=%v", indexes, changedErr)
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
	index, err := built.AddFileText("equipment/item.equ", "[price]\n100\n[price]\n200", pvf.TypeScript)
	if err != nil {
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
	text, err := tx.Stage().Text(index)
	if err != nil || !strings.Contains(text, "3.5") || strings.Contains(text, "200") {
		t.Fatalf("staged duplicate text = %q err=%v", text, err)
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
	text, err := tx.Stage().Text(0)
	if err != nil || text != "更新" {
		t.Fatalf("unicode staged text = %q err=%v", text, err)
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
	if indexes, changedErr := tx.ChangedIndexes(); changedErr != nil || len(indexes) != 1 {
		t.Fatalf("staged rollback candidate = %#v err=%v", indexes, changedErr)
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
