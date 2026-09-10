package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"pvfine/internal/pvf"
	scriptengine "pvfine/internal/script"
	pvfversion "pvfine/internal/version"
)

const (
	ScriptRunCompleted = "completed"
	ScriptRunFailed    = "failed"
	ScriptRunCancelled = "cancelled"

	ScriptErrorCompile   = "compile"
	ScriptErrorRuntime   = "runtime"
	ScriptErrorHost      = "host"
	ScriptErrorCancelled = "cancelled"
	ScriptErrorTimeout   = "timeout"

	ScriptFileChanged = "changed"
	ScriptFileSkipped = "skipped"
	ScriptFileError   = "error"

	scriptMaxSourceBytes = 1 << 20
	scriptMaxLogs        = 5000
	scriptRunTimeout     = 5 * time.Minute
	scriptPageSize       = 100
)

var (
	ErrScriptBusy      = errors.New("脚本正在运行")
	ErrScriptPlanStale = errors.New("脚本预览已过期,请重新运行")

	scriptPlanSequence uint64
	scriptRunSequence  uint64
)

// ScriptService executes user scripts against an isolated archive stage and
// exposes the retained preview plan to the frontend.
type ScriptService struct {
	c       *core
	runtime scriptengine.ScriptRuntime

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

	directory string
	initErr   error
}

// NewScriptService creates the script service under the same user config root
// used by SettingsService and the other user-managed resources.
func NewScriptService(c *core) *ScriptService {
	directory, err := os.UserConfigDir()
	if err != nil {
		return &ScriptService{c: c, runtime: scriptengine.NewGojaRuntime(), initErr: fmt.Errorf("获取脚本目录失败: %w", err)}
	}
	return newScriptService(c, scriptengine.NewGojaRuntime(), filepath.Join(directory, "pvfine", "scripts"))
}

func newScriptService(c *core, runtime scriptengine.ScriptRuntime, directory string) *ScriptService {
	if runtime == nil {
		runtime = scriptengine.NewGojaRuntime()
	}
	return &ScriptService{c: c, runtime: runtime, directory: directory}
}

// invalidateScriptLocked aborts a running VM and releases any retained
// preview transaction. The caller must hold c.mu.
func (c *core) invalidateScriptLocked() {
	if c == nil {
		return
	}
	if c.scriptCancel != nil {
		c.scriptCancel()
		c.scriptCancel = nil
	}
	c.invalidateScriptPlanLocked()
}

// invalidateScriptPlanLocked releases a staged preview without cancelling a
// currently executing run. The caller must hold c.mu.
func (c *core) invalidateScriptPlanLocked() {
	if c == nil {
		return
	}
	if c.scriptPlan != nil {
		c.scriptPlan.transaction.Rollback()
		c.scriptPlan = nil
	}
}

type ScriptCompileResult struct {
	Valid       bool                `json:"valid"`
	Diagnostics []*ScriptDiagnostic `json:"diagnostics"`
}

type ScriptDiagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

type ScriptRunRequest struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source"`
}

type ScriptRunResult struct {
	RunID         string       `json:"runId"`
	Status        string       `json:"status"`
	PlanID        string       `json:"planId,omitempty"`
	ScannedFiles  int          `json:"scannedFiles"`
	ModifiedFiles int          `json:"modifiedFiles"`
	DurationMs    int64        `json:"durationMs"`
	Logs          []*ScriptLog `json:"logs"`
	Error         *ScriptError `json:"error,omitempty"`
}

type ScriptError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

type ScriptLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ScriptProgress struct {
	RunID       string `json:"runId"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	Message     string `json:"message,omitempty"`
	CurrentPath string `json:"currentPath,omitempty"`
}

type ScriptPreviewPage struct {
	PlanID        string               `json:"planId"`
	NextCursor    int                  `json:"nextCursor"`
	ScannedFiles  int                  `json:"scannedFiles"`
	ModifiedFiles int                  `json:"modifiedFiles"`
	Rows          []*ScriptFilePreview `json:"rows"`
}

type ScriptFilePreview struct {
	FileIndex     int32            `json:"fileIndex"`
	Path          string           `json:"path"`
	Status        string           `json:"status"`
	MatchCount    int              `json:"matchCount"`
	Reason        string           `json:"reason,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Diff          []*BatchDiffLine `json:"diff,omitempty"`
	DiffTruncated bool             `json:"diffTruncated,omitempty"`
}

type ScriptApplyResult struct {
	AppliedFiles  int32   `json:"appliedFiles"`
	FileIndexes   []int32 `json:"fileIndexes"`
	ModifiedCount int     `json:"modifiedCount"`
	Revision      uint64  `json:"revision"`
}

type ScriptFile struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

type scriptPlan struct {
	id            string
	label         string
	archive       *pvf.Archive
	revision      uint64
	transaction   *scriptengine.Transaction
	rows          []scriptPlanRow
	scannedFiles  int
	modifiedFiles int
}

type scriptPlanRow struct {
	preview   ScriptFilePreview
	afterText string
}

// Compile validates script syntax without reading the current archive.
func (s *ScriptService) Compile(source string) ScriptCompileResult {
	if len([]byte(source)) > scriptMaxSourceBytes {
		return ScriptCompileResult{
			Valid: false,
			Diagnostics: []*ScriptDiagnostic{{
				Kind:    ScriptErrorCompile,
				Message: fmt.Sprintf("脚本长度不能超过 %d 字节", scriptMaxSourceBytes),
			}},
		}
	}
	result := s.runtime.Compile(source)
	return ScriptCompileResult{Valid: result.Valid, Diagnostics: scriptDiagnostics(result.Diagnostics)}
}

// Run executes the script against a transaction and retains a diff plan. It
// never changes the live archive. The Wails context parameter is hidden from
// generated TypeScript bindings and is cancelled by the explicit Cancel call
// or by the caller closing the cancellable promise.
func (s *ScriptService) Run(ctx context.Context, request ScriptRunRequest) (ScriptRunResult, error) {
	if s.initErr != nil {
		return ScriptRunResult{}, s.initErr
	}
	if len([]byte(request.Source)) > scriptMaxSourceBytes {
		return ScriptRunResult{}, fmt.Errorf("脚本长度不能超过 %d 字节", scriptMaxSourceBytes)
	}
	compile := s.Compile(request.Source)
	if !compile.Valid {
		return ScriptRunResult{
			RunID:  nextScriptRunID(),
			Status: ScriptRunFailed,
			Error:  firstScriptDiagnostic(compile.Diagnostics),
		}, nil
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ScriptRunResult{}, ErrScriptBusy
	}
	s.running = true
	s.mu.Unlock()

	startedAt := time.Now()
	runID := nextScriptRunID()
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, scriptRunTimeout)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.running = false
		s.mu.Unlock()
		s.c.mu.Lock()
		if s.c.scriptCancel != nil {
			s.c.scriptCancel = nil
		}
		s.c.mu.Unlock()
	}()

	s.c.mu.RLock()
	a := s.c.archive
	revision := s.c.batchRevision
	if a != nil {
		tx := scriptengine.NewTransaction(a)
		s.c.mu.RUnlock()

		// The core uses this cancellation hook when the active archive changes
		// or closes while the script is running. Install it before entering the
		// VM so a long-running script cannot outlive its archive context.
		s.c.mu.Lock()
		if s.c.archive != a || s.c.batchRevision != revision {
			s.c.mu.Unlock()
			tx.Rollback()
			return ScriptRunResult{}, ErrScriptPlanStale
		}
		s.c.scriptCancel = cancel
		s.c.invalidateScriptPlanLocked()
		s.c.mu.Unlock()

		logEntries := make([]*ScriptLog, 0)
		logCount := 0
		appendLog := func(entry scriptengine.LogEntry) {
			if logCount < scriptMaxLogs {
				logEntries = append(logEntries, &ScriptLog{Level: entry.Level, Message: entry.Message})
				emitEvent("script:log", map[string]any{
					"runId":   runID,
					"level":   entry.Level,
					"message": entry.Message,
				})
				logCount++
				return
			}
			if logCount == scriptMaxLogs {
				logCount++
				emitEvent("script:log", map[string]any{
					"runId":   runID,
					"level":   scriptengine.LogLevelWarn,
					"message": fmt.Sprintf("日志已达到 %d 条上限,后续日志已省略", scriptMaxLogs),
				})
			}
		}
		appendProgress := func(progress scriptengine.Progress) {
			emitEvent("script:progress", ScriptProgress{
				RunID: runID, Done: progress.Done, Total: progress.Total,
				Message: progress.Message, CurrentPath: progress.CurrentPath,
			})
		}
		host := scriptengine.NewBatchAPI(runCtx, tx, appendLog, appendProgress)
		emitEvent("script:state", map[string]any{"runId": runID, "status": "running"})
		runtimeResult, runErr := s.runtime.Run(runCtx, request.Source, host)
		if runErr != nil {
			tx.Rollback()
			return ScriptRunResult{}, runErr
		}
		result := ScriptRunResult{
			RunID:         runID,
			Status:        runtimeResult.Status,
			ScannedFiles:  runtimeResult.ScannedFiles,
			ModifiedFiles: runtimeResult.ModifiedFiles,
			DurationMs:    time.Since(startedAt).Milliseconds(),
			Logs:          logEntries,
			Error:         scriptError(runtimeResult.Error),
		}
		if runtimeResult.Status != scriptengine.RunStatusCompleted {
			tx.Rollback()
			emitEvent("script:state", map[string]any{"runId": runID, "status": runtimeResult.Status, "error": result.Error})
			return result, nil
		}

		changedIndexes, changedErr := tx.ChangedIndexes()
		if changedErr != nil {
			tx.Rollback()
			return ScriptRunResult{}, changedErr
		}
		plan := &scriptPlan{
			id:            nextScriptPlanID(),
			label:         strings.TrimSpace(request.Name),
			archive:       a,
			revision:      revision,
			transaction:   tx,
			scannedFiles:  runtimeResult.ScannedFiles,
			modifiedFiles: len(changedIndexes),
			rows:          make([]scriptPlanRow, 0, len(changedIndexes)),
		}
		for _, index := range changedIndexes {
			beforeText, beforeErr := tx.OriginalText(index)
			if beforeErr != nil {
				tx.Rollback()
				return ScriptRunResult{}, beforeErr
			}
			afterText, afterErr := tx.Stage().Text(index)
			if afterErr != nil {
				tx.Rollback()
				return ScriptRunResult{}, afterErr
			}
			plan.rows = append(plan.rows, scriptPlanRow{
				preview: ScriptFilePreview{
					FileIndex:  index,
					Path:       tx.Stage().Path(index),
					Status:     ScriptFileChanged,
					MatchCount: 1,
					Diff:       nil,
				},
				afterText: afterText,
			})
			plan.rows[len(plan.rows)-1].preview.Diff,
				plan.rows[len(plan.rows)-1].preview.DiffTruncated = buildBatchDiff(beforeText, afterText)
		}
		sort.SliceStable(plan.rows, func(left, right int) bool {
			leftPath := strings.ToLower(plan.rows[left].preview.Path)
			rightPath := strings.ToLower(plan.rows[right].preview.Path)
			if leftPath != rightPath {
				return leftPath < rightPath
			}
			return plan.rows[left].preview.FileIndex < plan.rows[right].preview.FileIndex
		})

		s.c.mu.Lock()
		if s.c.archive != a || s.c.batchRevision != revision {
			s.c.mu.Unlock()
			tx.Rollback()
			return ScriptRunResult{}, ErrScriptPlanStale
		}
		s.c.scriptPlan = plan
		s.c.mu.Unlock()
		result.PlanID = plan.id
		emitEvent("script:state", map[string]any{"runId": runID, "status": ScriptRunCompleted, "planId": plan.id})
		return result, nil
	}
	s.c.mu.RUnlock()
	return ScriptRunResult{}, ErrNoArchive
}

// Cancel requests cancellation of the active script. It is idempotent.
func (s *ScriptService) Cancel() bool {
	s.mu.Lock()
	cancel := s.cancel
	running := s.running
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return running
}

// IsRunning reports whether a script call is currently executing.
func (s *ScriptService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ScriptService) PreviewPage(planID string, cursor, limit int) (*ScriptPreviewPage, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	plan, err := s.currentScriptPlanLocked(planID)
	if err != nil {
		return nil, err
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(plan.rows) {
		cursor = len(plan.rows)
	}
	if limit <= 0 || limit > 500 {
		limit = scriptPageSize
	}
	end := cursor + limit
	if end > len(plan.rows) {
		end = len(plan.rows)
	}
	rows := make([]*ScriptFilePreview, 0, end-cursor)
	for index := cursor; index < end; index++ {
		row := plan.rows[index].preview
		row.Warnings = append([]string(nil), row.Warnings...)
		row.Diff = append([]*BatchDiffLine(nil), row.Diff...)
		rows = append(rows, &row)
	}
	next := -1
	if end < len(plan.rows) {
		next = end
	}
	return &ScriptPreviewPage{
		PlanID: plan.id, NextCursor: next,
		ScannedFiles: plan.scannedFiles, ModifiedFiles: plan.modifiedFiles,
		Rows: rows,
	}, nil
}

// Apply commits exactly the selected changed rows from one script plan.
func (s *ScriptService) Apply(planID string, fileIndexes []int32) (ScriptApplyResult, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ScriptApplyResult{}, ErrScriptBusy
	}
	defer s.mu.Unlock()
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, err
	}
	plan, err := s.currentScriptPlanLocked(planID)
	if err != nil {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, err
	}
	if len(fileIndexes) == 0 {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, fmt.Errorf("没有选中的脚本变更文件")
	}
	rowsByIndex := make(map[int32]*scriptPlanRow, len(plan.rows))
	for index := range plan.rows {
		row := &plan.rows[index]
		rowsByIndex[row.preview.FileIndex] = row
	}
	selected := make(map[int32]struct{}, len(fileIndexes))
	ordered := make([]int32, 0, len(fileIndexes))
	for _, index := range fileIndexes {
		if _, exists := selected[index]; exists {
			continue
		}
		row, exists := rowsByIndex[index]
		if !exists || row.preview.Status != ScriptFileChanged {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, fmt.Errorf("文件 %d 不是可应用的脚本结果", index)
		}
		selected[index] = struct{}{}
		ordered = append(ordered, index)
	}
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		paths := make([]string, 0, len(ordered))
		for _, index := range ordered {
			paths = append(paths, plan.archive.Path(index))
		}
		before, err = pvfversion.ContentSnapshotFromArchive(plan.archive, paths)
		if err != nil {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, err
		}
	}
	if err := plan.transaction.Commit(selected); err != nil {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, err
	}
	if s.c.versionRepo != nil {
		paths := make([]string, 0, len(ordered))
		for _, index := range ordered {
			paths = append(paths, plan.archive.Path(index))
		}
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(plan.archive, paths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, snapshotErr
		}
		label := "脚本"
		if plan.label != "" {
			label = "脚本 " + plan.label
		}
		if recordErr := s.c.recordVersionMutationLocked(label, before, after); recordErr != nil {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, recordErr
		}
	}
	if s.c.editorText == nil {
		s.c.editorText = make(map[int32]string)
	}
	for _, index := range ordered {
		s.c.editorText[index] = rowsByIndex[index].afterText
	}
	s.c.batchRevision++
	s.c.batchPlan = nil
	s.c.invalidateScriptPlanLocked()
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.invalidateAdvancedSearchLocked()
	info := plan.archive.Info()
	revision := s.c.batchRevision
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	emitEvent("archive:advanced-search-stale", map[string]any{"script": true})
	emitEvent("archive:batch-applied", map[string]any{
		"fileIndexes": ordered, "modifiedCount": info.ModifiedCount,
		"revision": revision, "source": "script",
	})
	emitEvent("archive:script-applied", map[string]any{
		"fileIndexes": ordered, "modifiedCount": info.ModifiedCount,
		"revision": revision,
	})
	if versioned {
		emitVersionState(s.c, "script-applied")
	}
	s.c.startSearchIndex()
	return ScriptApplyResult{
		AppliedFiles: int32(len(ordered)), FileIndexes: ordered,
		ModifiedCount: info.ModifiedCount, Revision: revision,
	}, nil
}

func (s *ScriptService) Discard(planID string) {
	s.c.mu.Lock()
	if s.c.scriptPlan != nil && s.c.scriptPlan.id == planID {
		s.c.scriptPlan.transaction.Rollback()
		s.c.scriptPlan = nil
	}
	s.c.mu.Unlock()
}

func (s *ScriptService) currentScriptPlanLocked(planID string) (*scriptPlan, error) {
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	plan := s.c.scriptPlan
	if plan == nil || plan.id != planID || plan.archive != s.c.archive || plan.revision != s.c.batchRevision {
		return nil, ErrScriptPlanStale
	}
	return plan, nil
}

func nextScriptPlanID() string {
	return fmt.Sprintf("script-%d", atomic.AddUint64(&scriptPlanSequence, 1))
}

func nextScriptRunID() string {
	return fmt.Sprintf("script-run-%d", atomic.AddUint64(&scriptRunSequence, 1))
}

func scriptDiagnostics(values []scriptengine.Diagnostic) []*ScriptDiagnostic {
	result := make([]*ScriptDiagnostic, 0, len(values))
	for _, value := range values {
		result = append(result, &ScriptDiagnostic{
			Kind: value.Kind, Message: value.Message, Stack: value.Stack,
			Line: value.Line, Column: value.Column,
		})
	}
	return result
}

func firstScriptDiagnostic(values []*ScriptDiagnostic) *ScriptError {
	if len(values) == 0 || values[0] == nil {
		return &ScriptError{Kind: ScriptErrorCompile, Message: "脚本编译失败"}
	}
	value := values[0]
	return &ScriptError{Kind: value.Kind, Message: value.Message, Stack: value.Stack, Line: value.Line, Column: value.Column}
}

func scriptError(value *scriptengine.Diagnostic) *ScriptError {
	if value == nil {
		return nil
	}
	return &ScriptError{Kind: value.Kind, Message: value.Message, Stack: value.Stack, Line: value.Line, Column: value.Column}
}

func (s *ScriptService) ScriptDirectory() (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	if err := os.MkdirAll(s.directory, 0o755); err != nil {
		return "", fmt.Errorf("创建脚本目录失败: %w", err)
	}
	return s.directory, nil
}

func (s *ScriptService) ListScripts() ([]*ScriptFile, error) {
	directory, err := s.ScriptDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("读取脚本目录失败: %w", err)
	}
	result := make([]*ScriptFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(strings.ToLower(entry.Name()), ".pvf.js") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		result = append(result, &ScriptFile{
			Name: entry.Name(), Size: info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339Nano),
		})
	}
	sort.SliceStable(result, func(left, right int) bool {
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	return result, nil
}

func (s *ScriptService) LoadScript(name string) (string, error) {
	path, err := s.scriptPath(name)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("读取脚本失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("脚本必须是脚本目录内的普通文件")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取脚本失败: %w", err)
	}
	if len(data) > scriptMaxSourceBytes {
		return "", fmt.Errorf("脚本长度不能超过 %d 字节", scriptMaxSourceBytes)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("脚本必须是 UTF-8 文本")
	}
	return string(data), nil
}

func (s *ScriptService) SaveScript(name, source string) error {
	path, err := s.scriptPath(name)
	if err != nil {
		return err
	}
	if len([]byte(source)) > scriptMaxSourceBytes {
		return fmt.Errorf("脚本长度不能超过 %d 字节", scriptMaxSourceBytes)
	}
	if !utf8.ValidString(source) {
		return fmt.Errorf("脚本必须是 UTF-8 文本")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".pvfine-script-*.tmp")
	if err != nil {
		return fmt.Errorf("创建脚本临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(source); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入脚本失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步脚本失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭脚本失败: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("保存脚本失败: %w", err)
	}
	return nil
}

func (s *ScriptService) OpenScriptDirectory() error {
	directory, err := s.ScriptDirectory()
	if err != nil {
		return err
	}
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{directory}
	case "windows":
		command, args = "explorer.exe", []string{directory}
	case "linux":
		command, args = "xdg-open", []string{directory}
	default:
		return fmt.Errorf("当前平台不支持打开脚本目录")
	}
	if err := exec.Command(command, args...).Start(); err != nil {
		return fmt.Errorf("打开脚本目录失败: %w", err)
	}
	return nil
}

func (s *ScriptService) scriptPath(name string) (string, error) {
	directory, err := s.ScriptDirectory()
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("脚本名称不能为空")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pvf.js") {
		name += ".pvf.js"
	}
	if filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("脚本名称必须是目录内的 .pvf.js 文件名")
	}
	path := filepath.Join(directory, name)
	relative, err := filepath.Rel(directory, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("脚本路径越界")
	}
	return path, nil
}
