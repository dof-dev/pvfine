package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

func (s *MCPService) workspace() map[string]any {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	c := s.c
	if c.archive != nil && c.mcpWorkspaceID == "" {
		c.mcpWorkspaceID = mcpID()
		c.mcpEpoch++
	}
	s.mu.Lock()
	windowStates := map[string]any{}
	for id, w := range s.windows {
		windowStates[id] = map[string]any{"dirty": w.dirty, "busy": w.busy, "stale": time.Since(w.seen) > 12*time.Second}
	}
	s.mu.Unlock()
	result := map[string]any{"windows": windowStates, "open": c.archive != nil, "workspaceEpoch": c.mcpEpoch, "workspaceId": c.mcpWorkspaceID, "revision": c.batchRevision, "versionLoading": c.versionLoading, "indexStatus": c.indexStatus, "capabilityVersion": 1}
	if c.archive != nil {
		result["archive"] = c.archive.Info()
		result["sourcePath"] = c.archive.SourcePath()
		result["sourceDirty"] = c.archive.Modified() || len(c.versionArtifactDirty) > 0
	}
	return result
}
func (s *MCPService) checkLocked(a mcpArgs) error {
	if s.c.archive == nil {
		return ErrNoArchive
	}
	if a.WorkspaceID == "" || a.WorkspaceID != s.c.mcpWorkspaceID {
		return fmt.Errorf("STALE_WORKSPACE: 请重新获取工作区")
	}
	if a.ExpectedRevision != s.c.batchRevision {
		return fmt.Errorf("STALE_WORKSPACE: 修订号已变化")
	}
	return s.c.ensureVersionReadyLocked()
}
func (s *MCPService) check(a mcpArgs) error {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return s.checkLocked(a)
}
func (s *MCPService) noPlans() error {
	if s.script.IsRunning() {
		return fmt.Errorf("TASK_BUSY: 脚本运行中")
	}
	s.c.mu.RLock()
	pending := s.c.scriptPlan != nil || s.c.batchPlan != nil || s.c.mcpImportPlan != ""
	s.c.mu.RUnlock()
	if pending {
		return fmt.Errorf("PLAN_PENDING: 请先应用或丢弃待处理预览")
	}
	s.export.mu.Lock()
	pending = s.export.plan != nil && s.export.validLockedSafe()
	s.export.mu.Unlock()
	if pending {
		return fmt.Errorf("EXPORT_PLAN_PENDING: 请先释放导出预览")
	}
	return nil
}
func (s *ExportService) validLockedSafe() bool {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return s.validLocked(s.plan)
}
func (s *MCPService) dirty(a mcpArgs) error {
	if a.DirtyPolicy != "" && a.DirtyPolicy != "reject" && a.DirtyPolicy != "save" && a.DirtyPolicy != "discard" {
		return fmt.Errorf("INVALID_ARGUMENT: dirtyPolicy")
	}
	s.c.mu.RLock()
	dirty := s.c.archive != nil && (s.c.archive.Modified() || len(s.c.versionArtifactDirty) > 0)
	s.c.mu.RUnlock()
	if !dirty {
		return nil
	}
	switch a.DirtyPolicy {
	case "discard":
		return nil
	case "save":
		_, err := s.save(a, false)
		return err
	default:
		return fmt.Errorf("UNSAVED_CHANGES: 请指定 save 或 discard，或先保存")
	}
}
func absolutePath(path string) error {
	if !filepath.IsAbs(path) || strings.TrimSpace(path) == "" || protectedImportPath(path) {
		return fmt.Errorf("INVALID_PATH: 需要本机绝对路径")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		parent, e := filepath.EvalSymlinks(filepath.Dir(path))
		if e == nil {
			resolved = filepath.Join(parent, filepath.Base(path))
		}
	}
	if protectedImportPath(resolved) {
		return fmt.Errorf("PROTECTED_PATH")
	}
	return nil
}
func (s *MCPService) open(a mcpArgs) (any, error) {
	if err := absolutePath(a.Path); err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(a.Path), ".pvf") {
		return nil, fmt.Errorf("INVALID_PATH: 归档路径必须以 .pvf 结尾")
	}
	s.c.mu.RLock()
	epoch := s.c.mcpEpoch
	loaded := s.c.archive != nil
	s.c.mu.RUnlock()
	if epoch != a.ExpectedWorkspaceEpoch {
		return nil, fmt.Errorf("STALE_WORKSPACE: 工作区已切换")
	}
	if loaded {
		if err := s.check(a); err != nil {
			return nil, err
		}
	}
	if err := s.noPlans(); err != nil {
		return nil, err
	}
	// Parse the candidate before any save/discard of the old workspace.
	candidate, err := s.c.openArchiveWithRememberedTextEncoding(a.Path, a.Path, "")
	if err != nil {
		return nil, err
	}
	installed := false
	defer func() {
		if !installed {
			candidate.Release()
		}
	}()
	if err = prepareArchiveDerivedIndexesForMCP(candidate); err != nil {
		return nil, err
	}
	if err = s.dirty(a); err != nil {
		return nil, err
	}
	if loaded {
		if err = s.check(a); err != nil {
			return nil, err
		}
	}
	// The UI is frozen for this task; Open performs the normal archive setup.
	if _, err = s.c.installOpenedArchive(a.Path, candidate, time.Now()); err != nil {
		return nil, err
	}
	installed = true
	return s.workspace(), nil
}
func prepareArchiveDerivedIndexesForMCP(a *pvf.Archive) error {
	p, err := prepareArchiveDerivedIndexes(a)
	if p != nil {
		closePreparedArchiveIndexes(p)
	}
	return err
}
func (s *MCPService) close(a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	if err := s.noPlans(); err != nil {
		return nil, err
	}
	if err := s.dirty(a); err != nil {
		return nil, err
	}
	if err := s.check(a); err != nil {
		return nil, err
	}
	s.archive.Close()
	return s.workspace(), nil
}
func (s *MCPService) save(a mcpArgs, copyOnly bool) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	if err := s.noPlans(); err != nil {
		return nil, err
	}
	if copyOnly {
		if err := absolutePath(a.Path); err != nil {
			return nil, err
		}
		if !strings.EqualFold(filepath.Ext(a.Path), ".pvf") {
			return nil, fmt.Errorf("INVALID_PATH: 目标必须以 .pvf 结尾")
		}
		s.c.mu.RLock()
		source := s.c.archive.SourcePath()
		s.c.mu.RUnlock()
		if sameDiskPath(source, a.Path) {
			return nil, fmt.Errorf("SAME_AS_SOURCE: 请使用 archive_save")
		}
		if info, err := os.Stat(a.Path); err == nil {
			if info.IsDir() {
				return nil, fmt.Errorf("INVALID_PATH: 目标为目录")
			}
			digest, err := pvfversion.HashFile(a.Path)
			if err != nil {
				return nil, err
			}
			if !a.Overwrite {
				return nil, fmt.Errorf("TARGET_EXISTS: targetDigest=%s", digest)
			}
			if a.TargetDigest == "" || a.TargetDigest != digest {
				return nil, fmt.Errorf("TARGET_CHANGED")
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		} else if a.Overwrite {
			return nil, fmt.Errorf("TARGET_CHANGED: 原目标已不存在")
		}
		path, err := s.editor.saveCopy(a.Path, a.Overwrite, a.TargetDigest)
		if err != nil {
			return nil, err
		}
		return map[string]any{"writtenPath": path, "workspace": s.workspace()}, nil
	}
	s.c.mu.RLock()
	source := s.c.archive.SourcePath()
	baseline := s.c.mcpSourceHash
	if baseline == "" {
		baseline = s.c.archive.SourceHash()
	}
	s.c.mu.RUnlock()
	if source == "" {
		return nil, fmt.Errorf("SAVE_AS_REQUIRED")
	}
	if err := absolutePath(source); err != nil {
		return nil, err
	}
	digest, err := pvfversion.HashFile(source)
	if err != nil {
		return nil, err
	}
	if digest != baseline {
		return nil, fmt.Errorf("SOURCE_CHANGED: 源文件已在外部修改")
	}
	s.c.mu.RLock()
	savedSequence := s.c.mcpSaveSequence
	s.c.mu.RUnlock()
	info, err := s.editor.Save()
	if err != nil {
		s.c.mu.RLock()
		committed := s.c.mcpSaveSequence != savedSequence
		s.c.mu.RUnlock()
		return map[string]any{"diskCommitted": committed, "workspace": s.workspace()}, err
	}
	return map[string]any{"diskCommitted": true, "writtenPath": source, "archive": info, "workspace": s.workspace()}, nil
}
func sameDiskPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	if aa == bb {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}
func (s *MCPService) ownScript(id string) error {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if id == "" || s.c.mcpScriptPlan != id {
		return fmt.Errorf("PLAN_NOT_FOUND: 非当前 MCP 脚本预览")
	}
	_, err := s.script.currentScriptPlanLocked(id)
	return err
}
func (s *MCPService) run(ctx context.Context, a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	if err := s.noPlans(); err != nil {
		return nil, err
	}
	result, err := s.script.Run(ctx, ScriptRunRequest{Name: a.Label, Source: a.Source})
	if err != nil {
		return result, err
	}
	if result.PlanID != "" && result.ModifiedFiles == 0 && result.FileSetChanges == 0 {
		s.script.Discard(result.PlanID)
		result.PlanID = ""
	}
	if result.PlanID != "" {
		s.c.mu.Lock()
		plan := s.c.scriptPlan
		if plan != nil && plan.id == result.PlanID && len(plan.fileSetRows) > 0 {
			s.c.invalidateScriptPlanLocked()
			s.c.mu.Unlock()
			return nil, fmt.Errorf("UNSUPPORTED_CHANGE: MCP 不支持文件集修改")
		}
		s.c.mcpScriptPlan = result.PlanID
		s.c.mu.Unlock()
		id := result.PlanID
		time.AfterFunc(mcpTTL, func() {
			s.c.mu.Lock()
			if s.c.mcpScriptPlan == id {
				s.c.invalidateScriptPlanLocked()
				s.c.mcpScriptPlan = ""
			}
			s.c.mu.Unlock()
		})
	}
	emitEvent("mcp:script-result", result)
	return result, nil
}
func (s *MCPService) search(a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	mode := "quick"
	if a.Mode == "content" {
		mode = "string"
	} else if a.Mode != "" && a.Mode != "path" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: mode=path/content")
	}
	r, err := s.archive.AdvancedSearch(mode, a.Query, a.Scope, false, a.Cursor, a.Limit)
	if err != nil {
		return nil, err
	}
	if err = s.check(a); err != nil {
		return nil, err
	}
	return map[string]any{"revision": a.ExpectedRevision, "page": r}, nil
}
func (s *MCPService) readFile(a mcpArgs) (any, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if err := s.checkLocked(a); err != nil {
		return nil, err
	}
	index, ok := s.c.archive.Find(a.Path)
	if !ok {
		return nil, fmt.Errorf("FILE_NOT_FOUND")
	}
	if s.c.archive.File(index).DataSize > 8<<20 {
		return nil, fmt.Errorf("FILE_TOO_LARGE: 请使用限定范围的脚本查询")
	}
	text, err := s.c.archive.Text(index)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	start := max(0, a.StartLine-1)
	if start > len(lines) {
		start = len(lines)
	}
	end := min(len(lines), start+a.Limit)
	part := strings.Join(lines[start:end], "\n")
	if len(part) > 256<<10 {
		return nil, fmt.Errorf("RESULT_TOO_LARGE: 单行或所选范围过大")
	}
	next := -1
	if end < len(lines) {
		next = end + 1
	}
	return map[string]any{"path": a.Path, "revision": a.ExpectedRevision, "text": part, "totalLines": len(lines), "startLine": start + 1, "nextLine": next}, nil
}
