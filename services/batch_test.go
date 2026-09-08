package services

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func batchTestArchive(t *testing.T, files map[string]string) *pvf.Archive {
	t.Helper()
	built := pvf.New()
	for path, text := range files {
		if _, err := built.AddFileText(path, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestBatchTextPreviewAndApply(t *testing.T) {
	c := NewCore()
	a := batchTestArchive(t, map[string]string{
		"skill/a.stk": "[cool time]\n10000\n[name]\n`a`",
		"skill/b.stk": "[cool time]\n10000\n[name]\n`b`",
	})
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	svc := NewBatchService(c)
	page, err := svc.Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{"skill/a.stk", "skill/b.stk"},
		Text:  &TextReplaceSpec{Find: "10000", Replacement: "5000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.MatchedFiles != 2 || page.MatchedOccurrences != 2 || page.ChangedFiles != 2 {
		t.Fatalf("summary = %#v", page)
	}
	if a.ModifiedCount() != 0 {
		t.Fatalf("preview changed archive: %d", a.ModifiedCount())
	}
	if len(page.Rows) != 2 || len(page.Rows[0].Diff) == 0 {
		t.Fatalf("preview rows = %#v", page.Rows)
	}

	indexes := []int32{page.Rows[0].FileIndex}
	result, err := svc.Apply(page.PlanID, indexes)
	if err != nil {
		t.Fatal(err)
	}
	if result.AppliedFiles != 1 || a.ModifiedCount() != 1 {
		t.Fatalf("apply result = %#v modified=%d", result, a.ModifiedCount())
	}
	changed, err := a.Text(indexes[0])
	if err != nil || !strings.Contains(changed, "5000") {
		t.Fatalf("changed text = %q err=%v", changed, err)
	}
	other, ok := a.Find("skill/b.stk")
	if !ok {
		t.Fatal("other file missing")
	}
	unchanged, err := a.Text(other)
	if err != nil || !strings.Contains(unchanged, "10000") {
		t.Fatalf("unselected text = %q err=%v", unchanged, err)
	}
}

func TestBatchRegexPreviewAndStalePlan(t *testing.T) {
	c := NewCore()
	a := batchTestArchive(t, map[string]string{
		"skill/a.stk": "[name]\n`skill-12`",
	})
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	svc := NewBatchService(c)
	page, err := svc.Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{"skill/a.stk"},
		Text:  &TextReplaceSpec{Find: `skill-(\d+)`, Replacement: `skill-$1-updated`, Regex: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	index := page.Rows[0].FileIndex
	if err := NewEditorService(c).SetText(index, "[name]\n`manual`"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(page.PlanID, []int32{index}); !errors.Is(err, ErrBatchPlanStale) {
		t.Fatalf("stale apply error = %v", err)
	}
}

func TestBatchUnicodeTextReplacement(t *testing.T) {
	built := pvf.New()
	index, err := built.AddFileText("text/name.str", "原始名称", pvf.TypeUnicode)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := built.SaveTo(&data); err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Parse(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	page, err := NewBatchService(c).Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{"text/name.str"},
		Text:  &TextReplaceSpec{Find: "原始", Replacement: "新"},
	})
	if err != nil || len(page.Rows) != 1 || page.Rows[0].Status != BatchFileChanged {
		t.Fatalf("unicode preview = %#v err=%v", page, err)
	}
	if _, err := NewBatchService(c).Apply(page.PlanID, []int32{index}); err != nil {
		t.Fatal(err)
	}
	text, err := a.Text(index)
	if err != nil || text != "新名称" {
		t.Fatalf("unicode text = %q err=%v", text, err)
	}
}

func TestBatchRejectsInvalidReplacement(t *testing.T) {
	c := NewCore()
	a := batchTestArchive(t, map[string]string{"x/a.stk": "[name]\n`skill-12`"})
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()
	_, err := NewBatchService(c).Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{"x/a.stk"},
		Text:  &TextReplaceSpec{Find: `skill-(\d+)`, Replacement: "$2", Regex: true},
	})
	if err == nil || !strings.Contains(err.Error(), "捕获组") {
		t.Fatalf("invalid replacement error = %v", err)
	}
}

func TestBatchStructuredPreviewAndApply(t *testing.T) {
	c := NewCore()
	a := batchTestArchive(t, map[string]string{
		"equipment/a.equ": "[price]\n100\n[need]\n1\n[/need]\n[name]\n`a`",
	})
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	svc := NewBatchService(c)
	page, err := svc.Preview(BatchRequest{
		Mode:  BatchModeStructured,
		Paths: []string{"equipment/a.equ"},
		Operations: []StructuredOperation{
			{Kind: "number", Section: "price", TokenIndex: 0, Operator: "+", Operand: "50"},
			{Kind: "insert", Section: "custom flag", AnchorSection: "name", Value: "1", HasEndTag: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.ChangedFiles != 1 || len(page.Rows) != 1 || len(page.Rows[0].Diff) == 0 {
		t.Fatalf("structured preview = %#v", page)
	}
	if _, err := svc.Apply(page.PlanID, []int32{page.Rows[0].FileIndex}); err != nil {
		t.Fatal(err)
	}
	index := page.Rows[0].FileIndex
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "150") || !strings.Contains(text, "[custom flag]") || !strings.Contains(text, "[/custom flag]") {
		t.Fatalf("structured applied text = %q", text)
	}
}

func TestBatchSetAllValuesReportsWarning(t *testing.T) {
	c := NewCore()
	a := batchTestArchive(t, map[string]string{
		"equipment/a.equ": "[values]\n1\n2",
	})
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	page, err := NewBatchService(c).Preview(BatchRequest{
		Mode:  BatchModeStructured,
		Paths: []string{"equipment/a.equ"},
		Operations: []StructuredOperation{{
			Kind: "set", Section: "values", TokenIndex: -1, Value: "10",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Status != BatchFileChanged || len(page.Rows[0].Warnings) != 1 {
		t.Fatalf("all-value preview = %#v", page.Rows)
	}
	if !strings.Contains(page.Rows[0].Warnings[0], "数量") {
		t.Fatalf("all-value warning = %#v", page.Rows[0].Warnings)
	}
	if _, err := NewBatchService(c).Apply(page.PlanID, []int32{page.Rows[0].FileIndex}); err != nil {
		t.Fatal(err)
	}
	text, err := a.Text(page.Rows[0].FileIndex)
	if err != nil || !strings.Contains(text, "10") || strings.Contains(text, "\n1\n") {
		t.Fatalf("all-value applied text = %q err=%v", text, err)
	}
}

func TestBatchPreviewPage(t *testing.T) {
	c := NewCore()
	files := make(map[string]string, 3)
	for index := 0; index < 3; index++ {
		files["x/"+string(rune('a'+index))+".stk"] = "[value]\n1"
	}
	a := batchTestArchive(t, files)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	svc := NewBatchService(c)
	first, err := svc.Preview(BatchRequest{
		Mode:  BatchModeText,
		Paths: []string{"x/a.stk", "x/b.stk", "x/c.stk"},
		Text:  &TextReplaceSpec{Find: "1", Replacement: "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor < 0 {
		// The service's default page is intentionally larger than this fixture;
		// exercise the explicit page size path instead.
		page, err := svc.PreviewPage(first.PlanID, 0, 1)
		if err != nil || len(page.Rows) != 1 || page.NextCursor < 0 {
			t.Fatalf("explicit preview page = %#v err=%v", page, err)
		}
		return
	}
	t.Fatal("fixture unexpectedly exceeded default page")
}
