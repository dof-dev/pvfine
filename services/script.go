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
	ScriptFileAdded   = "added"
	ScriptFileDeleted = "deleted"
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

	// fileSets is the same persistence-backed service the sidebar saves through,
	// so a scripted change lands in file-sets.json instead of a private copy.
	fileSets *FileSetService

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

	directory string
	initErr   error
}

// NewScriptService creates the script service under the same user config root
// used by SettingsService and the other user-managed resources.
func NewScriptService(c *core, fileSets *FileSetService) *ScriptService {
	directory, err := os.UserConfigDir()
	if err != nil {
		return &ScriptService{c: c, runtime: scriptengine.NewGojaRuntime(), fileSets: fileSets, initErr: fmt.Errorf("获取脚本目录失败: %w", err)}
	}
	return newScriptService(c, scriptengine.NewGojaRuntime(), filepath.Join(directory, "pvfine", "scripts"), fileSets)
}

func newScriptService(c *core, runtime scriptengine.ScriptRuntime, directory string, fileSets *FileSetService) *ScriptService {
	if runtime == nil {
		runtime = scriptengine.NewGojaRuntime()
	}
	return &ScriptService{c: c, runtime: runtime, fileSets: fileSets, directory: directory}
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
	RunID          string       `json:"runId"`
	Status         string       `json:"status"`
	PlanID         string       `json:"planId,omitempty"`
	ScannedFiles   int          `json:"scannedFiles"`
	ModifiedFiles  int          `json:"modifiedFiles"`
	FileSetChanges int          `json:"fileSetChanges"`
	DurationMs     int64        `json:"durationMs"`
	Logs           []*ScriptLog `json:"logs"`
	Error          *ScriptError `json:"error,omitempty"`
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
	PlanID     string `json:"planId"`
	NextCursor int    `json:"nextCursor"`
	// Filter echoes the path filter this page was built with.
	Filter string `json:"filter,omitempty"`
	// MatchedFiles counts every row matching the filter across all pages, so
	// the UI can report an accurate total instead of only what is loaded.
	MatchedFiles  int                  `json:"matchedFiles"`
	ScannedFiles  int                  `json:"scannedFiles"`
	ModifiedFiles int                  `json:"modifiedFiles"`
	Rows          []*ScriptFilePreview `json:"rows"`
	// FileSetRows lists the staged file set mutations. They are not diffed rows,
	// but the apply step must show and select them alongside archive changes.
	FileSetRows []*ScriptFileSetPreview `json:"fileSetRows,omitempty"`
	// FileSetSelectKey is the change key the frontend selects to apply every
	// file set mutation, mirroring how archive rows use ChangeKey.
	FileSetSelectKey string `json:"fileSetSelectKey,omitempty"`
}

// ScriptFileSetPreview describes one staged file set mutation.
type ScriptFileSetPreview struct {
	ChangeKey string `json:"changeKey"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	// Added and Removed are the path-level difference against the persisted set,
	// so the UI can show what a setAll actually changed.
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	// Count is the resulting entry total after the change.
	Count int `json:"count"`
}

type ScriptFilePreview struct {
	// ChangeKey is the stable selection key. Structural changes shift entry
	// indexes, so the frontend must select rows by this key, not FileIndex.
	ChangeKey     string           `json:"changeKey"`
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
	// Structural reports whether the commit changed the entry table, which
	// means every previously previewed file index is now stale.
	Structural bool `json:"structural"`
	// AppliedFileSets counts the file set mutations written to user config.
	AppliedFileSets int `json:"appliedFileSets"`
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

	// fileSets is the run's baseline snapshot, retained so the apply step can
	// tell an edit from a create when merging into the persisted document.
	fileSets *scriptengine.FileSetStage

	fileSetRows []*ScriptFileSetPreview
}

type scriptPlanRow struct {
	preview   ScriptFilePreview
	afterText string
	// change is the staged mutation used to build the commit payload.
	change scriptengine.Change
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
		// File sets are a user-config resource, not archive content, so the run
		// reads them through the same service the sidebar saves through and
		// stages its edits for the apply step.
		host.SetFileSetStage(scriptengine.NewFileSetStage(s.loadFileSetSnapshot()))
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

		changes, changesErr := tx.Changes()
		if changesErr != nil {
			tx.Rollback()
			return ScriptRunResult{}, changesErr
		}
		plan := &scriptPlan{
			id:            nextScriptPlanID(),
			label:         strings.TrimSpace(request.Name),
			archive:       a,
			revision:      revision,
			transaction:   tx,
			scannedFiles:  runtimeResult.ScannedFiles,
			modifiedFiles: len(changes),
			rows:          make([]scriptPlanRow, 0, len(changes)),
			fileSets:      host.FileSetStage(),
		}
		// A run that only touched file sets has no archive change to apply, but
		// the plan is still worth retaining: the user must be able to review and
		// apply it.
		plan.fileSetRows = buildFileSetPreviewRows(plan.fileSets)
		result.FileSetChanges = len(plan.fileSetRows)
		for _, change := range changes {
			status := ScriptFileChanged
			switch change.Kind {
			case pvf.ChangeKindCreated:
				status = ScriptFileAdded
			case pvf.ChangeKindDeleted:
				status = ScriptFileDeleted
			}
			row := scriptPlanRow{change: change, afterText: change.AfterText}
			row.preview = ScriptFilePreview{
				ChangeKey:  change.Normalized,
				Path:       change.Path,
				Status:     status,
				MatchCount: 1,
			}
			if row.preview.Path == "" {
				row.preview.Path = change.Normalized
			}
			// Structural rows have no stable entry index; only an in-place
			// payload edit can reference the entry it came from.
			row.preview.FileIndex = -1
			if status == ScriptFileChanged {
				if index, ok := a.Find(change.Path); ok {
					row.preview.FileIndex = index
				}
			}
			row.preview.Diff, row.preview.DiffTruncated = buildBatchDiff(change.BeforeText, change.AfterText)
			plan.rows = append(plan.rows, row)
		}
		sort.SliceStable(plan.rows, func(left, right int) bool {
			return plan.rows[left].preview.ChangeKey < plan.rows[right].preview.ChangeKey
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

// PreviewPage returns one page of preview rows, optionally narrowed to rows
// whose path contains filter (case-insensitive). Filtering happens here rather
// than in the UI so pagination and the matched total stay consistent: a filter
// that matches only later pages still reports and pages its matches correctly.
func (s *ScriptService) PreviewPage(planID, filter string, cursor, limit int) (*ScriptPreviewPage, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	plan, err := s.currentScriptPlanLocked(planID)
	if err != nil {
		return nil, err
	}
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 500 {
		limit = scriptPageSize
	}

	matched := matchingScriptRows(plan, filter)
	if cursor > len(matched) {
		cursor = len(matched)
	}
	end := cursor + limit
	if end > len(matched) {
		end = len(matched)
	}
	rows := make([]*ScriptFilePreview, 0, end-cursor)
	for index := cursor; index < end; index++ {
		row := matched[index].preview
		row.Warnings = append([]string(nil), row.Warnings...)
		row.Diff = append([]*BatchDiffLine(nil), row.Diff...)
		rows = append(rows, &row)
	}
	next := -1
	if end < len(matched) {
		next = end
	}
	page := &ScriptPreviewPage{
		PlanID: plan.id, NextCursor: next,
		Filter:        strings.TrimSpace(filter),
		MatchedFiles:  len(matched),
		ScannedFiles:  plan.scannedFiles,
		ModifiedFiles: plan.modifiedFiles,
		Rows:          rows,
		FileSetRows:   plan.fileSetRows,
	}
	if len(plan.fileSetRows) > 0 {
		page.FileSetSelectKey = fileSetChangeKey
	}
	return page, nil
}

// matchingScriptRows returns the plan rows whose path contains filter. An empty
// filter matches every row. The order matches the plan's stable path order.
func matchingScriptRows(plan *scriptPlan, filter string) []*scriptPlanRow {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		matched := make([]*scriptPlanRow, 0, len(plan.rows))
		for index := range plan.rows {
			matched = append(matched, &plan.rows[index])
		}
		return matched
	}
	matched := make([]*scriptPlanRow, 0)
	for index := range plan.rows {
		row := &plan.rows[index]
		if strings.Contains(strings.ToLower(row.preview.Path), filter) {
			matched = append(matched, row)
		}
	}
	return matched
}

// SelectableChangeKeys returns the change keys of every row matching filter
// that can be applied. The frontend uses this so "apply selected" covers the
// whole filtered set even when only the loaded pages were ever previewed. The
// file set key is filter-independent: a fileset name is not an archive path.
func (s *ScriptService) SelectableChangeKeys(planID, filter string) ([]string, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	plan, err := s.currentScriptPlanLocked(planID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(plan.rows))
	for _, row := range matchingScriptRows(plan, filter) {
		if row.preview.Status == ScriptFileChanged ||
			row.preview.Status == ScriptFileAdded ||
			row.preview.Status == ScriptFileDeleted {
			keys = append(keys, row.preview.ChangeKey)
		}
	}
	if len(plan.fileSetRows) > 0 {
		keys = append(keys, fileSetChangeKey)
	}
	return keys, nil
}

// Apply commits exactly the selected changed rows from one script plan. Rows
// are addressed by ChangeKey because created and deleted entries shift the
// entry indexes the other rows were previewed with.
func (s *ScriptService) Apply(planID string, changeKeys []string) (ScriptApplyResult, error) {
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
	if len(changeKeys) == 0 {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, fmt.Errorf("没有选中的脚本变更文件")
	}
	rowsByKey := make(map[string]*scriptPlanRow, len(plan.rows))
	for index := range plan.rows {
		row := &plan.rows[index]
		rowsByKey[row.preview.ChangeKey] = row
	}
	selected := make(map[string]struct{}, len(changeKeys))
	ordered := make([]string, 0, len(changeKeys))
	paths := make([]string, 0, len(changeKeys))
	// File set changes are applied separately from archive changes: they write
	// user config, not the PVF overlay. The key addresses the whole group, which
	// is what the preview panel's single checkbox selects.
	applyFileSets := false
	for _, key := range changeKeys {
		if _, exists := selected[key]; exists {
			continue
		}
		if key == fileSetChangeKey {
			if len(plan.fileSetRows) == 0 {
				s.c.mu.Unlock()
				return ScriptApplyResult{}, fmt.Errorf("变更 %q 不是可应用的脚本结果", key)
			}
			applyFileSets = true
			continue
		}
		row, exists := rowsByKey[key]
		if !exists {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, fmt.Errorf("变更 %q 不是可应用的脚本结果", key)
		}
		selected[key] = struct{}{}
		ordered = append(ordered, key)
		paths = append(paths, row.change.Path)
	}
	if len(ordered) == 0 && !applyFileSets {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, fmt.Errorf("没有选中的脚本变更文件")
	}
	var fileSetChanges []scriptengine.FileSetChange
	if applyFileSets && plan.fileSets != nil {
		fileSetChanges = plan.fileSets.Changes()
	}
	var before pvfversion.ContentSnapshot
	if len(paths) > 0 && s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(plan.archive, paths)
		if err != nil {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, err
		}
	}
	mutationCheckpoint := plan.archive.MutationCheckpoint()
	structural, err := plan.transaction.Commit(selected)
	if err != nil {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, err
	}
	// A structural commit changes the entry table, so every derived index must
	// be rebuilt before the new tree can be served.
	if structural {
		if err := s.c.rebuildArchiveIndexesLocked(plan.archive); err != nil {
			s.c.mu.Unlock()
			return ScriptApplyResult{}, err
		}
	}
	if len(paths) > 0 && s.c.versionRepo != nil {
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
	// Persist file set changes only after the archive commit succeeded, so a
	// rejected commit cannot leave user config ahead of the archive.
	appliedFileSets, fileSetErr := s.applyFileSetChanges(fileSetChanges)
	if fileSetErr != nil {
		s.c.mu.Unlock()
		return ScriptApplyResult{}, fileSetErr
	}
	// After a structural commit every index is rebuilt, so resolve each
	// surviving path against the new table instead of the previewed index.
	appliedIndexes := make([]int32, 0, len(ordered))
	for _, key := range ordered {
		row := rowsByKey[key]
		if row.preview.Status != ScriptFileChanged {
			continue
		}
		index, ok := plan.archive.Find(row.change.Path)
		if !ok {
			continue
		}
		if s.c.editorText == nil {
			s.c.editorText = make(map[int32]string)
		}
		s.c.editorText[index] = row.afterText
		appliedIndexes = append(appliedIndexes, index)
	}
	mutationSummary := plan.archive.MutationsSince(mutationCheckpoint)
	plan.archive.ClearMutations()
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
		"fileIndexes": appliedIndexes, "modifiedCount": info.ModifiedCount,
		"revision": revision, "source": "script", "structural": structural,
	})
	emitEvent("archive:script-applied", map[string]any{
		"fileIndexes": appliedIndexes, "modifiedCount": info.ModifiedCount,
		"revision": revision, "structural": structural,
	})
	// Only a structural commit changes the entry table, so the tree and open
	// tabs must be re-resolved; a payload-only apply keeps every index valid.
	if structural {
		emitEvent("archive:changed", info)
	}
	// File sets live in user config, not the archive, so the sidebar's in-memory
	// copy is stale after an apply and must reload.
	if appliedFileSets > 0 {
		emitEvent("fileset:changed", map[string]any{"count": appliedFileSets, "source": "script"})
	}
	if versioned {
		emitVersionState(s.c, "script-applied")
	}
	s.c.scheduleArchiveMutations(plan.archive, mutationSummary)
	return ScriptApplyResult{
		AppliedFiles: int32(len(ordered)), FileIndexes: appliedIndexes,
		ModifiedCount: info.ModifiedCount, Revision: revision, Structural: structural,
		AppliedFileSets: appliedFileSets,
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
