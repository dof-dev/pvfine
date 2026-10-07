package services

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const coreLockDiagnosticDelay = time.Second

var terminalDiagnosticMu sync.Mutex

// Bypass CaptureLogs so large diagnostic dumps never enter the UI log buffer.
func terminalDebugLog(format string, args ...any) {
	output := log.Writer()
	for {
		capture, ok := output.(*logCaptureWriter)
		if !ok {
			break
		}
		output = capture.output
	}
	terminalDiagnosticMu.Lock()
	defer terminalDiagnosticMu.Unlock()
	log.New(output, "", log.LstdFlags|log.Lmicroseconds).Printf("[DEBUG] [diagnostic] "+format, args...)
}

func diagnoseCoreLock(operation, detail string, lock func()) {
	diagnoseLockWait(operation, detail, coreLockDiagnosticDelay, lock)
}

func diagnoseLockWait(operation, detail string, delay time.Duration, lock func()) {
	started := time.Now()
	var acquired atomic.Bool
	timer := time.AfterFunc(delay, func() {
		if acquired.Load() {
			return
		}
		stack := make([]byte, 64<<10)
		var size int
		for {
			size = runtime.Stack(stack, true)
			if size < len(stack) || len(stack) >= 8<<20 {
				break
			}
			stack = make([]byte, len(stack)*2)
		}
		if acquired.Load() {
			return
		}
		terminalDebugLog("core-lock slow-wait operation=%s %s wait=%s stack-truncated=%t\n%s",
			operation, detail, time.Since(started), size == len(stack), stack[:size])
	})
	lock()
	acquired.Store(true)
	timer.Stop()
}

func terminalDirectoryStage(path, stage string, started time.Time) {
	terminalDebugLog("ListChildren.%s path=%s elapsed=%s", stage, path, time.Since(started))
}

func terminalSlowDirectoryNode(path, stage string, elapsed time.Duration) {
	if elapsed >= 50*time.Millisecond {
		terminalDebugLog("ListChildren.slow-node %s path=%s elapsed=%s", stage, path, elapsed)
	}
}
