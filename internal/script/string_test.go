package script

import (
	"context"
	"os"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestRegisterStringReferenceIsolationAndCommit(t *testing.T) {
	archive := pvf.New()
	for _, file := range []struct {
		path string
		text string
		kind int32
	}{
		{"list/n_string.lst", "31 `string/names.str`", pvf.TypeScript},
		{"string/names.str", "shared>Original\n", pvf.TypeUnicode},
		{"equipment/item.equ", "[name]\n{8=`<31::shared>`}", pvf.TypeScript},
	} {
		if _, err := archive.AddFileText(file.path, file.text, file.kind); err != nil {
			t.Fatal(err)
		}
	}
	tx := NewTransaction(archive)
	index, _ := tx.Stage().Find("equipment/item.equ")
	raw, _ := tx.Stage().RawBytes(index)
	doc, err := tx.Stage().ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	existing, _ := doc.GetValue([]string{"name"}, 0, 0)
	value, err := tx.RegisterStringReference("equipment/item.equ", "Replacement", &existing)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := tx.RegisterStringReference("equipment/item.equ", "Replacement", &existing)
	if err != nil || repeated.Value != value.Value {
		t.Fatalf("reused value = %#v err=%v", repeated, err)
	}
	if value.Type != pvf.ScriptTokenBlock8 || tx.Stage().ResolvePlaceholder(value.Value.(string)) != "Replacement" {
		t.Fatalf("registered value = %#v", value)
	}
	if tx.Stage().ResolvePlaceholder("<31::shared>") != "Original" {
		t.Fatal("shared reference changed")
	}
	empty, err := tx.RegisterStringReference("equipment/item.equ", "", &existing)
	if err != nil {
		t.Fatal(err)
	}
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	if text, err := host.GetOriStrValue(empty.Value.(string)); err != nil || text != "" {
		t.Fatalf("empty reference = %q err=%v", text, err)
	}
	if _, err := tx.RegisterStringReference("equipment/item.equ", "invalid\nline", &existing); err == nil {
		t.Fatal("multiline text was accepted into a line-based table")
	}
	if _, err := doc.Set([]string{"name"}, 0, 0, value, false, false); err != nil {
		t.Fatal(err)
	}
	encoded, err := tx.Stage().EncodeScriptDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetRawBytes("equipment/item.equ", encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(map[string]struct{}{"equipment/item.equ": {}}); err == nil {
		t.Fatal("commit accepted missing string-table dependency")
	}
	if archive.ResolvePlaceholder(value.Value.(string)) != value.Value {
		t.Fatal("preview mutated live table")
	}
	if _, err := tx.Commit(map[string]struct{}{"equipment/item.equ": {}, "string/names.str": {}}); err != nil {
		t.Fatal(err)
	}
	if archive.ResolvePlaceholder(value.Value.(string)) != "Replacement" {
		t.Fatal("committed reference cannot be resolved")
	}
}

func TestGojaRuntimeRemoveChildrenAndLegacySet(t *testing.T) {
	archive := pvf.New()
	if _, err := archive.AddFileText("test/item.equ",
		"[parent]\n1\n[child]\n2\n[grandchild]\n3\n[/grandchild]\n[/child]\n[keep]\n4\n[/keep]\n[child]\n5\n[/child]\n[/parent]\n[name]\n{8=`<31::old>`}", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
const file = pvf.find("test/item.equ");
const doc = file.parse();
const parent = doc.section("parent");
const removed = parent.children()[0];
const grandchild = removed.children()[0];
if (parent.removeChildren("[child]") !== 2) throw new Error("remove count");
if (parent.removeChildren("child") !== 0) throw new Error("repeat remove");
if (parent.children().length !== 1 || parent.children()[0].name !== "keep" || parent.get() !== 1) throw new Error("unrelated content changed");
let rejected = false;
try { grandchild.set(9); } catch { rejected = true; }
if (!rejected) throw new Error("removed subtree handle remains usable");
rejected = false;
try { parent.removeChildren(""); } catch { rejected = true; }
if (!rejected) throw new Error("empty name accepted");
doc.set("name", "Plain");
if (doc.getValue("name").type !== "quoted") throw new Error("legacy set retained reference type");
parent.setStrValue("Literal");
file.write(doc);
const parsed = file.parse();
if (parsed.sections(["parent", "child"]).length !== 0) throw new Error("children survived write");
if (parsed.get("parent") !== "Literal") throw new Error("legacy text lost");
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("result=%#v diagnostic=%#v err=%v", result, result.Error, err)
	}
}

func TestGojaRuntimePaged110SetStrings(t *testing.T) {
	filePath := os.Getenv("PVF_TESTFILE")
	if filePath == "" {
		t.Skip("PVF_TESTFILE not set")
	}
	archive, err := pvf.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !archive.ContentRules().SupportsStringReferences {
		t.Skip("archive does not use string references")
	}
	tx := NewTransaction(archive)
	host := NewBatchAPI(context.Background(), tx, nil, nil)
	result, err := NewGojaRuntime().Run(context.Background(), `
const file = pvf.createFile("pvfine-script-test/set.equ", pvf.types.script, "[name]\n`+"`old`"+`");
const doc = file.parse();
doc.set("name", "Automatically registered");
const name = doc.section("name");
if (name.getValue().type !== "block8" || name.getOriStrValue() !== "Automatically registered") throw new Error("document registration");
name.set("Section text");
if (name.getValue().type !== "block8" || name.getOriStrValue() !== "Section text") throw new Error("section registration");
if (!doc.set("description", "New section", {create:true})) throw new Error("create section");
name.set("");
if (name.getValue().type !== "block8" || name.getOriStrValue() !== "") throw new Error("empty string");
doc.setStrValue("name", "Forced legacy");
if (name.getValue().type !== "quoted") throw new Error("forced document legacy");
name.setStrValue("Section legacy");
if (name.getValue().type !== "quoted") throw new Error("forced section legacy");
file.write(doc);
`, host)
	if err != nil || result.Status != RunStatusCompleted {
		t.Fatalf("result=%#v diagnostic=%#v err=%v", result, result.Error, err)
	}
	changes, err := tx.Changes()
	if err != nil || len(changes) < 2 {
		t.Fatalf("changes=%d err=%v", len(changes), err)
	}
	foundTable := false
	for _, change := range changes {
		foundTable = foundTable || strings.HasSuffix(strings.ToLower(change.Path), ".str")
	}
	if !foundTable {
		t.Fatal("automatic registration was not included in preview")
	}
	tx.Rollback()
}
