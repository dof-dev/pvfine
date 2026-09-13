package services

import (
	"fmt"
	"strings"
	"testing"
)

// listStyleLines builds a large body of distinct lines, mirroring how a real
// .lst file renders as one record per line.
func listStyleLines(records int) []string {
	lines := make([]string, 0, records)
	for index := 0; index < records; index++ {
		id := 10018 + index
		lines = append(lines, fmt.Sprintf("%d `character/common/jacket/cloth/vest_%d.equ`", id, id))
	}
	return lines
}

func countDiffKinds(diff []*BatchDiffLine) (removes, adds, context int) {
	for _, line := range diff {
		switch line.Kind {
		case "remove":
			removes++
		case "add":
			adds++
		default:
			context++
		}
	}
	return removes, adds, context
}

// A single line edit inside a large file must stay a single-line diff. Before
// common-affix trimming, any file over 1200 lines hit the size fallback and
// reported every line as removed and re-added.
func TestBuildBatchDiffKeepsLargeFileEditSmall(t *testing.T) {
	for _, records := range []int{940, 1201, 6000, 20000} {
		t.Run(fmt.Sprintf("%d-lines", records), func(t *testing.T) {
			before := listStyleLines(records)
			after := append([]string(nil), before...)
			after[records/2] = "10086 `character/common/amulet/100300001.equ`"

			diff, truncated := buildBatchDiff(strings.Join(before, "\n"), strings.Join(after, "\n"))
			if truncated {
				t.Fatalf("truncated = true, want a small exact diff")
			}
			removes, adds, context := countDiffKinds(diff)
			if removes != 1 || adds != 1 {
				t.Fatalf("removes = %d adds = %d, want 1 and 1", removes, adds)
			}
			// Only the changed line plus the surrounding context is emitted.
			if context != 2*batchDiffContext {
				t.Fatalf("context = %d, want %d", context, 2*batchDiffContext)
			}
		})
	}
}

// The trimming must not lose information: the reported line numbers still have
// to point at the original positions in both revisions.
func TestBuildBatchDiffLineNumbersAfterTrimming(t *testing.T) {
	before := listStyleLines(2000)
	after := append([]string(nil), before...)
	changedIndex := 1500
	after[changedIndex] = "9999 `character/common/amulet/target.equ`"

	diff, truncated := buildBatchDiff(strings.Join(before, "\n"), strings.Join(after, "\n"))
	if truncated {
		t.Fatal("unexpected truncation")
	}
	var remove, add *BatchDiffLine
	for _, line := range diff {
		if line.Kind == "remove" {
			remove = line
		}
		if line.Kind == "add" {
			add = line
		}
	}
	if remove == nil || add == nil {
		t.Fatalf("diff = %+v", diff)
	}
	if remove.OldLine != changedIndex+1 || add.NewLine != changedIndex+1 {
		t.Fatalf("line numbers = old %d new %d, want %d", remove.OldLine, add.NewLine, changedIndex+1)
	}
	// The change is reported with its original text on the remove side.
	if remove.Text != before[changedIndex] {
		t.Fatalf("remove text = %q, want %q", remove.Text, before[changedIndex])
	}
}

// Appending to the end of a large file must report only the appended lines.
func TestBuildBatchDiffLargeAppend(t *testing.T) {
	before := listStyleLines(5000)
	after := append(append([]string(nil), before...), "9001 `character/common/amulet/new.equ`")

	diff, truncated := buildBatchDiff(strings.Join(before, "\n"), strings.Join(after, "\n"))
	if truncated {
		t.Fatal("unexpected truncation")
	}
	removes, adds, _ := countDiffKinds(diff)
	if removes != 0 || adds != 1 {
		t.Fatalf("removes = %d adds = %d, want 0 and 1", removes, adds)
	}
}

// A genuinely large rewrite may still fall back to remove/add, but it must not
// silently drop one side: truncation has to be reported.
func TestBuildBatchDiffReportsTruncation(t *testing.T) {
	before := listStyleLines(5000)
	after := make([]string, 0, len(before))
	for _, line := range before {
		after = append(after, "rewritten "+line)
	}

	diff, truncated := buildBatchDiff(strings.Join(before, "\n"), strings.Join(after, "\n"))
	if !truncated {
		t.Fatal("expected a truncated diff for a full rewrite of a large file")
	}
	if len(diff) != batchMaxDiffLines {
		t.Fatalf("diff length = %d, want %d", len(diff), batchMaxDiffLines)
	}
}

// Identical content produces no diff at all.
func TestBuildBatchDiffIdenticalInput(t *testing.T) {
	text := strings.Join(listStyleLines(3000), "\n")
	if diff, truncated := buildBatchDiff(text, text); len(diff) != 0 || truncated {
		t.Fatalf("diff = %d lines truncated = %v, want empty", len(diff), truncated)
	}
}
