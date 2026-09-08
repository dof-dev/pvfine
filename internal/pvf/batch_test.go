package pvf

import (
	"strings"
	"testing"
)

func applyStructuredForTest(t *testing.T, text string, operations ...StructuredBatchOperation) string {
	t.Helper()
	a := New()
	index, err := a.AddFileText("test/item.equ", text, TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.TransformStructuredBatch(raw, operations)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetRawBytes(index, result.Raw()); err != nil {
		t.Fatal(err)
	}
	got, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStructuredBatchSetsAllDuplicateSections(t *testing.T) {
	got := applyStructuredForTest(t,
		"[price]\n100\n[name]\n`one`\n[price]\n200",
		StructuredBatchOperation{Kind: "set", Section: "price", TokenIndex: 0, Value: "500"},
	)
	if strings.Count(got, "500") != 2 || strings.Contains(got, "100") || strings.Contains(got, "200") {
		t.Fatalf("updated text = %q", got)
	}
}

func TestStructuredBatchSetsNestedSection(t *testing.T) {
	got := applyStructuredForTest(t,
		"[parent]\n0\n[keep]\n2",
		StructuredBatchOperation{
			Kind:       "set",
			Section:    "parent",
			TokenIndex: 0,
			Value:      "[child]\n1\n[/child]",
		},
	)
	for _, fragment := range []string{"[parent]", "[child]", "1", "[/child]", "[keep]"} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("nested set result missing %q: %q", fragment, got)
		}
	}
	if strings.Contains(got, "\n0\n") ||
		strings.Index(got, "[parent]") > strings.Index(got, "[child]") ||
		strings.Index(got, "[/child]") > strings.Index(got, "[keep]") {
		t.Fatalf("nested set result is invalid: %q", got)
	}
}

func TestStructuredBatchSetMissingSectionCanInsert(t *testing.T) {
	got := applyStructuredForTest(t,
		"[name]\n`one`",
		StructuredBatchOperation{
			Kind:            "set",
			Section:         "new section",
			TokenIndex:      0,
			Value:           "[child]\n1\n[/child]",
			CreateIfMissing: true,
			HasEndTag:       true,
		},
	)
	for _, fragment := range []string{"[new section]", "[child]", "1", "[/child]", "[/new section]"} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("missing-section set result missing %q: %q", fragment, got)
		}
	}
	if strings.Index(got, "[name]") > strings.Index(got, "[new section]") {
		t.Fatalf("new section was not appended: %q", got)
	}
}

func TestStructuredBatchSetAllValuesWarnsOnCountMismatch(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/item.equ", "[values]\n1\n2\n[keep]\n3", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.TransformStructuredBatch(raw, []StructuredBatchOperation{{
		Kind: "set", Section: "values", TokenIndex: -1, Value: "10",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed() || len(result.Warnings()) != 1 || !strings.Contains(result.Warnings()[0], "数量") {
		t.Fatalf("all-value set result = %#v", result)
	}
	if err := a.SetRawBytes(index, result.Raw()); err != nil {
		t.Fatal(err)
	}
	got, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "10") || strings.Contains(got, "\n1\n") || strings.Contains(got, "\n2\n") {
		t.Fatalf("all-value set text = %q", got)
	}
	typeResult, err := a.TransformStructuredBatch(raw, []StructuredBatchOperation{{
		Kind: "set", Section: "values", TokenIndex: -1, Value: "10\n`new`",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(typeResult.Warnings()) != 1 || !strings.Contains(typeResult.Warnings()[0], "类型不一致") {
		t.Fatalf("type-mismatch warning = %#v", typeResult.Warnings())
	}
}

func TestStructuredBatchNumericOperators(t *testing.T) {
	got := applyStructuredForTest(t,
		"[price]\n100\n[rate]\n1.25",
		StructuredBatchOperation{Kind: "number", Section: "price", TokenIndex: 0, Operator: "+%", Operand: "10"},
		StructuredBatchOperation{Kind: "number", Section: "rate", TokenIndex: 0, Operator: "round", Operand: "1"},
	)
	if !strings.Contains(got, "110") || !strings.Contains(got, "1.3") {
		t.Fatalf("numeric result = %q", got)
	}
}

func TestStructuredBatchNumericBoundariesAndDivideByZero(t *testing.T) {
	got := applyStructuredForTest(t,
		"[value]\n20\n[min]\n8\n[max]\n3\n[clamp]\n50",
		StructuredBatchOperation{Kind: "number", Section: "value", TokenIndex: 0, Operator: "min", Operand: "10"},
		StructuredBatchOperation{Kind: "number", Section: "min", TokenIndex: 0, Operator: "max", Operand: "12"},
		StructuredBatchOperation{Kind: "number", Section: "clamp", TokenIndex: 0, Operator: "clamp", Operand: "0", OperandEnd: "25"},
	)
	if !strings.Contains(got, "10") || !strings.Contains(got, "12") || !strings.Contains(got, "25") {
		t.Fatalf("boundary result = %q", got)
	}

	a := New()
	index, err := a.AddFileText("test/item.equ", "[value]\n20", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.TransformStructuredBatch(raw, []StructuredBatchOperation{{
		Kind: "number", Section: "value", TokenIndex: 0, Operator: "÷", Operand: "0",
	}}); err == nil {
		t.Fatal("expected divide by zero error")
	}
}

func TestStructuredBatchDeletePairedAndFlatSections(t *testing.T) {
	got := applyStructuredForTest(t,
		"[parent]\n1\n[child]\n2\n[/child]\n3\n[/parent]\n[keep]\n4",
		StructuredBatchOperation{Kind: "delete", Section: "parent"},
	)
	if strings.Contains(got, "parent") || strings.Contains(got, "child") || !strings.Contains(got, "[keep]") {
		t.Fatalf("paired delete result = %q", got)
	}

	got = applyStructuredForTest(t,
		"[remove]\n1\n[name]\n`keep`\n[remove]\n2",
		StructuredBatchOperation{Kind: "delete", Section: "remove"},
	)
	if strings.Contains(got, "remove") || !strings.Contains(got, "keep") {
		t.Fatalf("flat delete result = %q", got)
	}
}

func TestStructuredBatchInsertAfterAnchorAndAtEnd(t *testing.T) {
	got := applyStructuredForTest(t,
		"[name]\n`one`\n[price]\n100",
		StructuredBatchOperation{Kind: "insert", Section: "custom flag", AnchorSection: "name", Value: "1", HasEndTag: true},
	)
	if !strings.Contains(got, "[custom flag]") || !strings.Contains(got, "[/custom flag]") {
		t.Fatalf("insert after anchor result = %q", got)
	}
	if strings.Index(got, "[custom flag]") < strings.Index(got, "[name]") {
		t.Fatalf("inserted section appeared before anchor: %q", got)
	}

	got = applyStructuredForTest(t,
		"[name]\n`one`",
		StructuredBatchOperation{Kind: "insert", Section: "tail", Value: "`done`"},
	)
	if !strings.HasSuffix(strings.TrimSpace(got), "`done`") {
		t.Fatalf("append result = %q", got)
	}
}

func TestStructuredBatchInsertNestedSection(t *testing.T) {
	got := applyStructuredForTest(t,
		"[name]\n`one`",
		StructuredBatchOperation{
			Kind:          "insert",
			Section:       "parent",
			AnchorSection: "name",
			Value:         "[child]\n1\n[/child]\n`tail`",
			HasEndTag:     true,
		},
	)
	for _, fragment := range []string{"[parent]", "[child]", "1", "[/child]", "`tail`", "[/parent]"} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("nested insert result missing %q: %q", fragment, got)
		}
	}
	if strings.Index(got, "[parent]") > strings.Index(got, "[child]") ||
		strings.Index(got, "[/child]") > strings.Index(got, "[/parent]") {
		t.Fatalf("nested section order is invalid: %q", got)
	}
}

func TestStructuredBatchRejectsMalformedAndNonNumeric(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/item.equ", "[price]\n`100`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.TransformStructuredBatch(raw, []StructuredBatchOperation{{
		Kind: "number", Section: "price", TokenIndex: 0, Operator: "+", Operand: "1",
	}}); err == nil {
		t.Fatal("expected non-numeric error")
	}
	if _, err := a.TransformStructuredBatch([]byte{3, 1}, nil); err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestBatchCloneAndCommitDoNotMutatePreviewSource(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/item.equ", "[price]\n100", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	original, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	stage := a.CloneForBatch()
	raw, err := stage.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	result, err := stage.TransformStructuredBatch(raw, []StructuredBatchOperation{{
		Kind: "set", Section: "price", TokenIndex: 0, Value: "500",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.SetRawBytes(index, result.Raw()); err != nil {
		t.Fatal(err)
	}
	previewSource, err := a.Text(index)
	if err != nil || previewSource != original {
		t.Fatalf("preview mutated source: text=%q err=%v", previewSource, err)
	}
	if err := a.CommitBatch(stage, map[int32]struct{}{index: {}}); err != nil {
		t.Fatal(err)
	}
	committed, err := a.Text(index)
	if err != nil || !strings.Contains(committed, "500") {
		t.Fatalf("committed text = %q err=%v", committed, err)
	}
}
