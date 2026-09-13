package pvf

import (
	"bytes"
	"strings"
	"testing"
)

func TestApplyScriptChangesRoundTrip(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("equipment/keep.equ", "[price]\n100", TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("equipment/drop.equ", "[price]\n200", TypeScript); err != nil {
		t.Fatal(err)
	}
	var seed bytes.Buffer
	if err := a.SaveTo(&seed); err != nil {
		t.Fatal(err)
	}
	archive, err := Parse(seed.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	stage := archive.CloneForBatch()
	editedIndex, ok := stage.Find("equipment/keep.equ")
	if !ok {
		t.Fatal("keep.equ missing from stage")
	}
	if err := stage.SetText(editedIndex, "[price]\n150"); err != nil {
		t.Fatal(err)
	}
	editedRaw, err := stage.RawBytes(editedIndex)
	if err != nil {
		t.Fatal(err)
	}
	// Encode the new file's payload through the stage so it resolves string
	// pool offsets the same way a script-created file would.
	scratch, err := stage.AddFileText("scratch/encode.equ", "[price]\n300", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	addedRaw, err := stage.RawBytes(scratch)
	if err != nil {
		t.Fatal(err)
	}

	changes := []ScriptChange{
		{Kind: ChangeKindChanged, Path: "equipment/keep.equ", Raw: editedRaw, DataType: TypeScript},
		{Kind: ChangeKindCreated, Path: "equipment/new.equ", Raw: addedRaw, DataType: TypeScript},
		{Kind: ChangeKindDeleted, Path: "equipment/drop.equ"},
	}
	if err := archive.ApplyScriptChanges(stage, changes); err != nil {
		t.Fatal(err)
	}

	if _, ok := archive.Find("equipment/drop.equ"); ok {
		t.Fatal("deleted entry still resolves")
	}
	newIndex, ok := archive.Find("equipment/new.equ")
	if !ok {
		t.Fatal("created entry does not resolve")
	}
	if text, err := archive.Text(newIndex); err != nil || !strings.Contains(text, "300") {
		t.Fatalf("created text = %q err=%v", text, err)
	}
	kept, ok := archive.Find("equipment/keep.equ")
	if !ok {
		t.Fatal("kept entry does not resolve")
	}
	if text, err := archive.Text(kept); err != nil || !strings.Contains(text, "150") {
		t.Fatalf("edited text = %q err=%v", text, err)
	}

	// The rebuilt archive must survive a save/parse round trip with the same
	// entry table and payloads.
	var out bytes.Buffer
	if err := archive.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.FileCount() != archive.FileCount() {
		t.Fatalf("file count = %d want %d", reparsed.FileCount(), archive.FileCount())
	}
	if _, ok := reparsed.Find("equipment/drop.equ"); ok {
		t.Fatal("deleted entry reappeared after round trip")
	}
	reparsedNew, ok := reparsed.Find("equipment/new.equ")
	if !ok {
		t.Fatal("created entry vanished after round trip")
	}
	if text, err := reparsed.Text(reparsedNew); err != nil || !strings.Contains(text, "300") {
		t.Fatalf("round-trip created text = %q err=%v", text, err)
	}
}

func TestNormalizeNewFilePathAllowsMissingDirectories(t *testing.T) {
	valid := map[string]string{
		"equipment/new.equ":     "equipment/new.equ",
		"a/b/c/deep.equ":        "a/b/c/deep.equ",
		"/dir/leading.equ/":     "dir/leading.equ",
		`dir\windows\style.equ`: "dir/windows/style.equ",
		"  dir/spaced.equ  ":    "dir/spaced.equ",
	}
	for raw, want := range valid {
		got, err := NormalizeNewFilePath(raw)
		if err != nil {
			t.Fatalf("NormalizeNewFilePath(%q) error = %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeNewFilePath(%q) = %q want %q", raw, got, want)
		}
	}

	invalid := []string{
		"", "   ", "../escape.equ", "dir/../escape.equ",
		"dir/./file.equ", "./file.equ", "brand/new/deep//nested.equ",
	}
	for _, raw := range invalid {
		if got, err := NormalizeNewFilePath(raw); err == nil {
			t.Fatalf("NormalizeNewFilePath(%q) = %q, want an error", raw, got)
		}
	}
}

func TestApplyScriptChangesCreatesMissingDirectories(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("equipment/keep.equ", "[price]\n100", TypeScript); err != nil {
		t.Fatal(err)
	}
	stage := a.CloneForBatch()
	scratch, err := stage.AddFileText("scratch/encode.equ", "[price]\n300", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	addedRaw, err := stage.RawBytes(scratch)
	if err != nil {
		t.Fatal(err)
	}

	// Neither brand/ nor its descendants exist yet; creating the file is
	// expected to introduce the whole directory chain.
	if err := a.ApplyScriptChanges(stage, []ScriptChange{
		{Kind: ChangeKindCreated, Path: "brand/new/deep/nested.equ", Raw: addedRaw, DataType: TypeScript},
	}); err != nil {
		t.Fatal(err)
	}
	index, ok := a.Find("brand/new/deep/nested.equ")
	if !ok {
		t.Fatal("created entry in a new directory does not resolve")
	}
	if text, err := a.Text(index); err != nil || !strings.Contains(text, "300") {
		t.Fatalf("created text = %q err=%v", text, err)
	}

	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reparsed.Find("brand/new/deep/nested.equ"); !ok {
		t.Fatal("entry in a new directory missing after a save/parse round trip")
	}
}

func TestApplyScriptChangesRejectsInvalidBatchAtomically(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("equipment/keep.equ", "[price]\n100", TypeScript); err != nil {
		t.Fatal(err)
	}
	stage := a.CloneForBatch()
	before := a.FileCount()

	err := a.ApplyScriptChanges(stage, []ScriptChange{
		{Kind: ChangeKindDeleted, Path: "equipment/keep.equ"},
		{Kind: ChangeKindCreated, Path: "../escape.equ", Raw: []byte{}, DataType: TypeScript},
	})
	if err == nil {
		t.Fatal("expected the invalid path to be rejected")
	}
	if a.FileCount() != before {
		t.Fatalf("file count = %d want %d", a.FileCount(), before)
	}
	if _, ok := a.Find("equipment/keep.equ"); !ok {
		t.Fatal("rejected batch still deleted an entry")
	}
}
