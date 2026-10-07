package services

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

type diagnosticTestWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	wrote  chan struct{}
}

func (w *diagnosticTestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(p)
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return n, err
}

func (w *diagnosticTestWriter) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func TestTerminalDiagnosticsBypassLogCapture(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(CaptureLogs(CaptureLogs(&output)))
	t.Cleanup(func() { log.SetOutput(original) })
	terminalDebugLog("terminal-only-marker")
	terminalDirectoryStage("equipment", "query", time.Now())
	terminalSlowDirectoryNode("equipment/a.equ", "tags", 60*time.Millisecond)
	for _, marker := range []string{"terminal-only-marker", "ListChildren.query", "ListChildren.slow-node tags"} {
		if !strings.Contains(output.String(), marker) {
			t.Fatalf("terminal missing %q: %s", marker, output.String())
		}
		for _, entry := range applicationLogs.snapshot() {
			if strings.Contains(entry.Message, marker) {
				t.Fatalf("terminal diagnostic entered UI log: %#v", entry)
			}
		}
	}
}

func TestDiagnoseLockWaitDumpsStacksWhileBlockedOnce(t *testing.T) {
	output := &diagnosticTestWriter{wrote: make(chan struct{}, 4)}
	original := log.Writer()
	log.SetOutput(CaptureLogs(output))
	t.Cleanup(func() { log.SetOutput(original) })
	var mu sync.RWMutex
	mu.RLock()
	release := sync.OnceFunc(mu.RUnlock)
	defer release()
	done := make(chan struct{})
	go func() {
		defer close(done)
		diagnoseLockWait("test-write", "file=42", 10*time.Millisecond, mu.Lock)
		mu.Unlock()
	}()
	select {
	case <-output.wrote:
	case <-time.After(5 * time.Second):
		release()
		<-done
		t.Fatal("missing stack dump while lock is still held")
	}
	text := output.text()
	if !strings.Contains(text, "core-lock slow-wait operation=test-write file=42") ||
		!strings.Contains(text, "goroutine ") || !strings.Contains(text, "diagnoseLockWait") {
		t.Fatalf("unexpected dump: %s", text)
	}
	time.Sleep(30 * time.Millisecond)
	if strings.Count(output.text(), "core-lock slow-wait") != 1 {
		t.Fatal("a single wait should emit only one dump")
	}
	release()
	<-done
}

func TestDiagnoseLockWaitCancelsFastAcquisition(t *testing.T) {
	output := &diagnosticTestWriter{wrote: make(chan struct{}, 1)}
	original := log.Writer()
	log.SetOutput(CaptureLogs(output))
	t.Cleanup(func() { log.SetOutput(original) })
	var mu sync.RWMutex
	diagnoseLockWait("test-fast", "", 10*time.Millisecond, mu.Lock)
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	if text := output.text(); text != "" {
		t.Fatalf("fast acquisition emitted diagnostics: %s", text)
	}
}
