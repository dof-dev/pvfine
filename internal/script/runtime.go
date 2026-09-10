// Package script contains the sandboxed JavaScript automation layer.
//
// The package deliberately exposes a small host surface to the JavaScript
// runtime. It must not be widened to pass pvf.Archive or operating-system
// handles into Goja.
package script

import "context"

const (
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"

	ErrorKindCompile   = "compile"
	ErrorKindRuntime   = "runtime"
	ErrorKindHost      = "host"
	ErrorKindCancelled = "cancelled"
	ErrorKindTimeout   = "timeout"

	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Diagnostic is a source-positioned compiler or runtime diagnostic.
type Diagnostic struct {
	Kind    string
	Message string
	Stack   string
	Line    int
	Column  int
}

// CompileResult is the engine-independent syntax-check result.
type CompileResult struct {
	Valid       bool
	Diagnostics []Diagnostic
}

// LogEntry is one message emitted by console or pvf.log.
type LogEntry struct {
	Level   string
	Message string
}

// Progress is a user-visible progress update emitted by pvf.progress.
type Progress struct {
	Done        int
	Total       int
	Message     string
	CurrentPath string
}

// RunResult is the engine-independent result of one sandbox execution.
type RunResult struct {
	Status        string
	Error         *Diagnostic
	ScannedFiles  int
	ModifiedFiles int
	Logs          []LogEntry
}

// ScriptRuntime is the replaceable engine boundary. Implementations must run
// all JavaScript callbacks on one goroutine and must not expose the concrete
// PVF archive to the script.
type ScriptRuntime interface {
	Compile(source string) CompileResult
	Run(ctx context.Context, source string, host *BatchAPI) (RunResult, error)
}
