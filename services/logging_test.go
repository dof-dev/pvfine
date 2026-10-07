package services

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
)

func TestLogBufferBoundedAndSnapshotIndependent(t *testing.T) {
	var buffer logBuffer
	for i := 0; i < logCapacity+10; i++ {
		buffer.append("INFO", "test", fmt.Sprint(i))
	}
	snapshot := buffer.snapshot()
	if len(snapshot) != logCapacity || snapshot[0].ID != 11 || snapshot[len(snapshot)-1].ID != logCapacity+10 {
		t.Fatalf("unexpected snapshot bounds: %d, %d, %d", len(snapshot), snapshot[0].ID, snapshot[len(snapshot)-1].ID)
	}
	snapshot[0].Message = "changed"
	if buffer.snapshot()[0].Message == "changed" {
		t.Fatal("snapshot aliases the buffer")
	}
}

func TestLogBufferConcurrent(t *testing.T) {
	var buffer logBuffer
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			for j := 0; j < 100; j++ {
				buffer.append("INFO", "test", "entry")
				buffer.snapshot()
			}
		})
	}
	workers.Wait()
	snapshot := buffer.snapshot()
	if len(snapshot) != 800 {
		t.Fatalf("entries = %d", len(snapshot))
	}
	for i, entry := range snapshot {
		if entry.ID != uint64(i+1) {
			t.Fatalf("non-sequential ID at %d: %d", i, entry.ID)
		}
	}
}

func TestClassifyLogLevel(t *testing.T) {
	for _, test := range []struct{ message, level string }{
		{"[ERR] broken", "ERROR"},
		{"level=ERROR msg=broken", "ERROR"},
		{"index failed: error=broken", "ERROR"},
		{"打开失败", "ERROR"},
		{"prepare finished elapsed=2ms error=<nil>", "INFO"},
		{"level=WARN msg=warning", "WARN"},
		{"cache publication unavailable", "WARN"},
		{"level=DEBUG msg=trace", "DEBUG"},
		{"index finished elapsed=100ms", "INFO"},
		{"index finished skipped=0", "INFO"},
	} {
		if got := classifyLogLevel(test.message); got != test.level {
			t.Errorf("%q: got %s, want %s", test.message, got, test.level)
		}
	}
}

func TestServiceErrorsAreLoggedWithoutChangingSerialization(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(CaptureLogs(&output))
	t.Cleanup(func() { log.SetOutput(original) })
	if result := LogServiceError(errors.New("service failure")); result != nil {
		t.Fatal("must use the default Wails serialization")
	}
	if !strings.Contains(output.String(), "[ERR] service failure") {
		t.Fatalf("missing error output: %s", output.String())
	}
	snapshot := applicationLogs.snapshot()
	last := snapshot[len(snapshot)-1]
	if last.Level != "ERROR" || !strings.Contains(last.Message, "service failure") {
		t.Fatalf("error entry = %#v", last)
	}
	if result := LogServiceError(nil); result != nil {
		t.Fatal("nil errors must also use default serialization")
	}
}

func TestCaptureLogsPreservesOutput(t *testing.T) {
	var output bytes.Buffer
	writer := CaptureLogs(&output)
	line := "[ERR] 日志错误\n"
	n, err := writer.Write([]byte(line))
	if err != nil || n != len(line) || output.String() != line {
		t.Fatalf("write = %d, %v, %q", n, err, output.String())
	}
}

func TestDescribeArchiveAndIndexLogs(t *testing.T) {
	level, message, _, _ := describeLogEvent("archive:opened", ArchiveInfo{
		Path: "test.pvf", FileCount: 20, Format: "paged110", Paged110: true,
	})
	if level != "INFO" || !strings.Contains(message, "test.pvf") || !strings.Contains(message, "文件=20") || !strings.Contains(message, "变体=paged110") {
		t.Fatalf("archive log = %s, %s", level, message)
	}
	first := IndexStatus{State: "building", Stage: "strings", Done: 1, Total: 100}
	_, _, key, signature := describeLogEvent("archive:index-progress", first)
	first.Done++
	_, _, nextKey, nextSignature := describeLogEvent("archive:index-progress", first)
	if key != nextKey || signature != nextSignature {
		t.Fatal("per-file progress must not produce a new stage signature")
	}
	first.State, first.Stage, first.BuildDurationMs, first.OpenDurationMs = "ready", "ready-cache", 42, 12
	_, message, _, nextSignature = describeLogEvent("archive:index-ready", first)
	if signature == nextSignature || !strings.Contains(message, "42.00 ms") || !strings.Contains(message, "12.00 ms") {
		t.Fatalf("ready log missing timing: %s", message)
	}
	first.RefreshError = "refresh failed"
	level, message, _, _ = describeLogEvent("archive:index-ready", first)
	if level != "ERROR" || !strings.Contains(message, "refresh failed") {
		t.Fatalf("refresh error = %s, %s", level, message)
	}
}

func TestDescribeOtherEvents(t *testing.T) {
	for _, name := range []string{"archive:closed", "archive:file-index-ready", "archive:script-applied", "version:committed"} {
		if _, message, _, _ := describeLogEvent(name, nil); message == "" {
			t.Errorf("%s has no log", name)
		}
	}
	level, message, _, _ := describeLogEvent("script:log", map[string]any{"level": "warn", "message": "脚本警告"})
	if level != "WARN" || message != "脚本警告" {
		t.Fatalf("script log = %s, %s", level, message)
	}
	level, message, _, _ = describeLogEvent("image:index-error", ImageIndexStatus{State: "error", Error: "broken", BuildDurationMs: 10})
	if level != "ERROR" || !strings.Contains(message, "broken") || !strings.Contains(message, "10.00 ms") {
		t.Fatalf("image log = %s, %s", level, message)
	}
}
