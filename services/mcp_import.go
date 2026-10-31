package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mcpImportRow struct {
	Key            string           `json:"key"`
	SourcePath     string           `json:"sourcePath"`
	TargetPath     string           `json:"targetPath"`
	Overwrite      bool             `json:"overwrite"`
	Size           int              `json:"size"`
	DataType       int32            `json:"dataType"`
	Diff           []*BatchDiffLine `json:"diff,omitempty"`
	DiffTruncated  bool             `json:"diffTruncated,omitempty"`
	SourceEncoding string           `json:"sourceEncoding,omitempty"`
}
type mcpImportPlan struct {
	id           string
	workspace    string
	revision     uint64
	created      time.Time
	files        []importFile
	rows         []mcpImportRow
	target, mode string
}

func protectedImportPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		name := strings.ToLower(part)
		if name == ".env" || strings.HasPrefix(name, ".env.") || name == ".ssh" || name == ".aws" || name == ".gnupg" || name == "credentials" || name == "credentials.json" || name == "id_rsa" || name == "id_ed25519" || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
			return true
		}
	}
	return false
}
func (s *MCPService) importPrepare(a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	if err := s.noPlans(); err != nil {
		return nil, err
	}
	target, err := normalizeImportTargetDir(a.TargetDir)
	if err != nil {
		return nil, err
	}
	mode, err := normalizeImportMode(a.Mode)
	if err != nil {
		return nil, err
	}
	for _, path := range a.SourcePaths {
		if err := absolutePath(path); err != nil {
			return nil, err
		}
		if protectedImportPath(path) {
			return nil, fmt.Errorf("PROTECTED_SOURCE: 禁止读取敏感配置")
		}
	}
	files, err := collectImportFilesLimited(a.SourcePaths, target, 10000)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 || len(files) > 10000 {
		return nil, fmt.Errorf("IMPORT_LIMIT: 导入文件数量需为 1-10000")
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range files {
		if protectedImportPath(f.sourcePath) {
			return nil, fmt.Errorf("PROTECTED_SOURCE: 来源目录含敏感配置，请缩小范围")
		}
		key := strings.ToLower(f.targetPath)
		if seen[key] {
			return nil, fmt.Errorf("IMPORT_CONFLICT: 多个来源映射同一目标")
		}
		seen[key] = true
		info, err := os.Stat(f.sourcePath)
		if err != nil {
			return nil, err
		}
		total += info.Size()
		if info.Size() > 8<<20 || total > 64<<20 {
			return nil, fmt.Errorf("IMPORT_LIMIT: 单文件最多 8 MiB，总计最多 64 MiB")
		}
	}
	if err := readMCPImportFiles(files, mode); err != nil {
		return nil, err
	}
	total = 0
	for _, f := range files {
		total += int64(len(f.data))
		if len(f.data) > 8<<20 || total > 64<<20 {
			return nil, fmt.Errorf("IMPORT_LIMIT")
		}
	}
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if err := s.checkLocked(a); err != nil {
		return nil, err
	}
	plan := &mcpImportPlan{id: "import-" + mcpID(), workspace: a.WorkspaceID, revision: a.ExpectedRevision, created: time.Now(), files: files, target: target, mode: mode}
	stage := s.c.archive.CloneForBatch()
	defer stage.Release()
	if _, err := applyPreparedImport(stage, files, mode); err != nil {
		return nil, err
	}
	for _, f := range files {
		i, exists := s.c.archive.Find(f.targetPath)
		row := mcpImportRow{Key: f.targetPath, SourcePath: f.sourcePath, TargetPath: f.targetPath, Overwrite: exists, Size: len(f.data), DataType: f.dataType, SourceEncoding: f.sourceEncoding}
		if mode == ImportModeText {
			before := ""
			if exists {
				before, _ = s.c.archive.Text(i)
			}
			afterIndex, _ := stage.Find(f.targetPath)
			after, _ := stage.Text(afterIndex)
			row.Diff, row.DiffTruncated = buildBatchDiff(before, after)
		}
		plan.rows = append(plan.rows, row)
	}
	s.mu.Lock()
	s.imports[plan.id] = plan
	s.mu.Unlock()
	s.c.mcpImportPlan = plan.id
	time.AfterFunc(mcpTTL, func() {
		s.mu.Lock()
		delete(s.imports, plan.id)
		s.mu.Unlock()
		s.c.mu.Lock()
		if s.c.mcpImportPlan == plan.id {
			s.c.mcpImportPlan = ""
		}
		s.c.mu.Unlock()
	})
	return map[string]any{"importPlanId": plan.id, "totalFiles": len(files), "mode": mode, "targetDir": target}, nil
}
func (s *MCPService) importPlan(id string) (*mcpImportPlan, error) {
	s.mu.Lock()
	p := s.imports[id]
	s.mu.Unlock()
	if p == nil || time.Since(p.created) > mcpTTL {
		return nil, fmt.Errorf("STALE_IMPORT")
	}
	if err := s.check(mcpArgs{WorkspaceID: p.workspace, ExpectedRevision: p.revision}); err != nil {
		return nil, err
	}
	return p, nil
}
func (s *MCPService) importPreview(a mcpArgs) (any, error) {
	p, err := s.importPlan(a.ImportPlanID)
	if err != nil {
		return nil, err
	}
	start := min(a.Cursor, len(p.rows))
	end := min(start+a.Limit, len(p.rows))
	next := -1
	if end < len(p.rows) {
		next = end
	}
	rows := append([]mcpImportRow(nil), p.rows[start:end]...)
	for i := range rows {
		var cut bool
		rows[i].Diff, cut = mcpDiff(rows[i].Diff)
		rows[i].DiffTruncated = rows[i].DiffTruncated || cut
	}
	return map[string]any{"importPlanId": p.id, "rows": rows, "nextCursor": next, "total": len(p.rows)}, nil
}
func (s *MCPService) importApply(a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	p, err := s.importPlan(a.ImportPlanID)
	if err != nil {
		return nil, err
	}
	overwrite := map[string]bool{}
	for _, key := range a.OverwriteKeys {
		overwrite[key] = true
	}
	lookup := map[string]int{}
	for i, row := range p.rows {
		lookup[row.Key] = i
	}
	chosen := map[string]bool{}
	files := []importFile{}
	for _, key := range a.SelectedKeys {
		i, ok := lookup[key]
		if !ok {
			return nil, fmt.Errorf("INVALID_SELECTION")
		}
		if chosen[key] {
			continue
		}
		chosen[key] = true
		if p.rows[i].Overwrite && !overwrite[key] {
			return nil, fmt.Errorf("OVERWRITE_REQUIRED: %s", key)
		}
		files = append(files, p.files[i])
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("EMPTY_SELECTION")
	}
	r, err := s.archive.importPreparedRevision(files, p.target, p.mode, p.workspace, p.revision)
	if err != nil {
		return r, err
	}
	s.mu.Lock()
	delete(s.imports, p.id)
	s.mu.Unlock()
	s.c.mu.Lock()
	s.c.mcpImportPlan = ""
	revision := s.c.batchRevision
	s.c.mu.Unlock()
	emitEvent("archive:batch-applied", map[string]any{"structural": true})
	workspace := s.workspace()
	return map[string]any{"import": r, "revision": revision, "sourceDirty": workspace["sourceDirty"], "workspace": workspace}, nil
}

func readMCPImportFiles(files []importFile, mode string) error {
	var total int64
	for i := range files {
		file := &files[i]
		resolved, err := filepath.EvalSymlinks(file.sourcePath)
		if err != nil {
			return err
		}
		if protectedImportPath(resolved) {
			return fmt.Errorf("PROTECTED_SOURCE")
		}
		handle, err := os.Open(file.sourcePath)
		if err != nil {
			return err
		}
		info, err := handle.Stat()
		if err != nil || !info.Mode().IsRegular() {
			handle.Close()
			return fmt.Errorf("INVALID_SOURCE")
		}
		limit := min(int64(8<<20), int64(64<<20)-total)
		data, err := io.ReadAll(io.LimitReader(handle, limit+1))
		handle.Close()
		if err != nil {
			return err
		}
		if int64(len(data)) > limit {
			return fmt.Errorf("IMPORT_LIMIT")
		}
		total += int64(len(data))
		file.data = data
		file.dataType = importDataType(file.targetPath)
		if mode == ImportModeText {
			if err := file.decodeText(); err != nil {
				return fmt.Errorf("INVALID_ENCODING: %w", err)
			}
		}
	}
	return nil
}
