package preview

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseANIStandardAnimation(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("[LOOP]\n\t1\n\n[SHADOW]\n\t0\n\n[FRAME MAX]\n\t105\n")
	for index := 0; index < 105; index++ {
		fmt.Fprintf(&builder, "\n[FRAME%03d]\n\n[IMAGE]\n\t`Item/Title/15_summer_2.img`\t%d\n\n[IMAGE POS]\n\t0\t0\n\n[DELAY]\n\t100\n", index, index)
	}

	document := ParseANI(builder.String())
	if !document.Valid {
		t.Fatalf("document invalid: %#v", document.Issues)
	}
	if !document.Loop || document.Shadow || document.FrameMax != 105 || len(document.Frames) != 105 {
		t.Fatalf("document metadata = %#v", document)
	}
	for index, frame := range document.Frames {
		if frame.Index != int32(index) || frame.Delay != 100 || len(frame.Layers) != 1 {
			t.Fatalf("frame %d = %#v", index, frame)
		}
		layer := frame.Layers[0]
		if layer.Path != "Item/Title/15_summer_2.img" || layer.Index != int32(index) || layer.X != 0 || layer.Y != 0 {
			t.Fatalf("frame %d layer = %#v", index, layer)
		}
	}
}

func TestParseANIMultipleLayersAndQuotedComments(t *testing.T) {
	text := "[LOOP] # play once\n" +
		" 0\n[SHADOW]\n 1\n[FRAME MAX]\n 1\n[FRAME000]\n" +
		"[IMAGE]\n  `Item/a``b.img` 2 # first layer\n[IMAGE POS]\n" +
		" -3 4\n[IMAGE]\n  `Item/second.img` 5\n[IMAGE POS]\n" +
		" 6 -7\n[DELAY]\n 80\n"

	document := ParseANI(text)
	if !document.Valid || document.Loop || !document.Shadow {
		t.Fatalf("document = %#v", document)
	}
	if len(document.Frames) != 1 || len(document.Frames[0].Layers) != 2 {
		t.Fatalf("layers = %#v", document.Frames)
	}
	first, second := document.Frames[0].Layers[0], document.Frames[0].Layers[1]
	if first.Path != "Item/a`b.img" || first.Index != 2 || first.X != -3 || first.Y != 4 {
		t.Fatalf("first layer = %#v", first)
	}
	if second.Path != "Item/second.img" || second.Index != 5 || second.X != 6 || second.Y != -7 {
		t.Fatalf("second layer = %#v", second)
	}
}

func TestParseANIRecoverableDefaultsAndStructuralErrors(t *testing.T) {
	text := "[FRAME MAX]\n 2\n[FRAME000]\n[IMAGE]\n `Item/a.img` 0\n" +
		"[FRAME002]\n[DELAY]\n 0\n"

	document := ParseANI(text)
	if document.Valid {
		t.Fatal("malformed document should not be valid")
	}
	if len(document.Frames) != 2 || document.Frames[0].Layers[0].X != 0 || document.Frames[0].Layers[0].Y != 0 {
		t.Fatalf("best effort frames = %#v", document.Frames)
	}
	if document.Frames[0].Delay != 100 || document.Frames[1].Delay != 100 {
		t.Fatalf("default delays = %#v", document.Frames)
	}
	assertIssue(t, document.Issues, IssueWarning, "图片缺少 [IMAGE POS]")
	assertIssue(t, document.Issues, IssueWarning, "帧缺少 [DELAY]")
	assertIssue(t, document.Issues, IssueWarning, "必须是正整数")
	assertIssue(t, document.Issues, IssueError, "帧编号不连续")
}

func TestParseANIDuplicateFrameAndMissingImage(t *testing.T) {
	text := "[FRAME MAX]\n 2\n[FRAME000]\n[FRAME000]\n[FRAME001]\n" +
		"[DELAY]\n 100\n"

	document := ParseANI(text)
	if document.Valid {
		t.Fatal("document with duplicate/missing frames should not be valid")
	}
	assertIssue(t, document.Issues, IssueError, "重复的帧编号")
	assertIssue(t, document.Issues, IssueError, "帧没有有效的 [IMAGE]")
}

func TestParseANIRejectsInvalidFrameNameAndImageIndex(t *testing.T) {
	text := "[FRAME MAX]\n 1\n[FRAMEoops]\n[IMAGE]\n `Item/test.img` -1\n"

	document := ParseANI(text)
	if document.Valid {
		t.Fatal("document with invalid frame syntax should not be valid")
	}
	assertIssue(t, document.Issues, IssueError, "帧 section 名称必须是 FRAME 加数字")
	assertIssue(t, document.Issues, IssueError, "声明帧数为 1，实际解析到 0 帧")

	invalidIndex := ParseANI("[FRAME MAX]\n 1\n[FRAME000]\n[IMAGE]\n `Item/test.img` -1\n")
	if invalidIndex.Valid {
		t.Fatal("document with invalid image index should not be valid")
	}
	assertIssue(t, invalidIndex.Issues, IssueError, "图片索引必须是非负整数")
}

func TestParseANIAllowsEmptyImageFrame(t *testing.T) {
	document := ParseANI("[FRAME MAX]\n 1\n[FRAME000]\n[IMAGE]\n[IMAGE POS]\n 3 4\n[DELAY]\n 50\n")
	if !document.Valid || len(document.Frames) != 1 || len(document.Frames[0].Layers) != 1 {
		t.Fatalf("empty image document = %#v", document)
	}
	layer := document.Frames[0].Layers[0]
	if layer.Path != "" || layer.Index != -1 || layer.X != 3 || layer.Y != 4 {
		t.Fatalf("empty image layer = %#v", layer)
	}
	assertIssue(t, document.Issues, IssueWarning, "图片引用为空")
}

func assertIssue(t *testing.T, issues []Issue, severity IssueSeverity, message string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Severity == severity && strings.Contains(issue.Message, message) {
			return
		}
	}
	t.Fatalf("missing %s issue containing %q: %#v", severity, message, issues)
}
